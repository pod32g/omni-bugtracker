package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	w.logEvent(ctx, ev)

	// Notify on every issue event; automation always evaluates.
	//
	// There is no search-index fan-out: search is Postgres FTS over generated tsvector
	// columns, maintained by the database itself, so there is nothing to project.
	if ev.IssueID != "" {
		if _, err := client.Insert(ctx, events.NotifyJobArgs{
			EventType: ev.EventType, IssueID: ev.IssueID, ActorID: ev.ActorID,
		}, nil); err != nil {
			return err
		}
		if _, err := client.Insert(ctx, events.AutomationJobArgs{
			EventType: ev.EventType, IssueID: ev.IssueID, ActorID: ev.ActorID,
		}, nil); err != nil {
			return err
		}
		if err := w.fanOutMentions(ctx, client, ev); err != nil {
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

// logEvent writes the domain event itself to the log — the record of what the tracker
// actually did, as opposed to which HTTP endpoints were touched doing it.
//
// The outbox already carries every meaningful thing that happens (an issue filed, a
// status moved, a comment posted, an SLA breached) and the dispatcher sees all of it,
// so this is the one place that can narrate the system in domain terms. It used to log
// "dispatch" with a bare issue UUID, which is why the log read as machine exhaust: the
// events were there, we just never wrote them down in a form anyone could follow.
//
// The key lookup costs one indexed read on an already low-volume path (domain events
// fire on real activity, and this job inserts several others anyway). Worth it: "BUG-64"
// is legible where "60f375df-1341-4ef4-9f8a-01a851e33860" is not. A missing issue is not
// worth a word — it was deleted between the write and this job, which is ordinary.
func (w *eventWorker) logEvent(ctx context.Context, ev events.DomainEventArgs) {
	attrs := []any{"event", ev.EventType}
	if id, err := uuid.Parse(ev.IssueID); err == nil {
		if issue, err := w.d.Store.GetIssueByID(ctx, id); err == nil {
			attrs = append(attrs, "issue", issue.Key, "status", issue.Status)
		}
	}
	if ev.ActorID != "" {
		attrs = append(attrs, "actor", ev.ActorID)
	}
	w.d.Logger.Info(ev.EventType, attrs...)
}

// fanOutMentions turns newly recorded @mentions into their own notification, separate
// from the watcher fan-out above. Being named is a different signal from "something
// happened on an issue you follow", and collapsing the two is how a mention becomes
// just more noise.
//
// Claiming is what makes it exactly-once: the same write produces several events
// (issue.updated and comment.created both fire), and every one of them runs this.
func (w *eventWorker) fanOutMentions(ctx context.Context, client *river.Client[pgx.Tx], ev events.DomainEventArgs) error {
	issueID, err := uuid.Parse(ev.IssueID)
	if err != nil {
		return nil //nolint:nilerr
	}
	recipients, err := w.d.Store.ClaimPendingMentions(ctx, issueID)
	if err != nil || len(recipients) == 0 {
		return err
	}
	_, err = client.Insert(ctx, events.NotifyJobArgs{
		EventType:  events.UserMentioned,
		IssueID:    ev.IssueID,
		ActorID:    ev.ActorID,
		Recipients: recipients,
	}, nil)
	return err
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
	issueID, err := uuid.Parse(job.Args.IssueID)
	if err != nil {
		return nil //nolint:nilerr
	}
	issue, err := w.d.Store.GetIssueByID(ctx, issueID)
	if err != nil {
		return nil //nolint:nilerr // deleted before the job ran — nothing to say
	}

	// Recipients = watchers minus the actor (people don't need to hear about
	// their own edits). Included as labels so Omni-Notify routes/templates can
	// use them; concrete delivery channels are configured in Omni-Notify.
	//
	// An explicit list on the job wins: a mention is addressed to the people named,
	// not to everyone watching. The field existed unused until mentions needed it.
	recipients := job.Args.Recipients
	if len(recipients) == 0 {
		if watchers, err := w.d.Store.ListWatchers(ctx, issue.ID); err == nil {
			for _, u := range watchers {
				if u.ID.String() != job.Args.ActorID {
					recipients = append(recipients, u.Email)
				}
			}
		}
	}

	// Preferences and per-issue mutes are applied once, server-side, and split the
	// audience by channel — so the inbox writer and the outbound adapter cannot
	// disagree about who asked for what.
	inboxTo, pushTo, err := w.d.Store.RouteRecipients(
		ctx, issue.ID, job.Args.EventType, recipients, service.DefaultChannels)
	if err != nil {
		// Routing is a read; failing it should not drop the notification entirely.
		w.d.Logger.Error("route recipients", "err", err, "issue", issue.Key)
		inboxTo, pushTo = recipients, recipients
	}

	// The inbox is written by the same step that pushes outward, so the two can never
	// disagree about who was told. It is deliberately not conditional on the external
	// adapter succeeding — Omni-Notify being unreachable is exactly when the in-app
	// list is the only thing that works.
	var actor *uuid.UUID
	if id, err := uuid.Parse(job.Args.ActorID); err == nil {
		actor = &id
	}
	if err := w.d.Store.RecordNotifications(ctx, issue.ID, job.Args.EventType, actor, inboxTo); err != nil {
		w.d.Logger.Error("record notifications", "err", err, "issue", issue.Key)
	}

	// Nobody wants this pushed: stop here rather than posting an event with an empty
	// recipient list, which Omni-Notify would route by its own rules.
	if len(pushTo) == 0 {
		return nil
	}
	recipients = pushTo

	severity := "info"
	if issue.Severity != nil {
		switch *issue.Severity {
		case domain.SeverityCritical:
			severity = "critical"
		case domain.SeverityHigh:
			severity = "error"
		case domain.SeverityMedium:
			severity = "warning"
		}
	}
	status := "firing"
	if job.Args.EventType == events.IssueResolved || job.Args.EventType == events.IssueClosed ||
		job.Args.EventType == events.IssueDeleted {
		status = "resolved"
	}

	labels := map[string]string{
		"service": "omni-bugtracker",
		"project": issue.ProjectKey,
		"issue":   issue.Key,
		"event":   job.Args.EventType,
		"status":  string(issue.Status),
	}
	if issue.Assignee != nil {
		labels["assignee"] = issue.Assignee.Email
	}
	if len(recipients) > 0 {
		labels["recipients"] = strings.Join(recipients, ",")
	}

	ev := integrations.NotifyEvent{
		// Fingerprint on issue+event so repeats inside Omni-Notify's dedupe
		// window collapse (e.g. rapid successive edits).
		EventID:   issue.Key + ":" + job.Args.EventType,
		Type:      "issue",
		Source:    "omni-bugtracker",
		Status:    status,
		Severity:  severity,
		Title:     "[" + issue.Key + "] " + issue.Title,
		Summary:   humanEventSummary(job.Args.EventType, issue),
		Labels:    labels,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	return softFail(w.d, "notify", w.d.Adapters.Notify.Notify(ctx, ev))
}

func humanEventSummary(eventType string, issue domain.Issue) string {
	switch eventType {
	case events.IssueCreated:
		return "New " + string(issue.Type) + " reported in " + issue.ProjectKey
	case events.IssueResolved:
		return issue.Key + " was resolved"
	case events.IssueClosed:
		return issue.Key + " was closed"
	case events.IssueReopened:
		return issue.Key + " was reopened"
	case events.UserMentioned:
		return "You were mentioned on " + issue.Key
	case events.IssueCommented:
		return "New comment on " + issue.Key
	case events.IssueStatusChanged:
		return issue.Key + " moved to " + string(issue.Status)
	case events.IssueSLAWarning:
		return issue.Key + " is approaching its SLA target"
	case events.IssueSLABreached:
		return issue.Key + " has breached its SLA target"
	default:
		return issue.Key + " was updated"
	}
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
	// A hook that has run out of retries is silently no longer integrated with anything.
	// That was recorded in webhook_deliveries and nowhere else, so it was only ever found
	// by someone going to look. Retries in progress are a warning; giving up is an error.
	level := slog.LevelWarn
	if status == "dead" {
		level = slog.LevelError
	}
	w.d.Logger.Log(ctx, level, "webhook_delivery_"+status,
		"webhook", job.Args.WebhookID, "event", job.Args.EventType,
		"url", url, "attempt", job.Attempt, "code", code, "err", err)
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

// automationWorker evaluates automation rules against the event.
type automationWorker struct {
	river.WorkerDefaults[events.AutomationJobArgs]
	d Deps
}

const automationBotSub = "automation|system"

type ruleTrigger struct {
	Event      string `json:"event"`
	Conditions struct {
		Type      string `json:"type,omitempty"`
		Severity  string `json:"severity,omitempty"`
		Priority  string `json:"priority,omitempty"`
		Label     string `json:"label,omitempty"`
		Component string `json:"component,omitempty"`
		Source    string `json:"source,omitempty"`
	} `json:"conditions"`
}

type ruleAction struct {
	Kind  string `json:"kind"` // set_priority|set_severity|set_assignee|add_label|set_status|add_comment
	Value string `json:"value"`
}

func (w *automationWorker) Work(ctx context.Context, job *river.Job[events.AutomationJobArgs]) error {
	botID, err := w.d.Store.EnsureBotUser(ctx, automationBotSub, "Automation", "automation@system.local")
	if err != nil {
		return err
	}
	// Loop guard: never evaluate events the automation bot itself caused.
	if job.Args.ActorID == botID.String() {
		w.d.Metrics.JobsProcessed.WithLabelValues("automation", "self_skip").Inc()
		return nil
	}
	issueID, err := uuid.Parse(job.Args.IssueID)
	if err != nil {
		return nil //nolint:nilerr
	}
	issue, err := w.d.Store.GetIssueByID(ctx, issueID)
	if err != nil {
		return nil //nolint:nilerr // deleted before evaluation
	}
	rules, err := w.d.Store.MatchingAutomationRules(ctx, issue.ProjectKey, job.Args.EventType)
	if err != nil {
		return err
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	publish := func(tx pgx.Tx) error {
		_, err := client.InsertTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueUpdated, IssueID: issue.ID.String(), ActorID: botID.String(),
		}, nil)
		return err
	}

	for _, rule := range rules {
		var trig ruleTrigger
		if err := json.Unmarshal(rule.Trigger, &trig); err != nil || !conditionsMatch(trig, issue) {
			continue
		}
		var actions []ruleAction
		if err := json.Unmarshal(rule.Actions, &actions); err != nil {
			_ = w.d.Store.RecordAutomationRun(ctx, rule.ID, issue.ID, "error",
				[]byte(`{"error":"bad actions json"}`))
			continue
		}
		applied, actErr := w.applyActions(ctx, issue, actions, botID, publish)
		status, logLine := "matched", `{"actions_applied":`+strconv.Itoa(applied)+`}`
		if actErr != nil {
			status = "error"
			logJSON, _ := json.Marshal(map[string]any{"actions_applied": applied, "error": actErr.Error()})
			logLine = string(logJSON)
		}
		_ = w.d.Store.RecordAutomationRun(ctx, rule.ID, issue.ID, status, []byte(logLine))
		w.d.Metrics.JobsProcessed.WithLabelValues("automation", status).Inc()
		// A rule firing changes an issue with no human behind it, so it is exactly the
		// kind of thing you go to the log to explain afterwards ("why did this reassign
		// itself?"). The automation_runs table has it, but only if you know to look.
		if actErr != nil {
			w.d.Logger.Error("automation_rule_failed", "rule", rule.Name, "issue", issue.Key,
				"event", job.Args.EventType, "actions_applied", applied, "err", actErr)
		} else if applied > 0 {
			w.d.Logger.Info("automation_rule_applied", "rule", rule.Name, "issue", issue.Key,
				"event", job.Args.EventType, "actions_applied", applied)
		}
		// Refresh the issue so subsequent rules see prior rules' effects.
		if updated, err := w.d.Store.GetIssueByID(ctx, issue.ID); err == nil {
			issue = updated
		}
	}
	return nil
}

func conditionsMatch(t ruleTrigger, issue domain.Issue) bool {
	c := t.Conditions
	if c.Type != "" && string(issue.Type) != c.Type {
		return false
	}
	if c.Severity != "" && (issue.Severity == nil || string(*issue.Severity) != c.Severity) {
		return false
	}
	if c.Priority != "" && string(issue.Priority) != c.Priority {
		return false
	}
	if c.Source != "" && string(issue.Source) != c.Source {
		return false
	}
	if c.Label != "" && !containsFold(issue.Labels, c.Label) {
		return false
	}
	if c.Component != "" && !containsFold(issue.Components, c.Component) {
		return false
	}
	return true
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func (w *automationWorker) applyActions(ctx context.Context, issue domain.Issue, actions []ruleAction,
	botID uuid.UUID, publish service.PublishFn) (int, error) {
	applied := 0
	for _, a := range actions {
		var err error
		switch a.Kind {
		case "set_priority":
			p := domain.Priority(a.Value)
			_, err = w.d.Store.UpdateIssue(ctx, issue.ID, botID, service.UpdateIssueInput{Priority: &p}, publish)
		case "set_severity":
			s := domain.Severity(a.Value)
			_, err = w.d.Store.UpdateIssue(ctx, issue.ID, botID, service.UpdateIssueInput{Severity: &s}, publish)
		case "set_assignee":
			var uid uuid.UUID
			if uid, err = uuid.Parse(a.Value); err == nil {
				_, err = w.d.Store.UpdateIssue(ctx, issue.ID, botID, service.UpdateIssueInput{AssigneeID: &uid}, publish)
			}
		case "add_label":
			merged := append(append([]string{}, issue.Labels...), a.Value)
			_, err = w.d.Store.UpdateIssue(ctx, issue.ID, botID, service.UpdateIssueInput{Labels: &merged}, publish)
		case "set_status":
			to := domain.IssueStatus(a.Value)
			if !domain.CanTransition(issue.Status, to) {
				err = errors.New("invalid transition " + string(issue.Status) + " -> " + a.Value)
			} else {
				_, err = w.d.Store.TransitionIssue(ctx, issue.ID, to, botID, publish)
			}
		case "add_comment":
			_, err = w.d.Store.AddComment(ctx, issue.ID, botID, a.Value, publish)
		default:
			err = errors.New("unknown action kind " + a.Kind)
		}
		if err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// autoArchiveWorker archives issues closed more than archive.auto_after_days ago. It
// runs daily (a River periodic job) and is a no-op when the setting is 0.
type autoArchiveWorker struct {
	river.WorkerDefaults[events.AutoArchiveArgs]
	d Deps
}

// wakeSnoozedWorker clears snoozes that have come due and tells the people who care.
//
// Runs every 15 minutes rather than daily: "snooze until tomorrow morning" is a promise
// about a time, and a day-granularity sweep would break it by up to 24 hours.
type wakeSnoozedWorker struct {
	river.WorkerDefaults[events.WakeSnoozedArgs]
	d Deps
}

func (w *wakeSnoozedWorker) Work(ctx context.Context, _ *river.Job[events.WakeSnoozedArgs]) error {
	ids, err := w.d.Store.WakeSnoozedIssues(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	w.d.Logger.Info("woke snoozed issues", "count", len(ids))

	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, id := range ids {
		// A woken issue is back in everyone's queue, which is the whole point of the
		// snooze — so say so, through the same fan-out every other event uses.
		if _, err := client.Insert(ctx, events.DomainEventArgs{
			EventType: events.IssueWoke, IssueID: id.String(),
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// slaSweepWorker escalates issues that have crossed an SLA threshold.
//
// The claim happens in the store, in one statement, so this loop only ever sees
// crossings nobody has announced yet — a re-run after a crash re-announces nothing.
type slaSweepWorker struct {
	river.WorkerDefaults[events.SLASweepArgs]
	d Deps
}

func (w *slaSweepWorker) Work(ctx context.Context, _ *river.Job[events.SLASweepArgs]) error {
	claims, err := w.d.Store.ClaimSLAEscalations(ctx)
	if err != nil {
		return err
	}
	if len(claims) == 0 {
		return nil
	}
	w.d.Logger.Info("sla sweep", "escalations", len(claims))

	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, c := range claims {
		eventType := events.IssueSLAWarning
		if strings.HasSuffix(c.Kind, "_breached") {
			eventType = events.IssueSLABreached
		}
		// The kind ("response_warning") rides in the payload rather than in the event
		// type: a webhook subscriber filtering on issue.sla_breached wants both
		// targets, and one that cares which can read it.
		payload, err := json.Marshal(map[string]string{"target": c.Kind})
		if err != nil {
			return err
		}
		if _, err := client.Insert(ctx, events.DomainEventArgs{
			EventType: eventType, IssueID: c.IssueID.String(), Payload: payload,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// iterationSnapshotWorker records today's remaining work per active iteration.
//
// The burndown is stored rather than derived because deriving it would redraw history
// every time an issue was re-estimated — see the migration. That only holds if this
// actually runs, so it upserts on (iteration, date): a missed run can be made up by
// the next one, and a double run overwrites rather than duplicating.
type iterationSnapshotWorker struct {
	river.WorkerDefaults[events.IterationSnapshotArgs]
	d Deps
}

func (w *iterationSnapshotWorker) Work(ctx context.Context, _ *river.Job[events.IterationSnapshotArgs]) error {
	n, err := w.d.Store.SnapshotIterations(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		w.d.Logger.Info("iteration snapshot", "iterations", n)
	}
	return nil
}

func (w *autoArchiveWorker) Work(ctx context.Context, _ *river.Job[events.AutoArchiveArgs]) error {
	// Read live: the Settings value (DB) overrides the bootstrap config default, so an
	// admin can toggle auto-archive without restarting the worker.
	days, err := service.EffectiveArchiveDays(ctx, w.d.Store, w.d.Cfg)
	if err != nil {
		return err
	}
	if days <= 0 {
		return nil
	}
	botID, err := w.d.Store.EnsureBotUser(ctx, "system:auto-archive", "System", "system@system.local")
	if err != nil {
		return err
	}
	n, err := w.d.Store.ArchiveStaleClosed(ctx, days, botID)
	if err != nil {
		return err
	}
	if n > 0 {
		w.d.Logger.Info("auto-archive", "archived", n, "closed_older_than_days", days)
	}
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
