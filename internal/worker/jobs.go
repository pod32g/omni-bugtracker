package worker

import (
	"context"
	"encoding/json"
	"errors"
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
	// TODO: query webhook subscribers for ev.EventType and enqueue one WebhookJobArgs each.
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
	// TODO: load webhook (url/secret) by ID, POST HMAC-signed payload, persist delivery row.
	// Returning an error reschedules with exponential backoff up to MaxAttempts, then dead-letters.
	w.d.Metrics.WebhookAttempts.WithLabelValues("skipped").Inc()
	_ = ctx
	_ = job
	return nil
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
	// TODO: upsert by (project, fingerprint) — repeated alerts increment an occurrence
	// counter on one issue rather than spawning duplicates.
	w.d.Metrics.JobsProcessed.WithLabelValues("obs_ingest", "noop").Inc()
	_ = ctx
	_ = job
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
