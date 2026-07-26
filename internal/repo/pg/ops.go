package pg

import (
	"context"

	"github.com/omni/bugtracker/internal/domain"
)

// OpsSnapshot reads River's own tables plus the webhook delivery log.
//
// River state lives in Postgres, so the queue is observable with a query rather than a
// client connection — which matters because the API process does not run the workers and
// should not have to talk to them to say how they are doing.
func (s *Store) OpsSnapshot(ctx context.Context) (domain.OpsSnapshot, error) {
	var ops domain.OpsSnapshot

	rows, err := s.pool.Query(ctx,
		`SELECT queue, state::text, count(*), max(attempt)
		   FROM river_job GROUP BY 1, 2 ORDER BY 1, 2`)
	if err != nil {
		// River's tables are created by its own migration. A tracker running without
		// them is misconfigured, not broken, and the page should say so rather than
		// 500 — an ops page that cannot load when something is wrong is the one page
		// that must not do that.
		ops.QueueError = err.Error()
	} else {
		for rows.Next() {
			var q domain.QueueState
			var maxAttempt *int
			if err := rows.Scan(&q.Queue, &q.State, &q.Count, &maxAttempt); err != nil {
				rows.Close()
				return ops, err
			}
			if maxAttempt != nil {
				q.MaxAttempt = *maxAttempt
			}
			ops.Queues = append(ops.Queues, q)
		}
		rows.Close()
	}

	// The last handful of failures, with the error River recorded. Discarded jobs are
	// the ones nobody will retry, so they lead.
	rows, err = s.pool.Query(ctx, `
		SELECT kind, state::text, attempt,
		       COALESCE(errors[array_length(errors, 1)] ->> 'error', ''), finalized_at, created_at
		  FROM river_job
		 WHERE state IN ('discarded', 'retryable')
		 ORDER BY (state = 'discarded') DESC, created_at DESC
		 LIMIT 15`)
	if err == nil {
		for rows.Next() {
			var f domain.JobFailure
			if err := rows.Scan(&f.Kind, &f.State, &f.Attempt, &f.Error, &f.FinalizedAt, &f.CreatedAt); err != nil {
				rows.Close()
				return ops, err
			}
			ops.Failures = append(ops.Failures, f)
		}
		rows.Close()
	}

	// Webhook delivery health over the last day, promoted out of the per-webhook page.
	rows, err = s.pool.Query(ctx, `
		SELECT w.url, count(*) FILTER (WHERE d.status = 'success'), count(*),
		       max(d.created_at)
		  FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
		 WHERE d.created_at > now() - interval '1 day'
		 GROUP BY w.url ORDER BY count(*) DESC LIMIT 20`)
	if err != nil {
		return ops, err
	}
	defer rows.Close()
	for rows.Next() {
		var d domain.DeliveryHealth
		if err := rows.Scan(&d.URL, &d.Succeeded, &d.Total, &d.LastAt); err != nil {
			return ops, err
		}
		ops.Deliveries = append(ops.Deliveries, d)
	}
	return ops, rows.Err()
}
