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
	q := fmt.Sprintf(`%s WHERE %s ORDER BY i.created_at DESC LIMIT $%d OFFSET $%d`,
		selectIssue, clause, len(args)-1, len(args))
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

	const upd = `
		UPDATE issues SET status = $2,
		  resolved_at = CASE WHEN $2 IN ('resolved','closed') AND resolved_at IS NULL THEN now() ELSE resolved_at END,
		  closed_at   = CASE WHEN $2 = 'closed' THEN now() ELSE closed_at END,
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
		`SELECT id, issue_id, author_id, body_md, edited_at, created_at
		 FROM comments WHERE issue_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at LIMIT $2 OFFSET $3`, issueID, clampLimit(limit), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Comment
	for rows.Next() {
		var c domain.Comment
		var author *uuid.UUID
		if err := rows.Scan(&c.ID, &c.IssueID, &author, &c.BodyMD, &c.EditedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		if author != nil {
			c.Author = &domain.User{ID: *author}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListActivity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, issue_id, actor_id, verb, entity_type, changes, occurred_at
		 FROM activity WHERE issue_id = $1 ORDER BY occurred_at DESC LIMIT $2 OFFSET $3`,
		issueID, clampLimit(limit), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Activity
	for rows.Next() {
		var a domain.Activity
		var actor *uuid.UUID
		if err := rows.Scan(&a.ID, &a.IssueID, &actor, &a.Verb, &a.EntityType, &a.Changes, &a.OccurredAt); err != nil {
			return nil, err
		}
		if actor != nil {
			a.Actor = &domain.User{ID: *actor}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ── helpers ──

const selectIssue = `
	SELECT i.id, p.key, i.number, i.type, i.title, i.description_md, i.status, i.severity, i.priority,
	       i.version_affected, i.version_fixed, i.git_commit_sha, i.pull_request_url,
	       i.repro_steps_md, i.expected_md, i.actual_md, i.environment_md, i.source,
	       i.created_at, i.updated_at
	FROM issues i JOIN projects p ON p.id = i.project_id`

type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(row scanner) (domain.Issue, error) {
	var i domain.Issue
	var sev *string
	err := row.Scan(
		&i.ID, &i.ProjectKey, &i.Number, &i.Type, &i.Title, &i.DescriptionMD, &i.Status, &sev, &i.Priority,
		&i.VersionAffected, &i.VersionFixed, &i.GitCommitSHA, &i.PullRequestURL,
		&i.ReproStepsMD, &i.ExpectedMD, &i.ActualMD, &i.EnvironmentMD, &i.Source,
		&i.CreatedAt, &i.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Issue{}, err
		}
		return domain.Issue{}, err
	}
	if sev != nil {
		sv := domain.Severity(*sev)
		i.Severity = &sv
	}
	i.Key = domain.IssueKey(i.ProjectKey, i.Number)
	return i, nil
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

func clampLimit(l int32) int32 {
	if l <= 0 || l > 200 {
		return 50
	}
	return l
}

var _ = time.Now // reserved for future time-based helpers
