package pg

import (
	"context"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
)

// RecordNotifications writes one inbox row per recipient of a notify job.
//
// Recipients arrive as email addresses because that is what the outbound adapter needs;
// resolving them back to users here keeps the worker's shape unchanged and means the
// inbox and the external push always agree on who was told.
//
// The actor is excluded in SQL as well as by the caller: the watcher list already drops
// them, but the explicit-recipient path (mentions) does not, and nobody needs an inbox
// entry about their own edit.
func (s *Store) RecordNotifications(
	ctx context.Context, issueID uuid.UUID, eventType string, actorID *uuid.UUID, emails []string,
) error {
	if len(emails) == 0 {
		return nil
	}
	// ON CONFLICT collapses repeats: a second comment on an issue you have not looked
	// at yet refreshes the existing row rather than adding a second one. The partial
	// unique index it targets only covers unread rows, so once you have read it the
	// next event starts a new one.
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notifications (user_id, issue_id, event_type, actor_id)
		 SELECT u.id, $1::uuid, $2::text, $3::uuid
		   FROM users u
		  WHERE u.email = ANY($4::text[]) AND u.id IS DISTINCT FROM $3::uuid
		 ON CONFLICT (user_id, issue_id, event_type) WHERE read_at IS NULL
		 DO UPDATE SET created_at = now(), actor_id = EXCLUDED.actor_id`,
		issueID, eventType, actorID, emails)
	return err
}

// ListNotifications returns one page of a user's inbox plus their unread total, so the
// badge and the list come from one round trip and cannot disagree.
func (s *Store) ListNotifications(
	ctx context.Context, userID uuid.UUID, unreadOnly bool, limit, offset int32,
) ([]domain.Notification, int, error) {
	var unread int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).
		Scan(&unread); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT n.id, n.event_type, n.read_at, n.created_at,
		        p.key || '-' || i.number, i.title, i.status::text,
		        a.display_name, a.email
		   FROM notifications n
		   JOIN issues i ON i.id = n.issue_id
		   JOIN projects p ON p.id = i.project_id
		   LEFT JOIN users a ON a.id = n.actor_id
		  WHERE n.user_id = $1 AND i.deleted_at IS NULL
		    AND ($2 = FALSE OR n.read_at IS NULL)
		  ORDER BY n.created_at DESC
		  LIMIT $3 OFFSET $4`, userID, unreadOnly, clampLimit(limit), clampOffset(offset))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.Notification
	for rows.Next() {
		var n domain.Notification
		var actorName, actorEmail *string
		if err := rows.Scan(&n.ID, &n.EventType, &n.ReadAt, &n.CreatedAt,
			&n.IssueKey, &n.IssueTitle, &n.IssueStatus, &actorName, &actorEmail); err != nil {
			return nil, 0, err
		}
		if actorName != nil {
			n.Actor = &domain.User{DisplayName: *actorName, Email: deref(actorEmail)}
		}
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

// MarkNotificationsRead marks specific rows read, or every unread row when ids is empty.
// Scoped to the user in SQL so one caller can never mark another's inbox read.
func (s *Store) MarkNotificationsRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now()
		  WHERE user_id = $1 AND read_at IS NULL
		    AND (cardinality($2::uuid[]) = 0 OR id = ANY($2::uuid[]))`, userID, ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// MarkIssueNotificationsRead clears a user's unread rows for one issue — opening the
// issue is the same act as reading the notification about it, and a badge that survives
// that is a badge people learn to ignore.
func (s *Store) MarkIssueNotificationsRead(ctx context.Context, userID, issueID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now()
		  WHERE user_id = $1 AND issue_id = $2 AND read_at IS NULL`, userID, issueID)
	return err
}
