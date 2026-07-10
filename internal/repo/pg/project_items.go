package pg

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── components ──

func (s *Store) ListComponents(ctx context.Context, projectKey string) ([]domain.Component, error) {
	const q = `
		SELECT c.id, c.name, c.description_md, c.lead_id, c.created_at,
		       (SELECT count(*) FROM issue_components ic
		          JOIN issues i ON i.id = ic.issue_id
		         WHERE ic.component_id = c.id AND i.deleted_at IS NULL
		           AND i.status NOT IN ('resolved','closed')) AS open_issues
		FROM components c
		JOIN projects p ON p.id = c.project_id
		WHERE p.key = $1
		ORDER BY c.name`
	rows, err := s.pool.Query(ctx, q, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Component
	for rows.Next() {
		var c domain.Component
		if err := rows.Scan(&c.ID, &c.Name, &c.DescriptionMD, &c.LeadID, &c.CreatedAt, &c.OpenIssues); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateComponent(ctx context.Context, in service.CreateComponentInput) (domain.Component, error) {
	const q = `
		INSERT INTO components (project_id, name, description_md, lead_id)
		SELECT p.id, $2, $3, $4 FROM projects p WHERE p.key = $1
		RETURNING id, name, description_md, lead_id, created_at`
	var c domain.Component
	err := s.pool.QueryRow(ctx, q, in.ProjectKey, strings.TrimSpace(in.Name), in.DescriptionMD, in.LeadID).
		Scan(&c.ID, &c.Name, &c.DescriptionMD, &c.LeadID, &c.CreatedAt)
	return c, err
}

func (s *Store) UpdateComponent(ctx context.Context, in service.UpdateComponentInput) (domain.Component, error) {
	const q = `
		UPDATE components SET
		  name           = COALESCE($2, name),
		  description_md = COALESCE($3, description_md),
		  -- nil = unchanged; the zero UUID clears the lead ($4::uuid, see 42P08 note).
		  lead_id        = CASE
		                     WHEN $4::uuid IS NULL THEN lead_id
		                     WHEN $4::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
		                     ELSE $4::uuid END
		WHERE id = $1
		RETURNING id, name, description_md, lead_id, created_at`
	var c domain.Component
	err := s.pool.QueryRow(ctx, q, in.ID, in.Name, in.DescriptionMD, in.LeadID).
		Scan(&c.ID, &c.Name, &c.DescriptionMD, &c.LeadID, &c.CreatedAt)
	return c, err
}

func (s *Store) DeleteComponent(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM components WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// setIssueComponents replaces (or appends to) an issue's component set. Unlike labels,
// components are managed structure — names that don't exist in the project are ignored
// rather than auto-created.
func setIssueComponents(ctx context.Context, tx pgx.Tx, projectID, issueID uuid.UUID, names []string, replace bool) error {
	if replace {
		if _, err := tx.Exec(ctx, `DELETE FROM issue_components WHERE issue_id = $1`, issueID); err != nil {
			return err
		}
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO issue_components (issue_id, component_id)
			 SELECT $1, c.id FROM components c
			 WHERE c.project_id = $2 AND lower(c.name) = lower($3)
			 ON CONFLICT DO NOTHING`, issueID, projectID, name); err != nil {
			return err
		}
	}
	return nil
}
