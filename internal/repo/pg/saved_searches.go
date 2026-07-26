package pg

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── saved searches: personal named filters, and project-scoped shared views ──

const selectView = `
	SELECT ss.id, ss.name, ss.query, ss.description, ss.sort, ss.is_shared, ss.is_default,
	       COALESCE(p.key, ''), ss.position, u.display_name, u.email, ss.created_at
	  FROM saved_searches ss
	  LEFT JOIN projects p ON p.id = ss.project_id
	  LEFT JOIN users u ON u.id = ss.user_id`

func scanView(row scanner) (domain.SavedSearch, error) {
	var ss domain.SavedSearch
	var authorName, authorEmail *string
	err := row.Scan(&ss.ID, &ss.Name, &ss.Query, &ss.Description, &ss.Sort, &ss.IsShared,
		&ss.IsDefault, &ss.ProjectKey, &ss.Position, &authorName, &authorEmail, &ss.CreatedAt)
	if err != nil {
		return domain.SavedSearch{}, err
	}
	if authorName != nil {
		ss.Author = &domain.User{DisplayName: *authorName, Email: deref(authorEmail)}
	}
	return ss, nil
}

func collectViews(rows pgx.Rows) ([]domain.SavedSearch, error) {
	defer rows.Close()
	var out []domain.SavedSearch
	for rows.Next() {
		ss, err := scanView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}

// ListSavedSearches returns the caller's personal views only. Shared views are listed
// per project, because that is the scope they belong to.
func (s *Store) ListSavedSearches(ctx context.Context, userID uuid.UUID) ([]domain.SavedSearch, error) {
	rows, err := s.pool.Query(ctx, selectView+`
		WHERE ss.user_id = $1 AND NOT ss.is_shared ORDER BY ss.name`, userID)
	if err != nil {
		return nil, err
	}
	return collectViews(rows)
}

// ListProjectViews returns a project's shared views in their pinned order.
func (s *Store) ListProjectViews(ctx context.Context, projectKey string) ([]domain.SavedSearch, error) {
	rows, err := s.pool.Query(ctx, selectView+`
		WHERE ss.is_shared AND p.key = $1 ORDER BY ss.position, lower(ss.name)`, projectKey)
	if err != nil {
		return nil, err
	}
	return collectViews(rows)
}

// UpsertSavedSearch creates or replaces the caller's personal filter with that name —
// re-saving under an existing name updates it, which is the natural UI flow.
func (s *Store) UpsertSavedSearch(ctx context.Context, userID uuid.UUID, name, query string) (domain.SavedSearch, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO saved_searches (user_id, name, query)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, lower(name)) WHERE NOT is_shared
		 DO UPDATE SET query = EXCLUDED.query
		 RETURNING id`, userID, strings.TrimSpace(name), query).Scan(&id)
	if err != nil {
		return domain.SavedSearch{}, err
	}
	return scanView(s.pool.QueryRow(ctx, selectView+` WHERE ss.id = $1`, id))
}

// CreateProjectView creates a shared view. Position defaults to the end of the list, so
// a new view never silently displaces one people already reach for.
func (s *Store) CreateProjectView(ctx context.Context, in service.ProjectViewInput) (domain.SavedSearch, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO saved_searches (user_id, project_id, name, query, description, sort, is_shared, position)
		 SELECT $1, p.id, $3, $4, $5, $6, TRUE,
		        COALESCE((SELECT max(position) + 1 FROM saved_searches WHERE project_id = p.id AND is_shared), 0)
		   FROM projects p WHERE p.key = $2
		 RETURNING id`,
		in.AuthorID, in.ProjectKey, strings.TrimSpace(in.Name), in.Query, in.Description, in.Sort).Scan(&id)
	if err != nil {
		return domain.SavedSearch{}, err
	}
	return scanView(s.pool.QueryRow(ctx, selectView+` WHERE ss.id = $1`, id))
}

// UpdateProjectView applies a partial edit to a shared view; nil fields are unchanged.
//
// Setting is_default clears the previous default first: the unique index guarantees at
// most one, so without the clear the second attempt would just fail.
func (s *Store) UpdateProjectView(ctx context.Context, in service.UpdateProjectViewInput) (domain.SavedSearch, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SavedSearch{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if in.IsDefault != nil && *in.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE saved_searches SET is_default = FALSE
			  WHERE is_default AND project_id = (SELECT project_id FROM saved_searches WHERE id = $1)`,
			in.ID); err != nil {
			return domain.SavedSearch{}, err
		}
	}
	tag, err := tx.Exec(ctx,
		`UPDATE saved_searches SET
		   name        = COALESCE($2, name),
		   query       = COALESCE($3, query),
		   description = COALESCE($4, description),
		   sort        = COALESCE($5, sort),
		   position    = COALESCE($6, position),
		   is_default  = COALESCE($7, is_default)
		 WHERE id = $1 AND is_shared`,
		in.ID, in.Name, in.Query, in.Description, in.Sort, in.Position, in.IsDefault)
	if err != nil {
		return domain.SavedSearch{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.SavedSearch{}, pgx.ErrNoRows
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SavedSearch{}, err
	}
	return scanView(s.pool.QueryRow(ctx, selectView+` WHERE ss.id = $1`, in.ID))
}

// ShareSavedSearch promotes a personal view into a project's shared list, keeping the
// same row so the author and creation date survive the promotion.
func (s *Store) ShareSavedSearch(ctx context.Context, userID, id uuid.UUID, projectKey string) (domain.SavedSearch, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE saved_searches ss
		    SET is_shared = TRUE,
		        project_id = (SELECT id FROM projects WHERE key = $3),
		        position = COALESCE((SELECT max(position) + 1 FROM saved_searches x
		                              WHERE x.project_id = (SELECT id FROM projects WHERE key = $3)
		                                AND x.is_shared), 0)
		  WHERE ss.id = $1 AND ss.user_id = $2 AND NOT ss.is_shared`, id, userID, projectKey)
	if err != nil {
		return domain.SavedSearch{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.SavedSearch{}, pgx.ErrNoRows
	}
	return scanView(s.pool.QueryRow(ctx, selectView+` WHERE ss.id = $1`, id))
}

// DeleteSavedSearch removes a personal view the caller owns.
func (s *Store) DeleteSavedSearch(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_searches WHERE id = $1 AND user_id = $2 AND NOT is_shared`, id, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteProjectView removes a shared view. Ownership is the project's, not the
// creator's, so the permission check lives in the handler rather than here.
func (s *Store) DeleteProjectView(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM saved_searches WHERE id = $1 AND is_shared`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// GetViewProjectKey returns the project a shared view belongs to, for permission checks.
func (s *Store) GetViewProjectKey(ctx context.Context, id uuid.UUID) (string, error) {
	var key string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(p.key, '') FROM saved_searches ss
		   LEFT JOIN projects p ON p.id = ss.project_id
		  WHERE ss.id = $1`, id).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pgx.ErrNoRows
	}
	return key, err
}
