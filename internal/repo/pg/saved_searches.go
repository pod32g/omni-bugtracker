package pg

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
)

// ── saved searches (personal named filters) ──

func (s *Store) ListSavedSearches(ctx context.Context, userID uuid.UUID) ([]domain.SavedSearch, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, query, created_at FROM saved_searches
		 WHERE user_id = $1 ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SavedSearch
	for rows.Next() {
		var ss domain.SavedSearch
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.Query, &ss.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}

// UpsertSavedSearch creates or replaces the user's filter with that name —
// re-saving under an existing name updates it, which is the natural UI flow.
func (s *Store) UpsertSavedSearch(ctx context.Context, userID uuid.UUID, name, query string) (domain.SavedSearch, error) {
	const q = `
		INSERT INTO saved_searches (user_id, name, query)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, name) DO UPDATE SET query = EXCLUDED.query
		RETURNING id, name, query, created_at`
	var ss domain.SavedSearch
	err := s.pool.QueryRow(ctx, q, userID, strings.TrimSpace(name), query).
		Scan(&ss.ID, &ss.Name, &ss.Query, &ss.CreatedAt)
	return ss, err
}

func (s *Store) DeleteSavedSearch(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_searches WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
