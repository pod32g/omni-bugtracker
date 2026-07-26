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

// WakeSnoozedIssues clears every snooze that has come due and returns the ids, so the
// caller can notify. Claiming and reporting in one statement means two worker runs
// cannot both wake the same issue and notify twice.
func (s *Store) WakeSnoozedIssues(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE issues
		    SET snoozed_until = NULL, updated_at = now()
		  WHERE snoozed_until IS NOT NULL AND snoozed_until <= now() AND deleted_at IS NULL
		 RETURNING id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Activity is recorded after the claim rather than inside it: a failure here should
	// leave a woken issue with a missing timeline entry, not a woken issue that gets
	// woken again on the next run.
	for _, id := range ids {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
			 VALUES ($1, NULL, 'issue.woke', 'issue', $1, '{"reason":"snooze_expired"}')`, id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}
