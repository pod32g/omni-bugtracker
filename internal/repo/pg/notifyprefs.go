package pg

import (
	"context"

	"github.com/google/uuid"
)

// GetNotificationPrefs returns a user's explicit channel choices. Absent events are
// absent from the map — the caller falls back to the defaults, which is what lets a
// change to those defaults reach everyone who never disagreed with them.
func (s *Store) GetNotificationPrefs(ctx context.Context, userID uuid.UUID) (map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT event_type, channel FROM notification_prefs WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var event, channel string
		if err := rows.Scan(&event, &channel); err != nil {
			return nil, err
		}
		out[event] = channel
	}
	return out, rows.Err()
}

// SetNotificationPrefs replaces a user's choices. An event set back to its default is
// deleted rather than stored, so the table only ever holds deliberate disagreement.
func (s *Store) SetNotificationPrefs(
	ctx context.Context, userID uuid.UUID, prefs, defaults map[string]string,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for event, channel := range prefs {
		if defaults[event] == channel {
			if _, err := tx.Exec(ctx,
				`DELETE FROM notification_prefs WHERE user_id = $1 AND event_type = $2`,
				userID, event); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO notification_prefs (user_id, event_type, channel) VALUES ($1,$2,$3)
			 ON CONFLICT (user_id, event_type)
			 DO UPDATE SET channel = EXCLUDED.channel, updated_at = now()`,
			userID, event, channel); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetIssueMute mutes or unmutes one issue for one user.
func (s *Store) SetIssueMute(ctx context.Context, issueID, userID uuid.UUID, muted bool) error {
	if !muted {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM issue_mutes WHERE issue_id = $1 AND user_id = $2`, issueID, userID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO issue_mutes (issue_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		issueID, userID)
	return err
}

// RouteRecipients splits candidate recipients into those who want this event in their
// inbox and those who want it pushed outward, applying per-user preferences and per-issue
// mutes in one query.
//
// Preferences are resolved server-side, once, rather than in each delivery path: the
// inbox writer and the outbound adapter must not be able to disagree about who asked for
// what. `defaults` is passed in because the defaults live in the service layer next to
// the reasoning for them, not duplicated in SQL.
func (s *Store) RouteRecipients(
	ctx context.Context, issueID uuid.UUID, eventType string, emails []string, defaults map[string]string,
) (inbox, push []string, err error) {
	if len(emails) == 0 {
		return nil, nil, nil
	}
	fallback := defaults[eventType]
	if fallback == "" {
		fallback = "inbox"
	}

	rows, err := s.pool.Query(ctx,
		`SELECT u.email, COALESCE(np.channel, $4)
		   FROM users u
		   LEFT JOIN notification_prefs np ON np.user_id = u.id AND np.event_type = $3
		  WHERE u.email = ANY($2::text[])
		    -- A mute silences the issue without unwatching it, so the watcher list is
		    -- unchanged and the replies stop.
		    AND NOT EXISTS (SELECT 1 FROM issue_mutes m WHERE m.issue_id = $1 AND m.user_id = u.id)`,
		issueID, emails, eventType, fallback)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var email, channel string
		if err := rows.Scan(&email, &channel); err != nil {
			return nil, nil, err
		}
		switch channel {
		case "inbox":
			inbox = append(inbox, email)
		case "push":
			push = append(push, email)
		case "both":
			inbox = append(inbox, email)
			push = append(push, email)
		}
	}
	return inbox, push, rows.Err()
}
