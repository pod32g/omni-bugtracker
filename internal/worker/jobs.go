package worker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/integrations"
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
	// TODO: parse `Fixes BUG-421` style refs (config close/link verbs), upsert commit/PR,
	// link to issues, and on merge + close-verb transition the issue + accrue release notes.
	w.d.Metrics.JobsProcessed.WithLabelValues("git_ingest", "noop").Inc()
	_ = ctx
	_ = job
	return nil
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
