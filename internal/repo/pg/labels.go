package pg

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── labels ──
//
// Labels are created implicitly by typing them on an issue (see ensureLabels), which
// is the right default and also how a project ends up with "regresion", "regression"
// and "Regression". These are the curation operations: rename, recolour, delete, and
// merge one into another.

const selectLabel = `
	SELECT l.id, l.name, l.color, l.description,
	       (SELECT count(*) FROM issue_labels il WHERE il.label_id = l.id)::int,
	       l.project_id IS NULL
	  FROM labels l`

func scanLabel(row scanner) (domain.Label, error) {
	var l domain.Label
	err := row.Scan(&l.ID, &l.Name, &l.Color, &l.Description, &l.IssueCount, &l.IsGlobal)
	return l, err
}

// ListLabels returns the labels usable in one project: its own, plus the global ones.
func (s *Store) ListLabels(ctx context.Context, projectKey string) ([]domain.Label, error) {
	rows, err := s.pool.Query(ctx, selectLabel+`
		LEFT JOIN projects p ON p.id = l.project_id
		 WHERE l.project_id IS NULL OR p.key = $1
		 ORDER BY lower(l.name)`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Label
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LabelScope returns the project key owning a label. A found global label reports an
// empty key, so callers must distinguish "global" from "no such label" by the bool.
func (s *Store) LabelScope(ctx context.Context, id uuid.UUID) (string, bool, error) {
	var key *string
	err := s.pool.QueryRow(ctx,
		`SELECT p.key FROM labels l LEFT JOIN projects p ON p.id = l.project_id WHERE l.id = $1`, id).
		Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return deref(key), true, nil
}

// CreateLabel adds a label up front rather than waiting for someone to type it on an
// issue. An empty ProjectKey creates a global label.
func (s *Store) CreateLabel(ctx context.Context, in service.CreateLabelInput) (domain.Label, error) {
	var projectID *uuid.UUID
	if in.ProjectKey != "" {
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM projects WHERE key = $1`, in.ProjectKey).Scan(&id); err != nil {
			return domain.Label{}, err
		}
		projectID = &id
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO labels (project_id, name, color, description)
		 VALUES ($1, $2, COALESCE(NULLIF($3, ''), '#8b5cf6'), $4)
		 RETURNING id`, projectID, in.Name, in.Color, in.Description).Scan(&id)
	if err != nil {
		return domain.Label{}, err
	}
	return s.getLabel(ctx, id)
}

func (s *Store) UpdateLabel(ctx context.Context, in service.UpdateLabelInput) (domain.Label, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE labels
		    SET name        = COALESCE($2, name),
		        color       = COALESCE(NULLIF($3, ''), color),
		        description = COALESCE($4, description)
		  WHERE id = $1`, in.ID, in.Name, in.Color, in.Description)
	if err != nil {
		return domain.Label{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Label{}, pgx.ErrNoRows
	}
	return s.getLabel(ctx, in.ID)
}

// DeleteLabel removes the label; issue_labels rows go with it by cascade, so the
// issues survive and simply stop carrying it.
func (s *Store) DeleteLabel(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM labels WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MergeLabels repoints every issue carrying source at target, then drops source.
// Issues that already carry target keep one row rather than colliding on the PK,
// which is what makes this safe to run on overlapping label sets.
func (s *Store) MergeLabels(ctx context.Context, sourceID, targetID uuid.UUID) (domain.Label, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Label{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO issue_labels (issue_id, label_id)
		 SELECT il.issue_id, $2 FROM issue_labels il WHERE il.label_id = $1
		 ON CONFLICT DO NOTHING`, sourceID, targetID); err != nil {
		return domain.Label{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM labels WHERE id = $1`, sourceID); err != nil {
		return domain.Label{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Label{}, err
	}
	return s.getLabel(ctx, targetID)
}

func (s *Store) getLabel(ctx context.Context, id uuid.UUID) (domain.Label, error) {
	return scanLabel(s.pool.QueryRow(ctx, selectLabel+` WHERE l.id = $1`, id))
}
