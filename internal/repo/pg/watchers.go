package pg

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
)

// ── issue watchers ──

// addWatcher subscribes a user inside an existing transaction (used by the
// auto-watch hooks on create/comment/assign). Idempotent.
func addWatcher(ctx context.Context, tx pgx.Tx, issueID, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO issue_watchers (issue_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		issueID, userID)
	return err
}

func (s *Store) ListWatchers(ctx context.Context, issueID uuid.UUID) ([]domain.User, error) {
	const q = `
		SELECT u.id, u.email, u.display_name, u.avatar_url
		FROM issue_watchers w JOIN users u ON u.id = w.user_id
		WHERE w.issue_id = $1 ORDER BY w.created_at`
	rows, err := s.pool.Query(ctx, q, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) IsWatcher(ctx context.Context, issueID, userID uuid.UUID) (bool, error) {
	var watching bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM issue_watchers WHERE issue_id = $1 AND user_id = $2)`,
		issueID, userID).Scan(&watching)
	return watching, err
}

func (s *Store) SetWatcher(ctx context.Context, issueID, userID uuid.UUID, watching bool) error {
	if watching {
		_, err := s.pool.Exec(ctx,
			`INSERT INTO issue_watchers (issue_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			issueID, userID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM issue_watchers WHERE issue_id = $1 AND user_id = $2`, issueID, userID)
	return err
}
