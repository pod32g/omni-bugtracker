package pg

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ProjectKeyForEntity resolves the owning project's key for an id-addressed
// project-scoped entity. The entity name is whitelisted — never interpolate
// caller input into SQL.
func (s *Store) ProjectKeyForEntity(ctx context.Context, entity string, id uuid.UUID) (string, error) {
	var q string
	switch entity {
	case "component":
		q = `SELECT p.key FROM components t JOIN projects p ON p.id = t.project_id WHERE t.id = $1`
	case "milestone":
		q = `SELECT p.key FROM milestones t JOIN projects p ON p.id = t.project_id WHERE t.id = $1`
	case "release":
		q = `SELECT p.key FROM releases t JOIN projects p ON p.id = t.project_id WHERE t.id = $1`
	case "board":
		q = `SELECT p.key FROM boards t JOIN projects p ON p.id = t.project_id WHERE t.id = $1`
	case "board_column":
		q = `SELECT p.key FROM board_columns c JOIN boards b ON b.id = c.board_id
		     JOIN projects p ON p.id = b.project_id WHERE c.id = $1`
	default:
		return "", fmt.Errorf("unknown entity %q", entity)
	}
	var key string
	err := s.pool.QueryRow(ctx, q, id).Scan(&key)
	return key, err
}

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

// ── milestones ──

const selectMilestone = `
	SELECT m.id, m.title, m.description_md, m.due_on, m.state, m.created_at,
	       (SELECT count(*) FROM issues i WHERE i.milestone_id = m.id AND i.deleted_at IS NULL
	          AND i.status NOT IN ('resolved','closed')) AS open_issues,
	       (SELECT count(*) FROM issues i WHERE i.milestone_id = m.id AND i.deleted_at IS NULL
	          AND i.status IN ('resolved','closed')) AS closed_issues
	FROM milestones m`

func scanMilestone(row scanner) (domain.Milestone, error) {
	var m domain.Milestone
	err := row.Scan(&m.ID, &m.Title, &m.DescriptionMD, &m.DueOn, &m.State, &m.CreatedAt,
		&m.OpenIssues, &m.ClosedIssues)
	return m, err
}

func (s *Store) ListMilestones(ctx context.Context, projectKey string) ([]domain.Milestone, error) {
	q := selectMilestone + `
		JOIN projects p ON p.id = m.project_id
		WHERE p.key = $1
		ORDER BY (m.state = 'closed'), m.due_on ASC NULLS LAST, m.created_at`
	rows, err := s.pool.Query(ctx, q, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Milestone
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateMilestone(ctx context.Context, in service.CreateMilestoneInput) (domain.Milestone, error) {
	const q = `
		INSERT INTO milestones (project_id, title, description_md, due_on)
		SELECT p.id, $2, $3, $4 FROM projects p WHERE p.key = $1
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, in.ProjectKey, strings.TrimSpace(in.Title), in.DescriptionMD, in.DueOn).
		Scan(&id); err != nil {
		return domain.Milestone{}, err
	}
	return scanMilestone(s.pool.QueryRow(ctx, selectMilestone+` WHERE m.id = $1`, id))
}

func (s *Store) UpdateMilestone(ctx context.Context, in service.UpdateMilestoneInput) (domain.Milestone, error) {
	const q = `
		UPDATE milestones SET
		  title          = COALESCE($2, title),
		  description_md = COALESCE($3, description_md),
		  state          = COALESCE($4::milestone_state, state),
		  due_on         = CASE WHEN $6 THEN NULL ELSE COALESCE($5, due_on) END
		WHERE id = $1
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, in.ID, in.Title, in.DescriptionMD, in.State, in.DueOn, in.ClearDueOn).
		Scan(&id); err != nil {
		return domain.Milestone{}, err
	}
	return scanMilestone(s.pool.QueryRow(ctx, selectMilestone+` WHERE m.id = $1`, id))
}

func (s *Store) DeleteMilestone(ctx context.Context, id uuid.UUID) (bool, error) {
	// issues.milestone_id is ON DELETE SET NULL, so issues survive the delete.
	tag, err := s.pool.Exec(ctx, `DELETE FROM milestones WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ── releases ──

const selectRelease = `
	SELECT r.id, r.version, r.name, r.notes_md, r.state, r.git_tag, r.released_at, r.created_at,
	       (SELECT count(*) FROM issues i WHERE i.release_id = r.id AND i.deleted_at IS NULL
	          AND i.status NOT IN ('resolved','closed')) AS open_issues,
	       (SELECT count(*) FROM issues i WHERE i.release_id = r.id AND i.deleted_at IS NULL
	          AND i.status IN ('resolved','closed')) AS done_issues
	FROM releases r`

func scanRelease(row scanner) (domain.Release, error) {
	var r domain.Release
	err := row.Scan(&r.ID, &r.Version, &r.Name, &r.NotesMD, &r.State, &r.GitTag, &r.ReleasedAt, &r.CreatedAt,
		&r.OpenIssues, &r.DoneIssues)
	return r, err
}

func (s *Store) ListReleases(ctx context.Context, projectKey string) ([]domain.Release, error) {
	q := selectRelease + `
		JOIN projects p ON p.id = r.project_id
		WHERE p.key = $1
		ORDER BY (r.state = 'published'), r.created_at DESC`
	rows, err := s.pool.Query(ctx, q, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateRelease(ctx context.Context, in service.CreateReleaseInput) (domain.Release, error) {
	const q = `
		INSERT INTO releases (project_id, version, name, notes_md, git_tag, created_by)
		SELECT p.id, $2, $3, $4, $5, $6 FROM projects p WHERE p.key = $1
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, in.ProjectKey, strings.TrimSpace(in.Version), in.Name, in.NotesMD,
		in.GitTag, in.CreatedBy).Scan(&id); err != nil {
		return domain.Release{}, err
	}
	return scanRelease(s.pool.QueryRow(ctx, selectRelease+` WHERE r.id = $1`, id))
}

func (s *Store) UpdateRelease(ctx context.Context, in service.UpdateReleaseInput) (domain.Release, error) {
	const q = `
		UPDATE releases SET
		  version     = COALESCE($2, version),
		  name        = COALESCE($3, name),
		  notes_md    = COALESCE($4, notes_md),
		  git_tag     = COALESCE($5, git_tag),
		  state       = COALESCE($6::release_state, state),
		  released_at = CASE WHEN $6::text = 'published' AND released_at IS NULL THEN now() ELSE released_at END
		WHERE id = $1
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, in.ID, in.Version, in.Name, in.NotesMD, in.GitTag, in.State).
		Scan(&id); err != nil {
		return domain.Release{}, err
	}
	return scanRelease(s.pool.QueryRow(ctx, selectRelease+` WHERE r.id = $1`, id))
}

func (s *Store) DeleteRelease(ctx context.Context, id uuid.UUID) (bool, error) {
	// issues.release_id is ON DELETE SET NULL, so issues survive the delete.
	tag, err := s.pool.Exec(ctx, `DELETE FROM releases WHERE id = $1`, id)
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
