package pg

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

const selectTemplate = `
	SELECT t.id, p.key, t.name, t.issue_type::text, t.body_md, t.required_sections,
	       t.default_labels, t.default_component, t.default_priority, t.default_severity,
	       t.default_assignee_id, u.display_name, u.email,
	       t.is_default, t.position, t.created_at
	  FROM issue_templates t
	  JOIN projects p ON p.id = t.project_id
	  LEFT JOIN users u ON u.id = t.default_assignee_id`

func scanTemplate(row scanner) (domain.IssueTemplate, error) {
	var t domain.IssueTemplate
	var typ string
	var prio, sev, name, email *string
	var assignee *uuid.UUID
	if err := row.Scan(&t.ID, &t.ProjectKey, &t.Name, &typ, &t.BodyMD, &t.RequiredSections,
		&t.DefaultLabels, &t.DefaultComponent, &prio, &sev,
		&assignee, &name, &email, &t.IsDefault, &t.Position, &t.CreatedAt); err != nil {
		return domain.IssueTemplate{}, err
	}
	t.Type = domain.IssueType(typ)
	if prio != nil {
		p := domain.Priority(*prio)
		t.DefaultPriority = &p
	}
	if sev != nil {
		s := domain.Severity(*sev)
		t.DefaultSeverity = &s
	}
	if assignee != nil {
		t.DefaultAssignee = &domain.User{ID: *assignee, DisplayName: deref(name), Email: deref(email)}
	}
	// [] rather than null, so a client never has to handle both.
	if t.RequiredSections == nil {
		t.RequiredSections = []string{}
	}
	if t.DefaultLabels == nil {
		t.DefaultLabels = []string{}
	}
	return t, nil
}

func (s *Store) ListIssueTemplates(ctx context.Context, projectKey string) ([]domain.IssueTemplate, error) {
	rows, err := s.pool.Query(ctx, selectTemplate+
		` WHERE p.key = $1 ORDER BY t.issue_type, t.is_default DESC, t.position, t.name`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.IssueTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetIssueTemplate(ctx context.Context, id uuid.UUID) (domain.IssueTemplate, error) {
	return scanTemplate(s.pool.QueryRow(ctx, selectTemplate+` WHERE t.id = $1`, id))
}

func (s *Store) CreateIssueTemplate(ctx context.Context, in service.IssueTemplateInput) (domain.IssueTemplate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.IssueTemplate{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO issue_templates
		     (project_id, name, issue_type, body_md, required_sections, default_labels,
		      default_component, default_priority, default_severity, default_assignee_id,
		      is_default, position)
		 SELECT p.id, $2, $3::issue_type, $4, $5, $6, $7, $8::priority, $9::severity, $10::uuid, $11,
		        COALESCE((SELECT max(position) + 1 FROM issue_templates
		                   WHERE project_id = p.id AND issue_type = $3::issue_type), 0)
		   FROM projects p WHERE p.key = $1
		 RETURNING id`,
		in.ProjectKey, in.Name, string(in.Type), in.BodyMD, in.RequiredSections, in.DefaultLabels,
		in.DefaultComponent, in.DefaultPriority, in.DefaultSeverity, in.DefaultAssigneeID,
		in.IsDefault).Scan(&id); err != nil {
		return domain.IssueTemplate{}, err
	}
	if in.IsDefault {
		if err := clearOtherDefaults(ctx, tx, id); err != nil {
			return domain.IssueTemplate{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.IssueTemplate{}, err
	}
	return s.GetIssueTemplate(ctx, id)
}

// clearOtherDefaults stands down the previous default for this (project, type). The
// partial unique index guarantees at most one, so this is what keeps a promotion from
// colliding with it rather than a check that can race.
func clearOtherDefaults(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE issue_templates SET is_default = FALSE
		  WHERE id <> $1 AND is_default
		    AND (project_id, issue_type) =
		        (SELECT project_id, issue_type FROM issue_templates WHERE id = $1)`, id)
	return err
}

func (s *Store) UpdateIssueTemplate(ctx context.Context, id uuid.UUID, in service.UpdateIssueTemplateInput) (domain.IssueTemplate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.IssueTemplate{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Promotion first: the partial unique index would reject the update otherwise.
	if in.IsDefault != nil && *in.IsDefault {
		if err := clearOtherDefaults(ctx, tx, id); err != nil {
			return domain.IssueTemplate{}, err
		}
	}
	tag, err := tx.Exec(ctx,
		`UPDATE issue_templates SET
		   name              = COALESCE($2, name),
		   body_md           = COALESCE($3, body_md),
		   required_sections = COALESCE($4, required_sections),
		   default_labels    = COALESCE($5, default_labels),
		   default_component = COALESCE($6, default_component),
		   default_priority  = COALESCE($7::priority, default_priority),
		   default_severity  = COALESCE($8::severity, default_severity),
		   is_default        = COALESCE($9, is_default),
		   position          = COALESCE($10, position)
		 WHERE id = $1`,
		id, in.Name, in.BodyMD, in.RequiredSections, in.DefaultLabels, in.DefaultComponent,
		in.DefaultPriority, in.DefaultSeverity, in.IsDefault, in.Position)
	if err != nil {
		return domain.IssueTemplate{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.IssueTemplate{}, pgx.ErrNoRows
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.IssueTemplate{}, err
	}
	return s.GetIssueTemplate(ctx, id)
}

func (s *Store) DeleteIssueTemplate(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM issue_templates WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
