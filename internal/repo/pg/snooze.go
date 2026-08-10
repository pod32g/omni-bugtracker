package pg

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// SetIssueSnooze sets or clears snoozed_until. A nil `until` wakes the issue now.
//
// The note is kept with the snooze so the reason survives the three weeks — "revisit
// after the migration" is worth more later than the date alone.
func (s *Store) SetIssueSnooze(
	ctx context.Context, id, actor uuid.UUID, until *time.Time, note string, publish service.PublishFn,
) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE issues SET snoozed_until = $2, snooze_note = $3, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL`, id, until, note)
	if err != nil {
		return domain.Issue{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Issue{}, pgx.ErrNoRows
	}

	verb, changes := "issue.woke", []byte("{}")
	if until != nil {
		verb = "issue.snoozed"
		changes, err = json.Marshal(map[string]string{
			"until": until.UTC().Format(time.RFC3339), "note": note,
		})
		if err != nil {
			return domain.Issue{}, err
		}
	}
	if err := recordActivityChanges(ctx, tx, id, actor, verb, "issue", id, changes); err != nil {
		return domain.Issue{}, err
	}
	if publish != nil {
		if err := publish(tx); err != nil {
			return domain.Issue{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Issue{}, err
	}
	return scanIssue(s.pool.QueryRow(ctx, selectLiveIssue, id))
}

// WakeSnoozedIssues clears every snooze that has come due and returns the ids.
// Claiming and reporting in one statement means two worker runs cannot both wake the
// same issue and notify twice.
//
// enqueue runs inside the same transaction. Waking is a claim like any other — the
// snooze is cleared, and the row will never come due again — so announcing it
// afterwards meant a failed insert silently swallowed the wake: the issue is back in
// somebody's queue and nothing ever said so. Rolling the claim back instead leaves it
// to come due again on the next tick.
func (s *Store) WakeSnoozedIssues(
	ctx context.Context, enqueue func(pgx.Tx, []uuid.UUID) error,
) ([]uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx,
		`UPDATE issues
		    SET snoozed_until = NULL, updated_at = now()
		  WHERE snoozed_until IS NOT NULL AND snoozed_until <= now() AND deleted_at IS NULL
		 RETURNING id`)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return nil, tx.Commit(ctx)
	}

	// The timeline entry now shares the transaction too. It was outside on the
	// argument that a missing entry beats a re-wake, but that trade does not exist
	// any more: rolling back means the issue simply comes due again.
	for _, id := range ids {
		if _, err := tx.Exec(ctx,
			`INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
			 VALUES ($1, NULL, 'issue.woke', 'issue', $1, '{"reason":"snooze_expired"}')`, id); err != nil {
			return nil, err
		}
	}
	if enqueue != nil {
		if err := enqueue(tx, ids); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}
