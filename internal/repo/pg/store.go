// Package pg implements service.Repository against Postgres using pgx directly.
//
// The db/queries/*.sql files + sqlc.yaml are the typed-query source of truth; teams that
// prefer generated code can run `make generate` and swap these method bodies for the
// sqlc-generated calls. This hand-written adapter exists so the scaffold compiles and runs
// before any codegen step. Multi-statement writes run in one transaction and invoke the
// caller's publish hook inside it — that transaction IS the event outbox.
package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ── users & tokens ──

func (s *Store) UpsertUser(ctx context.Context, in service.UpsertUserParams) (domain.User, error) {
	// COALESCE(NULLIF(...,'')) keeps existing profile fields when a caller (e.g. the
	// per-request middleware validating an access token that carries no email/name)
	// upserts with empty values — only the OIDC callback's id_token enrichment fills them.
	const q = `
		INSERT INTO users (identity_sub, email, display_name, avatar_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (identity_sub) DO UPDATE
		  SET email        = COALESCE(NULLIF(EXCLUDED.email, ''), users.email),
		      display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), users.display_name),
		      avatar_url   = COALESCE(NULLIF(EXCLUDED.avatar_url, ''), users.avatar_url),
		      last_seen_at = now(), updated_at = now()
		RETURNING id, identity_sub, email, display_name, avatar_url, role`
	var u domain.User
	err := s.pool.QueryRow(ctx, q, in.IdentitySub, in.Email, in.DisplayName, in.AvatarURL).
		Scan(&u.ID, &u.IdentitySub, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role)
	return u, err
}

func (s *Store) GetUserByToken(ctx context.Context, tokenHash []byte) (service.TokenPrincipal, error) {
	const q = `
		SELECT t.id, u.id, u.identity_sub, u.email, u.display_name, u.avatar_url, u.role, t.scopes
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > now())`
	var tp service.TokenPrincipal
	err := s.pool.QueryRow(ctx, q, tokenHash).Scan(
		&tp.TokenID, &tp.User.ID, &tp.User.IdentitySub, &tp.User.Email,
		&tp.User.DisplayName, &tp.User.AvatarURL, &tp.User.Role, &tp.Scopes,
	)
	return tp, err
}

func (s *Store) TouchToken(ctx context.Context, tokenID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, tokenID)
	return err
}

// ── projects ──

func (s *Store) GetProjectByKey(ctx context.Context, key string) (domain.Project, error) {
	const q = `SELECT id, key, name, description_md, is_archived, created_at FROM projects WHERE key = $1`
	var p domain.Project
	err := s.pool.QueryRow(ctx, q, key).Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.IsArchived, &p.CreatedAt)
	return p, err
}

func (s *Store) ListProjects(ctx context.Context, limit, offset int32) ([]domain.Project, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, key, name, description_md, is_archived, created_at
		 FROM projects WHERE is_archived = FALSE ORDER BY key LIMIT $1 OFFSET $2`,
		clampLimit(limit), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.IsArchived, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListLabels(ctx context.Context, projectKey string) ([]domain.Label, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT l.id, l.name, l.color FROM labels l
		 LEFT JOIN projects p ON p.id = l.project_id
		 WHERE l.project_id IS NULL OR p.key = $1
		 ORDER BY l.name`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Label
	for rows.Next() {
		var l domain.Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ensureLabels resolves label names to ids within a project, creating any that are new.
func ensureLabels(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, names []string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		var id uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT id FROM labels WHERE project_id = $1 AND lower(name) = lower($2)`, projectID, name).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.QueryRow(ctx,
				`INSERT INTO labels (project_id, name) VALUES ($1, $2) RETURNING id`, projectID, name).Scan(&id); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func setIssueLabels(ctx context.Context, tx pgx.Tx, projectID, issueID uuid.UUID, names []string, replace bool) error {
	if replace {
		if _, err := tx.Exec(ctx, `DELETE FROM issue_labels WHERE issue_id = $1`, issueID); err != nil {
			return err
		}
	}
	ids, err := ensureLabels(ctx, tx, projectID, names)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx,
			`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, issueID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateProject(ctx context.Context, in service.CreateProjectInput) (domain.Project, error) {
	const q = `INSERT INTO projects (key, name, description_md)
	           VALUES ($1, $2, $3)
	           RETURNING id, key, name, description_md, is_archived, created_at`
	var p domain.Project
	err := s.pool.QueryRow(ctx, q, in.Key, in.Name, in.DescriptionMD).
		Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.IsArchived, &p.CreatedAt)
	return p, err
}

// ── issues ──

func (s *Store) CreateIssue(ctx context.Context, in service.CreateIssueInput, publish service.PublishIssueFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// Allocate the next per-project number atomically.
	var projectID uuid.UUID
	var number int32
	if err := tx.QueryRow(ctx,
		`UPDATE projects SET next_issue_number = next_issue_number + 1, updated_at = now()
		 WHERE key = $1 RETURNING id, next_issue_number - 1`, in.ProjectKey).
		Scan(&projectID, &number); err != nil {
		return domain.Issue{}, fmt.Errorf("allocate number: %w", err)
	}

	const insert = `
		INSERT INTO issues (
			project_id, number, type, title, description_md, status, severity, priority,
			reporter_id, assignee_id, version_affected,
			repro_steps_md, expected_md, actual_md, environment_md, source, dedupe_key
		) VALUES ($1,$2,$3,$4,$5,'open',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id, created_at, updated_at`
	issue := domain.Issue{
		ProjectKey: in.ProjectKey, Number: number, Type: in.Type, Title: in.Title,
		DescriptionMD: in.DescriptionMD, Status: domain.StatusOpen, Severity: in.Severity,
		Priority: in.Priority, VersionAffected: in.VersionAffected, ReproStepsMD: in.ReproStepsMD,
		ExpectedMD: in.ExpectedMD, ActualMD: in.ActualMD, EnvironmentMD: in.EnvironmentMD,
		Source: in.Source, Labels: in.Labels, Components: in.Components,
	}
	err = tx.QueryRow(ctx, insert,
		projectID, number, in.Type, in.Title, in.DescriptionMD, sevPtr(in.Severity), in.Priority,
		in.ReporterID, in.AssigneeID, in.VersionAffected,
		in.ReproStepsMD, in.ExpectedMD, in.ActualMD, in.EnvironmentMD, in.Source, in.DedupeKey,
	).Scan(&issue.ID, &issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		return domain.Issue{}, fmt.Errorf("insert issue: %w", err)
	}
	issue.Key = domain.IssueKey(in.ProjectKey, number)

	if len(in.Labels) > 0 {
		if err := setIssueLabels(ctx, tx, projectID, issue.ID, in.Labels, false); err != nil {
			return domain.Issue{}, fmt.Errorf("set labels: %w", err)
		}
	}
	if err := recordActivity(ctx, tx, issue.ID, in.ReporterID, "issue.created", "issue", issue.ID); err != nil {
		return domain.Issue{}, err
	}
	if publish != nil {
		if err := publish(tx, issue); err != nil {
			return domain.Issue{}, fmt.Errorf("publish: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Issue{}, err
	}
	return issue, nil
}

func (s *Store) GetIssueByKey(ctx context.Context, projectKey string, number int32) (domain.Issue, error) {
	const q = selectIssue + ` WHERE p.key = $1 AND i.number = $2 AND i.deleted_at IS NULL`
	row := s.pool.QueryRow(ctx, q, projectKey, number)
	return scanIssue(row)
}

func (s *Store) ListIssues(ctx context.Context, f service.IssueFilter) ([]domain.Issue, int, error) {
	where := []string{"i.deleted_at IS NULL", "p.key = $1"}
	args := []any{f.ProjectKey}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Status != nil {
		add("i.status = $%d", string(*f.Status))
	}
	if f.AssigneeID != nil {
		add("i.assignee_id = $%d", *f.AssigneeID)
	}
	if f.Type != nil {
		add("i.type = $%d", string(*f.Type))
	}
	if f.Severity != nil {
		add("i.severity = $%d", string(*f.Severity))
	}
	if strings.TrimSpace(f.Label) != "" {
		add("EXISTS (SELECT 1 FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id AND lower(l.name) = lower($%d))", f.Label)
	}
	if strings.TrimSpace(f.Query) != "" {
		add("i.fts @@ websearch_to_tsquery('english', $%d)", f.Query)
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM issues i JOIN projects p ON p.id = i.project_id WHERE `+clause, args...).
		Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, clampLimit(f.Limit), f.Offset)
	q := fmt.Sprintf(`%s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		selectIssue, clause, orderBy(f.Sort), len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.Issue
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, iss)
	}
	return out, total, rows.Err()
}

func (s *Store) TransitionIssue(ctx context.Context, id uuid.UUID, to domain.IssueStatus, actor uuid.UUID, publish service.PublishFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// $2 is bound as text and cast per use — comparisons need text, the assignment needs
	// the enum. Using a bare $2 for both makes Postgres deduce conflicting types (42P08).
	const upd = `
		UPDATE issues SET status = $2::issue_status,
		  resolved_at = CASE WHEN $2::text IN ('resolved','closed') AND resolved_at IS NULL THEN now() ELSE resolved_at END,
		  closed_at   = CASE WHEN $2::text = 'closed' THEN now() ELSE closed_at END,
		  updated_at  = now()
		WHERE id = $1 AND deleted_at IS NULL`
	if _, err := tx.Exec(ctx, upd, id, string(to)); err != nil {
		return domain.Issue{}, err
	}
	if err := recordActivity(ctx, tx, id, actor, "issue.status_changed", "issue", id); err != nil {
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
	row := s.pool.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id)
	return scanIssue(row)
}

// UpdateIssue applies a partial update (COALESCE keeps unchanged fields), records an
// "issue.updated" timeline entry, enqueues the event, and returns the refreshed issue.
func (s *Store) UpdateIssue(ctx context.Context, id, actor uuid.UUID, in service.UpdateIssueInput, publish service.PublishFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const q = `
		UPDATE issues SET
		  title            = COALESCE($2, title),
		  description_md   = COALESCE($3, description_md),
		  type             = COALESCE($4::issue_type, type),
		  severity         = COALESCE($5::severity, severity),
		  priority         = COALESCE($6::priority, priority),
		  -- nil = unchanged; the zero UUID clears the assignee; otherwise assign.
		  assignee_id      = CASE
		                       WHEN $7 IS NULL THEN assignee_id
		                       WHEN $7 = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
		                       ELSE $7 END,
		  version_affected = COALESCE($8, version_affected),
		  version_fixed    = COALESCE($9, version_fixed),
		  repro_steps_md   = COALESCE($10, repro_steps_md),
		  expected_md      = COALESCE($11, expected_md),
		  actual_md        = COALESCE($12, actual_md),
		  environment_md   = COALESCE($13, environment_md),
		  updated_at       = now()
		WHERE id = $1 AND deleted_at IS NULL`
	if _, err := tx.Exec(ctx, q, id,
		in.Title, in.DescriptionMD, typePtr(in.Type), sevPtr(in.Severity), prioPtr(in.Priority),
		in.AssigneeID, in.VersionAffected, in.VersionFixed,
		in.ReproStepsMD, in.ExpectedMD, in.ActualMD, in.EnvironmentMD); err != nil {
		return domain.Issue{}, err
	}
	if in.Labels != nil {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1`, id).Scan(&projectID); err != nil {
			return domain.Issue{}, err
		}
		if err := setIssueLabels(ctx, tx, projectID, id, *in.Labels, true); err != nil {
			return domain.Issue{}, fmt.Errorf("set labels: %w", err)
		}
	}
	if err := recordActivity(ctx, tx, id, actor, "issue.updated", "issue", id); err != nil {
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
	return scanIssue(s.pool.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id))
}

// SoftDeleteIssue marks the issue deleted, records the timeline entry, and emits the event.
func (s *Store) SoftDeleteIssue(ctx context.Context, id, actor uuid.UUID, publish service.PublishFn) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `UPDATE issues SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id); err != nil {
		return err
	}
	if err := recordActivity(ctx, tx, id, actor, "issue.deleted", "issue", id); err != nil {
		return err
	}
	if publish != nil {
		if err := publish(tx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ── comments & activity ──

func (s *Store) AddComment(ctx context.Context, issueID, author uuid.UUID, body string, publish service.PublishFn) (domain.Comment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Comment{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var c domain.Comment
	c.IssueID = issueID
	c.BodyMD = body
	if err := tx.QueryRow(ctx,
		`INSERT INTO comments (issue_id, author_id, body_md) VALUES ($1,$2,$3)
		 RETURNING id, created_at`, issueID, author, body).
		Scan(&c.ID, &c.CreatedAt); err != nil {
		return domain.Comment{}, err
	}
	if err := recordActivity(ctx, tx, issueID, author, "comment.created", "comment", c.ID); err != nil {
		return domain.Comment{}, err
	}
	if publish != nil {
		if err := publish(tx); err != nil {
			return domain.Comment{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, err
	}
	return c, nil
}

func (s *Store) ListComments(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.issue_id, c.author_id, u.display_name, u.email, c.body_md, c.edited_at, c.created_at
		 FROM comments c LEFT JOIN users u ON u.id = c.author_id
		 WHERE c.issue_id = $1 AND c.deleted_at IS NULL
		 ORDER BY c.created_at LIMIT $2 OFFSET $3`, issueID, clampLimit(limit), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Comment
	for rows.Next() {
		var c domain.Comment
		var authorID *uuid.UUID
		var displayName, email *string
		if err := rows.Scan(&c.ID, &c.IssueID, &authorID, &displayName, &email, &c.BodyMD, &c.EditedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		if authorID != nil {
			c.Author = &domain.User{ID: *authorID, DisplayName: deref(displayName), Email: deref(email)}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListActivity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.id, a.issue_id, a.actor_id, u.display_name, u.email, a.verb, a.entity_type,
		        a.changes, a.occurred_at, p.key, i.number
		 FROM activity a
		 LEFT JOIN users u ON u.id = a.actor_id
		 LEFT JOIN issues i ON i.id = a.issue_id
		 LEFT JOIN projects p ON p.id = i.project_id
		 WHERE a.issue_id = $1 ORDER BY a.occurred_at DESC LIMIT $2 OFFSET $3`,
		issueID, clampLimit(limit), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityRows(rows)
}

// scanActivityRows scans the standard activity+actor+issue-key column set.
func scanActivityRows(rows pgx.Rows) ([]domain.Activity, error) {
	var out []domain.Activity
	for rows.Next() {
		var a domain.Activity
		var actorID *uuid.UUID
		var displayName, email, projectKey *string
		var number *int32
		if err := rows.Scan(&a.ID, &a.IssueID, &actorID, &displayName, &email, &a.Verb,
			&a.EntityType, &a.Changes, &a.OccurredAt, &projectKey, &number); err != nil {
			return nil, err
		}
		if actorID != nil {
			u := &domain.User{ID: *actorID}
			if displayName != nil {
				u.DisplayName = *displayName
			}
			if email != nil {
				u.Email = *email
			}
			a.Actor = u
		}
		if projectKey != nil && number != nil {
			a.IssueKey = domain.IssueKey(*projectKey, *number)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ── dashboard, activity feed, users ──

func (s *Store) RecentActivity(ctx context.Context, limit int32) ([]domain.Activity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.id, a.issue_id, a.actor_id, u.display_name, u.email, a.verb, a.entity_type, a.changes, a.occurred_at,
		        p.key, i.number
		 FROM activity a
		 LEFT JOIN users u ON u.id = a.actor_id
		 LEFT JOIN issues i ON i.id = a.issue_id
		 LEFT JOIN projects p ON p.id = i.project_id
		 ORDER BY a.occurred_at DESC LIMIT $1`, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityRows(rows)
}

func (s *Store) Dashboard(ctx context.Context) (domain.Dashboard, error) {
	d := domain.Dashboard{
		IssuesByStatus:    map[string]int{},
		IssuesByComponent: map[string]int{},
		TeamWorkload:      map[string]int{},
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE status NOT IN ('resolved','closed')),
		        count(*) FILTER (WHERE severity = 'critical' AND status NOT IN ('resolved','closed'))
		 FROM issues WHERE deleted_at IS NULL`).Scan(&d.OpenIssues, &d.CriticalIssues); err != nil {
		return d, err
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(EXTRACT(EPOCH FROM avg(resolved_at - created_at)) / 3600, 0),
		        COALESCE(EXTRACT(EPOCH FROM avg(resolved_at - created_at)
		                 FILTER (WHERE resolved_at > now() - interval '30 days')) / 3600, 0)
		 FROM issues WHERE resolved_at IS NOT NULL AND deleted_at IS NULL`).
		Scan(&d.AvgResolutionHours, &d.MTTRHours); err != nil {
		return d, err
	}

	var reopened, terminal int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE status = 'reopened'),
		        count(*) FILTER (WHERE status IN ('resolved','closed','reopened'))
		 FROM issues WHERE deleted_at IS NULL`).Scan(&reopened, &terminal); err != nil {
		return d, err
	}
	if terminal > 0 {
		d.RegressionRate = float64(reopened) / float64(terminal)
	}

	if err := scanCountMap(ctx, s, d.IssuesByStatus,
		`SELECT status::text, count(*) FROM issues WHERE deleted_at IS NULL GROUP BY status`); err != nil {
		return d, err
	}
	if err := scanCountMap(ctx, s, d.IssuesByComponent,
		`SELECT c.name, count(*) FROM issue_components ic
		   JOIN components c ON c.id = ic.component_id
		   JOIN issues i ON i.id = ic.issue_id
		 WHERE i.deleted_at IS NULL GROUP BY c.name`); err != nil {
		return d, err
	}
	if err := scanCountMap(ctx, s, d.TeamWorkload,
		`SELECT u.display_name, count(*) FROM issues i
		   JOIN users u ON u.id = i.assignee_id
		 WHERE i.deleted_at IS NULL AND i.status NOT IN ('resolved','closed')
		 GROUP BY u.display_name`); err != nil {
		return d, err
	}

	acts, err := s.RecentActivity(ctx, 12)
	if err != nil {
		return d, err
	}
	d.RecentActivity = acts
	return d, nil
}

func (s *Store) ListUsers(ctx context.Context, limit int32) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, identity_sub, email, display_name, avatar_url, role
		 FROM users WHERE is_active = TRUE ORDER BY display_name LIMIT $1`, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.IdentitySub, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func scanCountMap(ctx context.Context, s *Store, dst map[string]int, query string) error {
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		dst[k] = n
	}
	return rows.Err()
}

// ── git integration ──

func (s *Store) UpsertCommit(ctx context.Context, in service.CommitInput) (uuid.UUID, error) {
	var id uuid.UUID
	var committedAt any
	if !in.CommittedAt.IsZero() {
		committedAt = in.CommittedAt
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO git_commits (repo, sha, author, message, url, committed_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (repo, sha) DO UPDATE SET message = EXCLUDED.message
		 RETURNING id`,
		in.Repo, in.SHA, in.Author, in.Message, in.URL, committedAt).Scan(&id)
	return id, err
}

func (s *Store) UpsertPullRequest(ctx context.Context, in service.PRInput) (uuid.UUID, error) {
	state := in.State
	if state == "" {
		state = "open"
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO pull_requests (repo, number, url, title, state, merged_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (repo, number) DO UPDATE
		   SET state = EXCLUDED.state, title = EXCLUDED.title,
		       merged_at = EXCLUDED.merged_at, updated_at = now()
		 RETURNING id`,
		in.Repo, in.Number, in.URL, in.Title, state, in.MergedAt).Scan(&id)
	return id, err
}

// ApplyGitLink links a commit/PR to an issue, records a NULL-actor (system) timeline
// entry, optionally transitions the issue, and enqueues the event — all in one tx.
func (s *Store) ApplyGitLink(ctx context.Context, in service.GitLinkInput, publish service.PublishFn) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	entityType := "issue"
	var entityID uuid.UUID = in.IssueID
	if in.CommitID != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO issue_commits (issue_id, commit_id, verb) VALUES ($1,$2,$3)
			 ON CONFLICT (issue_id, commit_id) DO UPDATE SET verb = EXCLUDED.verb`,
			in.IssueID, *in.CommitID, in.Verb); err != nil {
			return err
		}
		entityType, entityID = "commit", *in.CommitID
	}
	if in.PRID != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO issue_pull_requests (issue_id, pr_id, verb) VALUES ($1,$2,$3)
			 ON CONFLICT (issue_id, pr_id) DO UPDATE SET verb = EXCLUDED.verb`,
			in.IssueID, *in.PRID, in.Verb); err != nil {
			return err
		}
		entityType, entityID = "pull_request", *in.PRID
	}
	if in.NewStatus != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE issues SET status = $2::issue_status,
			   resolved_at = CASE WHEN $2::text IN ('resolved','closed') AND resolved_at IS NULL THEN now() ELSE resolved_at END,
			   closed_at   = CASE WHEN $2::text = 'closed' THEN now() ELSE closed_at END,
			   updated_at  = now()
			 WHERE id = $1 AND deleted_at IS NULL`, in.IssueID, string(*in.NewStatus)); err != nil {
			return err
		}
	}
	detail := in.Detail
	if len(detail) == 0 {
		detail = []byte("{}")
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
		 VALUES ($1, NULL, $2, $3, $4, $5)`,
		in.IssueID, in.ActivityVerb, entityType, entityID, detail); err != nil {
		return err
	}
	if publish != nil {
		if err := publish(tx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListCommitsForIssue(ctx context.Context, issueID uuid.UUID) ([]domain.LinkedCommit, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.sha, c.repo, c.author, c.message, c.url, ic.verb, c.created_at
		 FROM git_commits c JOIN issue_commits ic ON ic.commit_id = c.id
		 WHERE ic.issue_id = $1 ORDER BY c.committed_at DESC NULLS LAST, c.created_at DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LinkedCommit
	for rows.Next() {
		var lc domain.LinkedCommit
		if err := rows.Scan(&lc.SHA, &lc.Repo, &lc.Author, &lc.Message, &lc.URL, &lc.Verb, &lc.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}

// ── helpers ──

const selectIssue = `
	SELECT i.id, p.key, i.number, i.type, i.title, i.description_md, i.status, i.severity, i.priority,
	       i.version_affected, i.version_fixed, i.git_commit_sha, i.pull_request_url,
	       i.repro_steps_md, i.expected_md, i.actual_md, i.environment_md, i.source,
	       i.created_at, i.updated_at,
	       ru.id, ru.display_name, ru.email,
	       au.id, au.display_name, au.email,
	       COALESCE(array(SELECT l.name FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id ORDER BY l.name), '{}') AS labels,
	       COALESCE(array(SELECT c.name FROM issue_components ic JOIN components c ON c.id = ic.component_id WHERE ic.issue_id = i.id ORDER BY c.name), '{}') AS components
	FROM issues i
	JOIN projects p ON p.id = i.project_id
	LEFT JOIN users ru ON ru.id = i.reporter_id
	LEFT JOIN users au ON au.id = i.assignee_id`

type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(row scanner) (domain.Issue, error) {
	var i domain.Issue
	var sev *string
	var reporterID, assigneeID *uuid.UUID
	var reporterName, reporterEmail, assigneeName, assigneeEmail *string
	err := row.Scan(
		&i.ID, &i.ProjectKey, &i.Number, &i.Type, &i.Title, &i.DescriptionMD, &i.Status, &sev, &i.Priority,
		&i.VersionAffected, &i.VersionFixed, &i.GitCommitSHA, &i.PullRequestURL,
		&i.ReproStepsMD, &i.ExpectedMD, &i.ActualMD, &i.EnvironmentMD, &i.Source,
		&i.CreatedAt, &i.UpdatedAt,
		&reporterID, &reporterName, &reporterEmail,
		&assigneeID, &assigneeName, &assigneeEmail,
		&i.Labels, &i.Components,
	)
	if err != nil {
		return domain.Issue{}, err
	}
	if sev != nil {
		sv := domain.Severity(*sev)
		i.Severity = &sv
	}
	if reporterID != nil {
		i.Reporter = &domain.User{ID: *reporterID, DisplayName: deref(reporterName), Email: deref(reporterEmail)}
	}
	if assigneeID != nil {
		i.Assignee = &domain.User{ID: *assigneeID, DisplayName: deref(assigneeName), Email: deref(assigneeEmail)}
	}
	i.Key = domain.IssueKey(i.ProjectKey, i.Number)
	return i, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func recordActivity(ctx context.Context, tx pgx.Tx, issueID, actor uuid.UUID, verb, entityType string, entityID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
		 VALUES ($1,$2,$3,$4,$5,'{}')`, issueID, actor, verb, entityType, entityID)
	return err
}

func sevPtr(s *domain.Severity) *string {
	if s == nil {
		return nil
	}
	v := string(*s)
	return &v
}

func typePtr(t *domain.IssueType) *string {
	if t == nil {
		return nil
	}
	v := string(*t)
	return &v
}

func prioPtr(p *domain.Priority) *string {
	if p == nil {
		return nil
	}
	v := string(*p)
	return &v
}

// orderBy maps a whitelisted sort key to a safe ORDER BY clause (never interpolate
// user input into SQL). Enum order puts p0 / critical first.
func orderBy(sort string) string {
	switch sort {
	case "created_at":
		return "i.created_at ASC"
	case "-updated_at", "updated":
		return "i.updated_at DESC"
	case "priority":
		return "i.priority ASC, i.created_at DESC"
	case "severity":
		return "i.severity ASC NULLS LAST, i.created_at DESC"
	default:
		return "i.created_at DESC"
	}
}

func clampLimit(l int32) int32 {
	if l <= 0 || l > 200 {
		return 50
	}
	return l
}

var _ = time.Now // reserved for future time-based helpers
