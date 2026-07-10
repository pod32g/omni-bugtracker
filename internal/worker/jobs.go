package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/git"
	"github.com/omni/bugtracker/internal/integrations"
	"github.com/omni/bugtracker/internal/service"
)

// eventWorker is the dispatcher: it fans one domain event out into downstream jobs.
type eventWorker struct {
	river.WorkerDefaults[events.DomainEventArgs]
	d Deps
}

func (w *eventWorker) Work(ctx context.Context, job *river.Job[events.DomainEventArgs]) error {
	client := river.ClientFromContext[pgx.Tx](ctx)
	ev := job.Args
	w.d.Logger.Info("dispatch", "event", ev.EventType, "issue", ev.IssueID)

	// Notify + index on every issue event; automation always evaluates.
	if ev.IssueID != "" {
		if _, err := client.Insert(ctx, events.NotifyJobArgs{
			EventType: ev.EventType, IssueID: ev.IssueID, ActorID: ev.ActorID,
		}, nil); err != nil {
			return err
		}
		if _, err := client.Insert(ctx, events.IndexJobArgs{DocType: "issue", DocID: ev.IssueID}, nil); err != nil {
			return err
		}
		if _, err := client.Insert(ctx, events.AutomationJobArgs{EventType: ev.EventType, IssueID: ev.IssueID}, nil); err != nil {
			return err
		}
	}
	// Fan out to subscribed webhooks: active hooks whose event filter matches
	// (empty filter = everything) and whose project scope covers the issue.
	if ev.IssueID != "" {
		if err := w.fanOutWebhooks(ctx, client, ev); err != nil {
			return err
		}
	}
	return nil
}

func (w *eventWorker) fanOutWebhooks(ctx context.Context, client *river.Client[pgx.Tx], ev events.DomainEventArgs) error {
	issueID, err := uuid.Parse(ev.IssueID)
	if err != nil {
		return nil //nolint:nilerr // non-issue events don't fan out
	}
	rows, err := w.d.DB.Query(ctx, `
		SELECT w.id FROM webhooks w
		WHERE w.is_active
		  AND (cardinality(w.events) = 0 OR $2 = ANY(w.events))
		  AND (w.project_id IS NULL OR w.project_id = (SELECT project_id FROM issues WHERE id = $1))`,
		issueID, ev.EventType)
	if err != nil {
		return err
	}
	var hookIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		hookIDs = append(hookIDs, id)
	}
	rows.Close()
	if len(hookIDs) == 0 {
		return nil
	}

	issue, err := w.d.Store.GetIssueByID(ctx, issueID)
	if err != nil {
		return softFail(w.d, "webhook_payload", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"event":       ev.EventType,
		"actor_id":    ev.ActorID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
		"issue": map[string]any{
			"id": issue.ID, "key": issue.Key, "project_key": issue.ProjectKey,
			"title": issue.Title, "type": issue.Type, "status": issue.Status,
			"priority": issue.Priority, "severity": issue.Severity,
		},
	})

	for _, hookID := range hookIDs {
		var deliveryID uuid.UUID
		if err := w.d.DB.QueryRow(ctx, `
			INSERT INTO webhook_deliveries (webhook_id, event_type, payload)
			VALUES ($1, $2, $3) RETURNING id`, hookID, ev.EventType, payload).Scan(&deliveryID); err != nil {
			return err
		}
		if _, err := client.Insert(ctx, events.WebhookJobArgs{
			WebhookID: hookID.String(), DeliveryID: deliveryID.String(),
			EventType: ev.EventType, Payload: payload,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// notifyWorker delivers to Omni-Notify (fails soft when disabled).
type notifyWorker struct {
	river.WorkerDefaults[events.NotifyJobArgs]
	d Deps
}

func (w *notifyWorker) Work(ctx context.Context, job *river.Job[events.NotifyJobArgs]) error {
	err := w.d.Adapters.Notify.Notify(ctx, integrations.NotifyEvent{
		EventType: job.Args.EventType, IssueID: job.Args.IssueID, ActorID: job.Args.ActorID,
		Recipients: job.Args.Recipients,
	})
	return softFail(w.d, "notify", err)
}

// webhookWorker delivers one outbound webhook. River handles retry/backoff via MaxAttempts.
type webhookWorker struct {
	river.WorkerDefaults[events.WebhookJobArgs]
	d Deps
}

func (w *webhookWorker) Work(ctx context.Context, job *river.Job[events.WebhookJobArgs]) error {
	var url, secret string
	err := w.d.DB.QueryRow(ctx,
		`SELECT url, secret FROM webhooks WHERE id = $1 AND is_active`, job.Args.WebhookID).
		Scan(&url, &secret)
	if err != nil {
		// Hook deleted or deactivated since enqueue — mark dead, don't retry.
		w.markDelivery(ctx, job.Args.DeliveryID, "dead", nil)
		return nil //nolint:nilerr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(job.Args.Payload))
	if err != nil {
		w.markDelivery(ctx, job.Args.DeliveryID, "dead", nil)
		return nil //nolint:nilerr // malformed URL never succeeds
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OBT-Event", job.Args.EventType)
	req.Header.Set("X-OBT-Delivery", job.Args.DeliveryID)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(job.Args.Payload)
		req.Header.Set("X-OBT-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	var code *int
	success := false
	if err == nil {
		c := resp.StatusCode
		code = &c
		success = c >= 200 && c < 300
		_ = resp.Body.Close()
	}

	if success {
		w.d.Metrics.WebhookAttempts.WithLabelValues("success").Inc()
		w.markDelivery(ctx, job.Args.DeliveryID, "success", code)
		return nil
	}
	w.d.Metrics.WebhookAttempts.WithLabelValues("failed").Inc()
	status := "failed"
	if job.Attempt >= 8 { // MaxAttempts — no more retries coming
		status = "dead"
	}
	w.markDelivery(ctx, job.Args.DeliveryID, status, code)
	if err != nil {
		return err
	}
	return errors.New("webhook delivery got HTTP " + strconv.Itoa(*code))
}

func (w *webhookWorker) markDelivery(ctx context.Context, deliveryID, status string, code *int) {
	if _, err := w.d.DB.Exec(ctx,
		`UPDATE webhook_deliveries SET status = $2::delivery_status, response_code = $3,
		        attempt = attempt + 1, updated_at = now()
		 WHERE id = $1`, deliveryID, status, code); err != nil {
		w.d.Logger.Error("webhook delivery bookkeeping", "err", err)
	}
}

// indexWorker projects a document into Omni-Search.
type indexWorker struct {
	river.WorkerDefaults[events.IndexJobArgs]
	d Deps
}

func (w *indexWorker) Work(ctx context.Context, job *river.Job[events.IndexJobArgs]) error {
	// TODO: load the entity and build a richer SearchDoc (title/body/fields).
	err := w.d.Adapters.Search.Index(ctx, integrations.SearchDoc{
		ID: job.Args.DocID, Type: job.Args.DocType,
	})
	return softFail(w.d, "search_index", err)
}

// automationWorker evaluates automation rules against the event.
type automationWorker struct {
	river.WorkerDefaults[events.AutomationJobArgs]
	d Deps
}

func (w *automationWorker) Work(ctx context.Context, job *river.Job[events.AutomationJobArgs]) error {
	// TODO: load active rules (project + global), evaluate trigger AST, run matched actions.
	// A per-issue/per-rule cooldown + source=automation guard prevents feedback loops.
	w.d.Metrics.JobsProcessed.WithLabelValues("automation", "noop").Inc()
	_ = ctx
	_ = job
	return nil
}

// gitIngestWorker parses commit/PR payloads for issue references.
type gitIngestWorker struct {
	river.WorkerDefaults[events.GitIngestArgs]
	d Deps
}

func (w *gitIngestWorker) Work(ctx context.Context, job *river.Job[events.GitIngestArgs]) error {
	ev, err := git.Parse(job.Args.Provider, job.Args.Event, job.Args.Payload)
	if err != nil {
		// Unparseable payload: log and drop (retrying won't help).
		w.d.Logger.Warn("git ingest: parse failed", "err", err, "event", job.Args.Event)
		w.d.Metrics.JobsProcessed.WithLabelValues("git_ingest", "unparseable").Inc()
		return nil
	}
	gitCfg := w.d.Cfg.Integrations.Git
	parser := git.NewRefParser(gitCfg.CloseVerbs, gitCfg.LinkVerbs)
	client := river.ClientFromContext[pgx.Tx](ctx)

	linked := 0
	switch ev.Kind {
	case "push":
		for _, c := range ev.Commits {
			commitID, err := w.d.Store.UpsertCommit(ctx, service.CommitInput{
				Repo: ev.Repo, SHA: c.SHA, Author: c.Author, Message: c.Message,
				URL: c.URL, CommittedAt: c.Timestamp,
			})
			if err != nil {
				w.d.Logger.Warn("git ingest: upsert commit", "err", err, "sha", c.SHA)
				continue
			}
			for _, ref := range parser.Parse(c.Message) {
				// A "fixes" in a pushed commit resolves the issue (final close is the merge/human).
				var newStatus *domain.IssueStatus
				if ref.Close {
					newStatus = ptrStatus(domain.StatusResolved)
				}
				if w.applyRef(ctx, client, ref, &commitID, nil, "commit", c.SHA, newStatus) {
					linked++
				}
			}
		}
	case "pull_request":
		pr := ev.PR
		if pr == nil {
			return nil
		}
		prID, err := w.d.Store.UpsertPullRequest(ctx, service.PRInput{
			Repo: ev.Repo, Number: pr.Number, URL: pr.URL, Title: pr.Title,
			State: pr.State, MergedAt: mergedAt(pr),
		})
		if err != nil {
			w.d.Logger.Warn("git ingest: upsert PR", "err", err, "num", pr.Number)
			return err
		}
		for _, ref := range parser.Parse(pr.Title + "\n" + pr.Body) {
			// Auto-close only when the PR is actually merged and policy allows it.
			var newStatus *domain.IssueStatus
			if ref.Close && pr.Merged && gitCfg.CloseOnMerge {
				newStatus = ptrStatus(domain.StatusClosed)
			}
			if w.applyRef(ctx, client, ref, nil, &prID, "PR", strconv.Itoa(pr.Number), newStatus) {
				linked++
			}
		}
	default:
		return nil // ignored event kind (ping, etc.)
	}

	w.d.Logger.Info("git ingest", "kind", ev.Kind, "repo", ev.Repo, "linked", linked)
	w.d.Metrics.JobsProcessed.WithLabelValues("git_ingest", "ok").Inc()
	return nil
}

// applyRef resolves a reference to an issue and links + optionally transitions it.
// Returns true if a link was applied. Unknown issue keys and no-op transitions are skipped.
func (w *gitIngestWorker) applyRef(
	ctx context.Context, client *river.Client[pgx.Tx], ref git.Ref,
	commitID, prID *uuid.UUID, sourceKind, sourceRef string, newStatus *domain.IssueStatus,
) bool {
	issue, err := w.d.Store.GetIssueByKey(ctx, ref.ProjectKey, ref.Number)
	if err != nil {
		return false // unknown issue — ignore the reference
	}
	// Don't re-transition an already-terminal issue.
	if newStatus != nil && isAtOrPast(issue.Status, *newStatus) {
		newStatus = nil
	}

	eventType := events.IssueUpdated
	if newStatus != nil {
		eventType = statusEvent(*newStatus)
	}
	detail, _ := json.Marshal(map[string]any{
		"source": sourceKind, "ref": sourceRef, "verb": ref.Verb,
	})
	publish := func(tx pgx.Tx) error {
		_, err := client.InsertTx(ctx, tx, events.DomainEventArgs{
			EventType: eventType, IssueID: issue.ID.String(), Payload: detail,
		}, nil)
		return err
	}
	err = w.d.Store.ApplyGitLink(ctx, service.GitLinkInput{
		IssueID: issue.ID, CommitID: commitID, PRID: prID, Verb: ref.Verb,
		NewStatus: newStatus, ActivityVerb: activityVerb(sourceKind, newStatus), Detail: detail,
	}, publish)
	if err != nil {
		w.d.Logger.Warn("git ingest: apply link", "err", err, "issue", issue.Key)
		return false
	}
	w.d.Logger.Info("git linked", "issue", issue.Key, "via", sourceKind, "ref", sourceRef,
		"verb", ref.Verb, "transition", newStatus != nil)
	return true
}

func ptrStatus(s domain.IssueStatus) *domain.IssueStatus { return &s }

func mergedAt(pr *git.PullRequest) *time.Time {
	if pr.Merged {
		t := time.Now().UTC()
		return &t
	}
	return nil
}

func statusEvent(s domain.IssueStatus) string {
	switch s {
	case domain.StatusResolved:
		return events.IssueResolved
	case domain.StatusClosed:
		return events.IssueClosed
	default:
		return events.IssueStatusChanged
	}
}

func activityVerb(sourceKind string, newStatus *domain.IssueStatus) string {
	if newStatus == nil {
		if sourceKind == "PR" {
			return "issue.pr_linked"
		}
		return "issue.commit_linked"
	}
	if *newStatus == domain.StatusClosed {
		return "issue.closed_by_git"
	}
	return "issue.resolved_by_git"
}

// isAtOrPast reports whether the issue is already at or beyond the target lifecycle stage,
// so a git-driven transition would be a no-op or a regression.
func isAtOrPast(cur, target domain.IssueStatus) bool {
	rank := map[domain.IssueStatus]int{
		domain.StatusResolved: 1,
		domain.StatusClosed:   2,
	}
	return rank[cur] >= rank[target] && rank[target] > 0
}

// obsIngestWorker creates/dedupes issues from logging/metrics alerts.
type obsIngestWorker struct {
	river.WorkerDefaults[events.ObsIngestArgs]
	d Deps
}

func (w *obsIngestWorker) Work(ctx context.Context, job *river.Job[events.ObsIngestArgs]) error {
	var alert struct {
		Rule       string  `json:"rule"`
		Title      string  `json:"title"`
		Severity   *string `json:"severity"`
		DetailsMD  string  `json:"details_md"`
		StackTrace string  `json:"stack_trace"`
	}
	if err := json.Unmarshal(job.Args.Payload, &alert); err != nil {
		w.d.Logger.Error("obs ingest: bad payload", "err", err)
		return nil //nolint:nilerr // malformed payloads never get better on retry
	}
	var sev *domain.Severity
	if alert.Severity != nil {
		switch domain.Severity(*alert.Severity) {
		case domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow:
			s := domain.Severity(*alert.Severity)
			sev = &s
		}
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	issue, created, err := w.d.Store.IngestObsAlert(ctx, service.ObsAlertInput{
		Source: job.Args.Source, ProjectKey: job.Args.ProjectKey, Fingerprint: job.Args.Fingerprint,
		Title: alert.Title, Rule: alert.Rule, DetailsMD: alert.DetailsMD, StackTrace: alert.StackTrace,
		Severity: sev,
	}, func(tx pgx.Tx, iss domain.Issue, eventType string) error {
		_, err := client.InsertTx(ctx, tx, events.DomainEventArgs{
			EventType: eventType, IssueID: iss.ID.String(),
		}, nil)
		return err
	})
	if err != nil {
		w.d.Metrics.JobsProcessed.WithLabelValues("obs_ingest", "error").Inc()
		return err
	}
	outcome := "bumped"
	if created {
		outcome = "created"
	}
	w.d.Metrics.JobsProcessed.WithLabelValues("obs_ingest", outcome).Inc()
	w.d.Logger.Info("obs ingest", "issue", issue.Key, "outcome", outcome, "source", job.Args.Source)
	return nil
}

// softFail treats a disabled integration as success (skip) and any other error as retryable.
func softFail(d Deps, kind string, err error) error {
	switch {
	case err == nil:
		d.Metrics.JobsProcessed.WithLabelValues(kind, "ok").Inc()
		return nil
	case errors.Is(err, integrations.ErrDisabled):
		d.Metrics.JobsProcessed.WithLabelValues(kind, "disabled").Inc()
		return nil
	default:
		d.Metrics.JobsProcessed.WithLabelValues(kind, "error").Inc()
		d.Logger.Warn("job failed", "kind", kind, "err", err)
		return err
	}
}
