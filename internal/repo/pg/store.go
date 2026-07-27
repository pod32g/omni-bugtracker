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
	"encoding/json"
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
	// Bootstrap: the very first user to sign in on a fresh install becomes owner, so
	// there's always an admin without hand-editing the DB. Everyone else defaults to member.
	const q = `
		INSERT INTO users (identity_sub, email, display_name, avatar_url, role)
		VALUES ($1, $2, $3, $4,
		        CASE WHEN NOT EXISTS (SELECT 1 FROM users) THEN 'owner'::app_role ELSE 'member'::app_role END)
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
	const q = `SELECT id, key, name, description_md, default_assignee_id, is_archived, created_at FROM projects WHERE key = $1`
	var p domain.Project
	err := s.pool.QueryRow(ctx, q, key).Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.DefaultAssigneeID, &p.IsArchived, &p.CreatedAt)
	return p, err
}

func (s *Store) ListProjects(ctx context.Context, limit, offset int32) ([]domain.Project, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, key, name, description_md, default_assignee_id, is_archived, created_at
		 FROM projects WHERE is_archived = FALSE ORDER BY key LIMIT $1 OFFSET $2`,
		clampLimit(limit), offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.DefaultAssigneeID, &p.IsArchived, &p.CreatedAt); err != nil {
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

// UpdateProject applies a partial edit keyed by project key; nil fields keep their value.
func (s *Store) UpdateProject(ctx context.Context, in service.UpdateProjectInput) (domain.Project, error) {
	const q = `UPDATE projects SET
	               name           = COALESCE($2, name),
	               description_md  = COALESCE($3, description_md),
	               is_archived    = COALESCE($4, is_archived),
	               -- nil = unchanged; the zero UUID clears; otherwise set ($5::uuid, see 42P08 note).
	               default_assignee_id = CASE
	                                       WHEN $5::uuid IS NULL THEN default_assignee_id
	                                       WHEN $5::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
	                                       ELSE $5::uuid END,
	               updated_at     = now()
	           WHERE key = $1
	           RETURNING id, key, name, description_md, default_assignee_id, is_archived, created_at`
	var p domain.Project
	err := s.pool.QueryRow(ctx, q, in.Key, in.Name, in.DescriptionMD, in.IsArchived, in.DefaultAssigneeID).
		Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.DefaultAssigneeID, &p.IsArchived, &p.CreatedAt)
	return p, err
}

// RenameProjectKey changes projects.key. Because issue keys are derived from the
// project key via join, no issue rows are touched. A collision with an existing key
// fails on the UNIQUE constraint; the format is guarded by the table's CHECK.
func (s *Store) RenameProjectKey(ctx context.Context, oldKey, newKey string) (domain.Project, error) {
	const q = `UPDATE projects SET key = $2, updated_at = now()
	           WHERE key = $1
	           RETURNING id, key, name, description_md, default_assignee_id, is_archived, created_at`
	var p domain.Project
	err := s.pool.QueryRow(ctx, q, oldKey, newKey).
		Scan(&p.ID, &p.Key, &p.Name, &p.DescriptionMD, &p.DefaultAssigneeID, &p.IsArchived, &p.CreatedAt)
	return p, err
}

// ── api tokens (self-service, per user) ──

func (s *Store) CreateAPIToken(ctx context.Context, in service.CreateTokenInput) (domain.APIToken, error) {
	scopes := in.Scopes
	if scopes == nil {
		scopes = []string{} // column is NOT NULL; a nil slice would encode as SQL NULL
	}
	const q = `INSERT INTO api_tokens (user_id, name, token_hash, scopes)
	           VALUES ($1, $2, $3, $4)
	           RETURNING id, name, scopes, last_used_at, expires_at, created_at`
	var t domain.APIToken
	err := s.pool.QueryRow(ctx, q, in.UserID, in.Name, in.TokenHash, scopes).
		Scan(&t.ID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt)
	return t, err
}

func (s *Store) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]domain.APIToken, error) {
	const q = `SELECT id, name, scopes, last_used_at, expires_at, created_at
	           FROM api_tokens
	           WHERE user_id = $1 AND revoked_at IS NULL
	           ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.APIToken
	for rows.Next() {
		var t domain.APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeAPIToken revokes a token owned by the user; reports whether a row was affected.
func (s *Store) RevokeAPIToken(ctx context.Context, userID, tokenID uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		tokenID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ── issues ──

func (s *Store) CreateIssue(ctx context.Context, in service.CreateIssueInput, publish service.PublishIssueFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// Allocate the next per-project number atomically. Also picks up the project's
	// default assignee so unassigned new issues land with someone responsible.
	var projectID uuid.UUID
	var number int32
	var defaultAssignee *uuid.UUID
	if err := tx.QueryRow(ctx,
		`UPDATE projects SET next_issue_number = next_issue_number + 1, updated_at = now()
		 WHERE key = $1 RETURNING id, next_issue_number - 1, default_assignee_id`, in.ProjectKey).
		Scan(&projectID, &number, &defaultAssignee); err != nil {
		return domain.Issue{}, fmt.Errorf("allocate number: %w", err)
	}
	// Routing precedence: whoever the reporter named, then the lead of a component
	// the issue is filed against, then the project default. components.lead_id was
	// stored, editable and never read by anything until this.
	var routedVia string
	if in.AssigneeID == nil && len(in.Components) > 0 {
		lead, component, err := componentLead(ctx, tx, projectID, in.Components)
		if err != nil {
			return domain.Issue{}, fmt.Errorf("component lead: %w", err)
		}
		if lead != nil {
			in.AssigneeID, routedVia = lead, component
		}
	}
	if in.AssigneeID == nil {
		in.AssigneeID = defaultAssignee
	}

	const insert = `
		INSERT INTO issues (
			project_id, number, type, title, description_md, status, severity, priority,
			reporter_id, assignee_id, version_affected,
			repro_steps_md, expected_md, actual_md, environment_md, source, dedupe_key, due_at,
			estimate_minutes
		) VALUES ($1,$2,$3,$4,$5,'open',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id, created_at, updated_at`
	issue := domain.Issue{
		ProjectKey: in.ProjectKey, Number: number, Type: in.Type, Title: in.Title,
		DescriptionMD: in.DescriptionMD, Status: domain.StatusOpen, Severity: in.Severity,
		Priority: in.Priority, VersionAffected: in.VersionAffected, ReproStepsMD: in.ReproStepsMD,
		ExpectedMD: in.ExpectedMD, ActualMD: in.ActualMD, EnvironmentMD: in.EnvironmentMD,
		Source: in.Source, Labels: in.Labels, Components: in.Components, DueAt: in.DueAt,
		EstimateMinutes: in.EstimateMinutes,
	}
	err = tx.QueryRow(ctx, insert,
		projectID, number, in.Type, in.Title, in.DescriptionMD, sevPtr(in.Severity), in.Priority,
		in.ReporterID, in.AssigneeID, in.VersionAffected,
		in.ReproStepsMD, in.ExpectedMD, in.ActualMD, in.EnvironmentMD, in.Source, in.DedupeKey, in.DueAt, in.EstimateMinutes,
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
	if len(in.Components) > 0 {
		if err := setIssueComponents(ctx, tx, projectID, issue.ID, in.Components, false); err != nil {
			return domain.Issue{}, fmt.Errorf("set components: %w", err)
		}
	}
	if err := recordActivity(ctx, tx, issue.ID, in.ReporterID, "issue.created", "issue", issue.ID); err != nil {
		return domain.Issue{}, err
	}
	// Say why it landed on them — nobody should have to guess who assigned this.
	if routedVia != "" {
		changes, err := json.Marshal(map[string]string{"reason": "component_lead", "component": routedVia})
		if err != nil {
			return domain.Issue{}, err
		}
		if err := recordActivityChanges(ctx, tx, issue.ID, in.ReporterID,
			"issue.auto_assigned", "issue", issue.ID, changes); err != nil {
			return domain.Issue{}, err
		}
	}
	body := strings.Join([]string{in.DescriptionMD, in.ReproStepsMD, in.ExpectedMD,
		in.ActualMD, in.EnvironmentMD}, "\n")
	if err := syncReferences(ctx, tx, issue.ID, nil, issue.Key, in.ReporterID, body); err != nil {
		return domain.Issue{}, fmt.Errorf("sync references: %w", err)
	}
	if err := syncMentions(ctx, tx, issue.ID, nil, in.ReporterID, body); err != nil {
		return domain.Issue{}, fmt.Errorf("sync mentions: %w", err)
	}
	// Auto-watch: the reporter and initial assignee follow their issue.
	if err := addWatcher(ctx, tx, issue.ID, in.ReporterID); err != nil {
		return domain.Issue{}, err
	}
	if in.AssigneeID != nil {
		if err := addWatcher(ctx, tx, issue.ID, *in.AssigneeID); err != nil {
			return domain.Issue{}, err
		}
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

func (s *Store) GetIssueByID(ctx context.Context, id uuid.UUID) (domain.Issue, error) {
	const q = selectIssue + ` WHERE i.id = $1 AND i.deleted_at IS NULL`
	return scanIssue(s.pool.QueryRow(ctx, q, id))
}

func (s *Store) ListIssues(ctx context.Context, f service.IssueFilter) ([]domain.Issue, int, error) {
	clause, args := issueWhere(f)

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM issues i JOIN projects p ON p.id = i.project_id WHERE `+clause, args...).
		Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, clampLimit(f.Limit), clampOffset(f.Offset))
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

// EachIssue streams every issue matching the filter, unpaged, calling fn per row.
// Export needs the whole result set, and materialising a 500-issue project into a slice
// just to serialise it one row at a time would be pointless — the rows come off the
// connection in order, so they can go straight out to the response.
func (s *Store) EachIssue(ctx context.Context, f service.IssueFilter, fn func(domain.Issue) error) error {
	clause, args := issueWhere(f)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`%s WHERE %s ORDER BY %s`,
		selectIssue, clause, orderBy(f.Sort)), args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return err
		}
		if err := fn(iss); err != nil {
			return err
		}
	}
	return rows.Err()
}

// worstSLAExpr collapses an issue_sla row's two states into the one a list renders.
// Ordered the same way domain.WorseSLAState orders them; kept as a constant so the SQL
// filter and the Go pill can never drift apart.
const worstSLAExpr = `CASE
	WHEN 'breached' IN (v.response_state, v.resolution_state) THEN 'breached'
	WHEN 'at_risk'  IN (v.response_state, v.resolution_state) THEN 'at_risk'
	WHEN 'ok'       IN (v.response_state, v.resolution_state) THEN 'ok'
	ELSE 'met' END`

// issueWhere builds the shared filter predicate and its arguments. Kept in one place so
// an export can never disagree with the list it was launched from.
func issueWhere(f service.IssueFilter) (string, []any) {
	// An empty project key means every project, which is what the cross-project
	// queue asks for; the dashboard's scope clause already worked this way.
	where := []string{"i.deleted_at IS NULL", "($1 = '' OR p.key = $1)"}
	// Archived issues are hidden from the default list; `is:archived` shows only them.
	if f.ShowArchived {
		where = append(where, "i.archived_at IS NOT NULL")
	} else {
		where = append(where, "i.archived_at IS NULL")
	}
	// Snoozed issues are hidden the same way, and `is:snoozed` shows only them. A
	// snooze whose time has passed reads as awake even before the job clears it, so a
	// late worker run cannot keep an issue buried.
	switch {
	case f.ShowSnoozed:
		where = append(where, "i.snoozed_until IS NOT NULL AND i.snoozed_until > now()")
	case !f.ShowArchived:
		where = append(where, "(i.snoozed_until IS NULL OR i.snoozed_until <= now())")
	}
	args := []any{f.ProjectKey}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if len(f.Statuses) > 0 {
		// A set, not a single value — `is:open` covers every non-terminal status.
		// Bound as text[] and cast, so one placeholder handles any set size.
		statuses := make([]string, 0, len(f.Statuses))
		for _, s := range f.Statuses {
			statuses = append(statuses, string(s))
		}
		add("i.status = ANY($%d::issue_status[])", statuses)
	}
	if f.AssigneeID != nil {
		add("i.assignee_id = $%d", *f.AssigneeID)
	}
	if f.ReporterID != nil {
		add("i.reporter_id = $%d", *f.ReporterID)
	}
	if f.Watching && f.MeUserID != nil {
		add("EXISTS (SELECT 1 FROM issue_watchers w WHERE w.issue_id = i.id AND w.user_id = $%d)", *f.MeUserID)
	}
	if f.Mentioned && f.MeUserID != nil {
		add("EXISTS (SELECT 1 FROM issue_mentions m WHERE m.issue_id = i.id AND m.user_id = $%d)", *f.MeUserID)
	}
	if f.Type != nil {
		add("i.type = $%d", string(*f.Type))
	}
	if f.Severity != nil {
		add("i.severity = $%d", string(*f.Severity))
	}
	if f.MilestoneID != nil {
		add("i.milestone_id = $%d", *f.MilestoneID)
	}
	if f.ReleaseID != nil {
		add("i.release_id = $%d", *f.ReleaseID)
	}
	if strings.TrimSpace(f.Label) != "" {
		add("EXISTS (SELECT 1 FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id AND lower(l.name) = lower($%d))", f.Label)
	}
	if strings.TrimSpace(f.Component) != "" {
		add("EXISTS (SELECT 1 FROM issue_components ic JOIN components c ON c.id = ic.component_id WHERE ic.issue_id = i.id AND lower(c.name) = lower($%d))", f.Component)
	}
	if strings.TrimSpace(f.Query) != "" {
		add("i.fts @@ websearch_to_tsquery('english', $%d)", f.Query)
	}
	// Overdue means "past its date and still not done" — an issue delivered a day late
	// is not overdue now, and listing it as such would make the queue useless.
	if f.DueOverdue {
		where = append(where, "i.due_at IS NOT NULL AND i.due_at < now() AND i.status NOT IN ('resolved','closed')")
	}
	if f.DueNone {
		where = append(where, "i.due_at IS NULL")
	}
	if f.DueAny {
		where = append(where, "i.due_at IS NOT NULL")
	}
	if f.DueBefore != nil {
		add("(i.due_at IS NOT NULL AND i.due_at <= $%d)", *f.DueBefore)
	}
	if f.DueAfter != nil {
		add("(i.due_at IS NOT NULL AND i.due_at >= $%d)", *f.DueAfter)
	}
	// Custom fields. Each term is its own EXISTS so several are ANDed rather than
	// fighting over one joined row — `field:team:infra field:tier:1` must mean both.
	for _, fp := range f.FieldPredicates {
		args = append(args, fp.DefinitionID)
		defParam := len(args)
		args = append(args, fp.Arg)
		valParam := len(args)
		match := fmt.Sprintf("v.%s = $%d%s", fp.Column, valParam, fp.Cast)
		if fp.ArrayContains {
			match = fmt.Sprintf("$%d = ANY(v.%s)", valParam, fp.Column)
		}
		where = append(where, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM issue_field_values v WHERE v.issue_id = i.id AND v.definition_id = $%d AND %s)",
			defParam, match))
	}
	// MatchNothing is a resolved-to-nothing constraint (`iteration:current` with no
	// active iteration). It has to narrow to empty, not disappear.
	if f.MatchNothing {
		where = append(where, "FALSE")
	}
	if f.IterationID != nil {
		add("i.iteration_id = $%d", *f.IterationID)
	}
	if f.IterationNone {
		where = append(where, "i.iteration_id IS NULL")
	}
	if f.EstimateNone {
		where = append(where, "i.estimate_minutes IS NULL")
	}
	if f.EstimateAny {
		where = append(where, "i.estimate_minutes IS NOT NULL")
	}
	if f.SpentOver != nil {
		add("COALESCE((SELECT tt.spent_minutes FROM issue_time tt WHERE tt.issue_id = i.id), 0) > $%d", *f.SpentOver)
	}
	if f.SpentUnder != nil {
		add("COALESCE((SELECT tt.spent_minutes FROM issue_time tt WHERE tt.issue_id = i.id), 0) < $%d", *f.SpentUnder)
	}
	if f.OverBudget != nil {
		// Only an estimated issue can be over budget. An unestimated one is unplanned,
		// which is a different problem and must not be swept in with `over-budget:false`.
		cond := `(i.estimate_minutes IS NOT NULL AND
		          COALESCE((SELECT tt.spent_minutes FROM issue_time tt WHERE tt.issue_id = i.id), 0) > i.estimate_minutes)`
		if !*f.OverBudget {
			cond = "NOT " + cond
		}
		where = append(where, cond)
	}
	if f.SLANone {
		where = append(where, "NOT EXISTS (SELECT 1 FROM issue_sla v WHERE v.issue_id = i.id)")
	}
	if len(f.SLAStates) > 0 {
		// Matched against the worse of the two states, which is what the single pill
		// in the list shows — filtering on something the row does not display would
		// look like a bug.
		add(`EXISTS (SELECT 1 FROM issue_sla v WHERE v.issue_id = i.id
		     AND `+worstSLAExpr+` = ANY($%d::text[]))`, f.SLAStates)
	}
	return strings.Join(where, " AND "), args
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
	tag, err := tx.Exec(ctx, upd, id, string(to))
	if err != nil {
		return domain.Issue{}, err
	}
	// No row means the issue was deleted between resolution and here. Bail before
	// writing an activity entry and publishing an event for a write that never landed.
	if tag.RowsAffected() == 0 {
		return domain.Issue{}, pgx.ErrNoRows
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
	row := s.pool.QueryRow(ctx, selectLiveIssue, id)
	return scanIssue(row)
}

// SetIssueArchived stamps or clears archived_at, records an activity entry, and runs
// publish in the same tx. Archiving hides the issue from default lists/search without
// changing its status or deleting it.
func (s *Store) SetIssueArchived(ctx context.Context, id, actor uuid.UUID, archived bool, publish service.PublishFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	set, verb := "archived_at = NULL", "issue.unarchived"
	if archived {
		set, verb = "archived_at = now()", "issue.archived"
	}
	tag, err := tx.Exec(ctx, `UPDATE issues SET `+set+`, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return domain.Issue{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Issue{}, pgx.ErrNoRows
	}
	if err := recordActivity(ctx, tx, id, actor, verb, "issue", id); err != nil {
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
	return scanIssue(s.pool.QueryRow(ctx, selectLiveIssue, id))
}

// ArchiveStaleClosed archives every non-archived, non-deleted issue whose closed_at is
// older than `days`, in one set-based statement, and writes one activity row per issue.
// Returns the number archived. Used by the daily auto-archive job.
func (s *Store) ArchiveStaleClosed(ctx context.Context, days int, actor uuid.UUID) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const q = `
		WITH archived AS (
			UPDATE issues SET archived_at = now(), updated_at = now()
			 WHERE closed_at IS NOT NULL
			   AND closed_at < now() - ($1 * interval '1 day')
			   AND archived_at IS NULL
			   AND deleted_at IS NULL
			RETURNING id
		), logged AS (
			INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
			SELECT id, $2, 'issue.archived', 'issue', id, '{}' FROM archived
		)
		SELECT count(*) FROM archived`
	var n int
	if err := tx.QueryRow(ctx, q, days, actor).Scan(&n); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

// UpdateIssue applies a partial update (COALESCE keeps unchanged fields), records an
// "issue.updated" timeline entry, enqueues the event, and returns the refreshed issue.
func (s *Store) UpdateIssue(ctx context.Context, id, actor uuid.UUID, in service.UpdateIssueInput, publish service.PublishFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Guard against cross-project dangling pointers: an assigned milestone must
	// belong to the issue's own project.
	if in.MilestoneID != nil && *in.MilestoneID != uuid.Nil {
		var ok bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM milestones m JOIN issues i ON i.project_id = m.project_id
			 WHERE m.id = $1 AND i.id = $2)`, *in.MilestoneID, id).Scan(&ok); err != nil {
			return domain.Issue{}, err
		}
		if !ok {
			return domain.Issue{}, fmt.Errorf("milestone does not belong to the issue's project")
		}
	}
	if in.ReleaseID != nil && *in.ReleaseID != uuid.Nil {
		var ok bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM releases r JOIN issues i ON i.project_id = r.project_id
			 WHERE r.id = $1 AND i.id = $2)`, *in.ReleaseID, id).Scan(&ok); err != nil {
			return domain.Issue{}, err
		}
		if !ok {
			return domain.Issue{}, fmt.Errorf("release does not belong to the issue's project")
		}
	}

	const q = `
		UPDATE issues SET
		  title            = COALESCE($2, title),
		  description_md   = COALESCE($3, description_md),
		  type             = COALESCE($4::issue_type, type),
		  severity         = COALESCE($5::severity, severity),
		  priority         = COALESCE($6::priority, priority),
		  -- nil = unchanged; the zero UUID clears the assignee; otherwise assign.
		  -- $7::uuid so Postgres can determine the parameter type (else 42P08).
		  assignee_id      = CASE
		                       WHEN $7::uuid IS NULL THEN assignee_id
		                       WHEN $7::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
		                       ELSE $7::uuid END,
		  version_affected = COALESCE($8, version_affected),
		  version_fixed    = COALESCE($9, version_fixed),
		  repro_steps_md   = COALESCE($10, repro_steps_md),
		  expected_md      = COALESCE($11, expected_md),
		  actual_md        = COALESCE($12, actual_md),
		  environment_md   = COALESCE($13, environment_md),
		  milestone_id     = CASE
		                       WHEN $14::uuid IS NULL THEN milestone_id
		                       WHEN $14::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
		                       ELSE $14::uuid END,
		  release_id       = CASE
		                       WHEN $15::uuid IS NULL THEN release_id
		                       WHEN $15::uuid = '00000000-0000-0000-0000-000000000000'::uuid THEN NULL
		                       ELSE $15::uuid END,
		  -- Due date follows the same three-way convention as the id fields, since
		  -- "clear the due date" and "leave it alone" are different requests: $16 nil
		  -- is unchanged, Go's zero time clears, anything else sets.
		  due_at           = CASE
		                       WHEN $16::timestamptz IS NULL THEN due_at
		                       WHEN $16::timestamptz = '0001-01-01 00:00:00+00'::timestamptz THEN NULL
		                       ELSE $16::timestamptz END,
		  -- Same three-way convention again: nil unchanged, 0 clears, >0 sets. An
		  -- estimate of zero is not a real estimate, so it is free to mean "remove".
		  estimate_minutes = CASE
		                       WHEN $17::int IS NULL THEN estimate_minutes
		                       WHEN $17::int = 0 THEN NULL
		                       ELSE $17::int END,
		  updated_at       = now()
		WHERE id = $1 AND deleted_at IS NULL`
	tag, err := tx.Exec(ctx, q, id,
		in.Title, in.DescriptionMD, typePtr(in.Type), sevPtr(in.Severity), prioPtr(in.Priority),
		in.AssigneeID, in.VersionAffected, in.VersionFixed,
		in.ReproStepsMD, in.ExpectedMD, in.ActualMD, in.EnvironmentMD,
		in.MilestoneID, in.ReleaseID, in.DueAt, in.EstimateMinutes)
	if err != nil {
		return domain.Issue{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Issue{}, pgx.ErrNoRows
	}
	if in.Labels != nil || in.Components != nil {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1`, id).Scan(&projectID); err != nil {
			return domain.Issue{}, err
		}
		if in.Labels != nil {
			if err := setIssueLabels(ctx, tx, projectID, id, *in.Labels, true); err != nil {
				return domain.Issue{}, fmt.Errorf("set labels: %w", err)
			}
		}
		if in.Components != nil {
			if err := setIssueComponents(ctx, tx, projectID, id, *in.Components, true); err != nil {
				return domain.Issue{}, fmt.Errorf("set components: %w", err)
			}
		}
	}
	if err := recordActivity(ctx, tx, id, actor, "issue.updated", "issue", id); err != nil {
		return domain.Issue{}, err
	}
	// The patch is partial, so the post-update prose is the only reliable source.
	key, body, err := issueProse(ctx, tx, id)
	if err != nil {
		return domain.Issue{}, err
	}
	if err := syncReferences(ctx, tx, id, nil, key, actor, body); err != nil {
		return domain.Issue{}, fmt.Errorf("sync references: %w", err)
	}
	if err := syncMentions(ctx, tx, id, nil, actor, body); err != nil {
		return domain.Issue{}, fmt.Errorf("sync mentions: %w", err)
	}
	// Auto-watch: a newly assigned user follows the issue.
	if in.AssigneeID != nil && *in.AssigneeID != uuid.Nil {
		if err := addWatcher(ctx, tx, id, *in.AssigneeID); err != nil {
			return domain.Issue{}, err
		}
	}
	if publish != nil {
		if err := publish(tx); err != nil {
			return domain.Issue{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Issue{}, err
	}
	return scanIssue(s.pool.QueryRow(ctx, selectLiveIssue, id))
}

// MoveIssue re-homes an issue into another project in one transaction. Because an issue's
// human key is (project.key, number) and the table enforces UNIQUE(project_id, number),
// the issue is given a fresh number allocated from the target project (mirroring
// CreateIssue) — so its key changes. Milestone, release and components are project-scoped,
// so they're cleared (they'd otherwise dangle to the source project); labels are re-mapped
// by name into the target project's scope. Records "issue.moved" and enqueues the event.
func (s *Store) MoveIssue(ctx context.Context, id, actor uuid.UUID, targetProjectKey string, publish service.PublishFn) (domain.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Snapshot the label names before the move so they can travel to the new project.
	var labelNames []string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(array(SELECT l.name FROM issue_labels il JOIN labels l ON l.id = il.label_id
		                       WHERE il.issue_id = $1 ORDER BY l.name), '{}')`, id).Scan(&labelNames); err != nil {
		return domain.Issue{}, err
	}

	// Allocate the next per-project number in the destination — the old number would
	// collide with UNIQUE(project_id, number) in most projects.
	var targetProjectID uuid.UUID
	var number int32
	if err := tx.QueryRow(ctx,
		`UPDATE projects SET next_issue_number = next_issue_number + 1, updated_at = now()
		 WHERE key = $1 RETURNING id, next_issue_number - 1`, targetProjectKey).
		Scan(&targetProjectID, &number); err != nil {
		return domain.Issue{}, fmt.Errorf("allocate number: %w", err)
	}

	// Re-home the issue; NULL the project-scoped milestone/release (they belong to the
	// source project). deleted_at guard mirrors the other issue writes.
	tag, err := tx.Exec(ctx,
		`UPDATE issues SET project_id = $2, number = $3, milestone_id = NULL, release_id = NULL, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`, id, targetProjectID, number)
	if err != nil {
		return domain.Issue{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Issue{}, pgx.ErrNoRows
	}

	// Components reference the source project; drop the associations.
	if _, err := tx.Exec(ctx, `DELETE FROM issue_components WHERE issue_id = $1`, id); err != nil {
		return domain.Issue{}, err
	}
	// Re-create the label set under the target project's scope.
	if err := setIssueLabels(ctx, tx, targetProjectID, id, labelNames, true); err != nil {
		return domain.Issue{}, fmt.Errorf("remap labels: %w", err)
	}
	if err := recordActivity(ctx, tx, id, actor, "issue.moved", "issue", id); err != nil {
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
	return scanIssue(s.pool.QueryRow(ctx, selectLiveIssue, id))
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
	// First response is the first comment by somebody other than the reporter — a
	// reporter adding "any update?" is not the team responding. Stamped once and only
	// once, in the same transaction as the comment, so the two can never disagree.
	if _, err := tx.Exec(ctx,
		`UPDATE issues SET first_response_at = $2
		  WHERE id = $1 AND first_response_at IS NULL AND reporter_id IS DISTINCT FROM $3`,
		issueID, c.CreatedAt, author); err != nil {
		return domain.Comment{}, err
	}
	// Auto-watch: commenting subscribes you to the conversation.
	if err := addWatcher(ctx, tx, issueID, author); err != nil {
		return domain.Comment{}, err
	}
	key, _, err := issueProse(ctx, tx, issueID)
	if err != nil {
		return domain.Comment{}, err
	}
	if err := syncReferences(ctx, tx, issueID, &c.ID, key, author, body); err != nil {
		return domain.Comment{}, fmt.Errorf("sync references: %w", err)
	}
	if err := syncMentions(ctx, tx, issueID, &c.ID, author, body); err != nil {
		return domain.Comment{}, fmt.Errorf("sync mentions: %w", err)
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

// ListComments returns one page of an issue's comments plus the unpaged total, so
// clients can page instead of silently seeing a truncated conversation.
func (s *Store) ListComments(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Comment, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM comments WHERE issue_id = $1 AND deleted_at IS NULL`, issueID).
		Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.issue_id, c.author_id, u.display_name, u.email, c.body_md, c.edited_at, c.created_at
		 FROM comments c LEFT JOIN users u ON u.id = c.author_id
		 WHERE c.issue_id = $1 AND c.deleted_at IS NULL
		 ORDER BY c.created_at LIMIT $2 OFFSET $3`, issueID, clampLimit(limit), clampOffset(offset))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Comment
	for rows.Next() {
		var c domain.Comment
		var authorID *uuid.UUID
		var displayName, email *string
		if err := rows.Scan(&c.ID, &c.IssueID, &authorID, &displayName, &email, &c.BodyMD, &c.EditedAt, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		if authorID != nil {
			c.Author = &domain.User{ID: *authorID, DisplayName: deref(displayName), Email: deref(email)}
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// GetComment returns a live (non-deleted) comment, with the owning project's
// key resolved for permission checks.
func (s *Store) GetComment(ctx context.Context, id uuid.UUID) (domain.Comment, error) {
	const q = `
		SELECT c.id, c.issue_id, c.author_id, u.display_name, u.email, c.body_md, c.edited_at, c.created_at, p.key
		FROM comments c
		LEFT JOIN users u ON u.id = c.author_id
		JOIN issues i ON i.id = c.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE c.id = $1 AND c.deleted_at IS NULL`
	var c domain.Comment
	var authorID *uuid.UUID
	var displayName, email *string
	if err := s.pool.QueryRow(ctx, q, id).
		Scan(&c.ID, &c.IssueID, &authorID, &displayName, &email, &c.BodyMD, &c.EditedAt, &c.CreatedAt, &c.ProjectKey); err != nil {
		return domain.Comment{}, err
	}
	if authorID != nil {
		c.Author = &domain.User{ID: *authorID, DisplayName: deref(displayName), Email: deref(email)}
	}
	return c, nil
}

// UpdateComment replaces the body and stamps edited_at; records comment.edited.
func (s *Store) UpdateComment(ctx context.Context, id, actor uuid.UUID, bodyMD string) (domain.Comment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Comment{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var issueID uuid.UUID
	if err := tx.QueryRow(ctx,
		`UPDATE comments SET body_md = $2, edited_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING issue_id`, id, bodyMD).Scan(&issueID); err != nil {
		return domain.Comment{}, err
	}
	if err := recordActivity(ctx, tx, issueID, actor, "comment.edited", "comment", id); err != nil {
		return domain.Comment{}, err
	}
	key, _, err := issueProse(ctx, tx, issueID)
	if err != nil {
		return domain.Comment{}, err
	}
	if err := syncReferences(ctx, tx, issueID, &id, key, actor, bodyMD); err != nil {
		return domain.Comment{}, fmt.Errorf("sync references: %w", err)
	}
	if err := syncMentions(ctx, tx, issueID, &id, actor, bodyMD); err != nil {
		return domain.Comment{}, fmt.Errorf("sync mentions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, err
	}
	return s.GetComment(ctx, id)
}

// SoftDeleteComment hides the comment (deleted_at) and records comment.deleted.
func (s *Store) SoftDeleteComment(ctx context.Context, id, actor uuid.UUID) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var issueID uuid.UUID
	err = tx.QueryRow(ctx,
		`UPDATE comments SET deleted_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING issue_id`, id).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := recordActivity(ctx, tx, issueID, actor, "comment.deleted", "comment", id); err != nil {
		return false, err
	}
	// Comments are soft-deleted, so the ON DELETE CASCADE on source_comment_id never
	// fires: without this, deleting a comment leaves its cross-references showing in
	// the target's "Referenced by" panel, pointing at text nobody can read — and an
	// unclaimed mention would still notify for a comment that no longer exists.
	// Derived rows follow the prose they were derived from.
	for _, q := range []string{
		`DELETE FROM issue_references WHERE source_comment_id = $1`,
		`DELETE FROM issue_mentions WHERE source_comment_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

// ListActivity returns one page of an issue's timeline plus the unpaged total.
func (s *Store) ListActivity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM activity WHERE issue_id = $1`, issueID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT a.id, a.issue_id, a.actor_id, u.display_name, u.email, a.verb, a.entity_type,
		        a.changes, a.occurred_at, p.key, i.number
		 FROM activity a
		 LEFT JOIN users u ON u.id = a.actor_id
		 LEFT JOIN issues i ON i.id = a.issue_id
		 LEFT JOIN projects p ON p.id = i.project_id
		 WHERE a.issue_id = $1 ORDER BY a.occurred_at DESC LIMIT $2 OFFSET $3`,
		issueID, clampLimit(limit), clampOffset(offset))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	acts, err := scanActivityRows(rows)
	return acts, total, err
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

// RecentActivity returns the newest timeline entries. An empty projectKey spans
// every project; otherwise the feed is scoped to that project's issues.
func (s *Store) RecentActivity(ctx context.Context, projectKey string, limit int32) ([]domain.Activity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.id, a.issue_id, a.actor_id, u.display_name, u.email, a.verb, a.entity_type, a.changes, a.occurred_at,
		        p.key, i.number
		 FROM activity a
		 LEFT JOIN users u ON u.id = a.actor_id
		 LEFT JOIN issues i ON i.id = a.issue_id
		 LEFT JOIN projects p ON p.id = i.project_id
		 WHERE $1 = '' OR p.key = $1
		 ORDER BY a.occurred_at DESC LIMIT $2`, projectKey, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityRows(rows)
}

// Dashboard aggregates health metrics. An empty projectKey spans every project;
// otherwise every figure is scoped to that one project — the UI labels this view
// per-project, so an unscoped count would be quietly wrong on a multi-project
// install. Archived issues are excluded throughout, matching lists and search.
func (s *Store) Dashboard(ctx context.Context, projectKey string) (domain.Dashboard, error) {
	d := domain.Dashboard{
		ProjectKey:          projectKey,
		IssuesByStatus:      map[string]int{},
		IssuesByComponent:   map[string]int{},
		TeamWorkload:        map[string]int{},
		RemainingByAssignee: map[string]int{},
	}

	// $1 = '' means "all projects"; the subquery resolves the scope once per query.
	const scope = `i.deleted_at IS NULL AND i.archived_at IS NULL
	               AND ($1 = '' OR i.project_id = (SELECT id FROM projects WHERE key = $1))`

	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE i.status NOT IN ('resolved','closed')),
		        count(*) FILTER (WHERE i.severity = 'critical' AND i.status NOT IN ('resolved','closed'))
		 FROM issues i WHERE `+scope, projectKey).Scan(&d.OpenIssues, &d.CriticalIssues); err != nil {
		return d, err
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(EXTRACT(EPOCH FROM avg(i.resolved_at - i.created_at)) / 3600, 0),
		        COALESCE(EXTRACT(EPOCH FROM avg(i.resolved_at - i.created_at)
		                 FILTER (WHERE i.resolved_at > now() - interval '30 days')) / 3600, 0)
		 FROM issues i WHERE i.resolved_at IS NOT NULL AND `+scope, projectKey).
		Scan(&d.AvgResolutionHours, &d.MTTRHours); err != nil {
		return d, err
	}

	// Overdue and SLA standing, counted over open issues only. A breach already paid
	// for by shipping late is history; a band that counted those would never go down
	// and would stop meaning anything within a month.
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE i.due_at IS NOT NULL AND i.due_at < now()),
		        count(*) FILTER (WHERE `+worstSLAExpr+` = 'at_risk'),
		        count(*) FILTER (WHERE `+worstSLAExpr+` = 'breached')
		   FROM issues i
		   LEFT JOIN issue_sla v ON v.issue_id = i.id
		  WHERE `+scope+` AND i.status NOT IN ('resolved','closed')`, projectKey).
		Scan(&d.OverdueIssues, &d.SLAAtRiskIssues, &d.SLABreachedIssues); err != nil {
		return d, err
	}

	var reopened, terminal int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE i.status = 'reopened'),
		        count(*) FILTER (WHERE i.status IN ('resolved','closed','reopened'))
		 FROM issues i WHERE `+scope, projectKey).Scan(&reopened, &terminal); err != nil {
		return d, err
	}
	if terminal > 0 {
		d.RegressionRate = float64(reopened) / float64(terminal)
	}

	if err := scanCountMap(ctx, s, d.IssuesByStatus,
		`SELECT i.status::text, count(*) FROM issues i WHERE `+scope+` GROUP BY i.status`, projectKey); err != nil {
		return d, err
	}
	if err := scanCountMap(ctx, s, d.IssuesByComponent,
		`SELECT c.name, count(*) FROM issue_components ic
		   JOIN components c ON c.id = ic.component_id
		   JOIN issues i ON i.id = ic.issue_id
		 WHERE `+scope+` GROUP BY c.name`, projectKey); err != nil {
		return d, err
	}
	if err := scanCountMap(ctx, s, d.TeamWorkload,
		`SELECT u.display_name, count(*) FROM issues i
		   JOIN users u ON u.id = i.assignee_id
		 WHERE `+scope+` AND i.status NOT IN ('resolved','closed')
		 GROUP BY u.display_name`, projectKey); err != nil {
		return d, err
	}
	remaining, err := s.RemainingByAssignee(ctx, projectKey)
	if err != nil {
		return d, err
	}
	d.RemainingByAssignee = remaining

	acts, err := s.RecentActivity(ctx, projectKey, 12)
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

func (s *Store) UpdateUserRole(ctx context.Context, userID uuid.UUID, role domain.Role) (domain.User, error) {
	const q = `UPDATE users SET role = $2::app_role, updated_at = now()
	           WHERE id = $1
	           RETURNING id, identity_sub, email, display_name, avatar_url, role`
	var u domain.User
	err := s.pool.QueryRow(ctx, q, userID, string(role)).
		Scan(&u.ID, &u.IdentitySub, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Role)
	return u, err
}

func scanCountMap(ctx context.Context, s *Store, dst map[string]int, query string, args ...any) error {
	rows, err := s.pool.Query(ctx, query, args...)
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
	       i.created_at, i.updated_at, i.archived_at, i.snoozed_until, i.snooze_note,
	       COALESCE(i.rank, ''),
	       i.due_at, i.first_response_at, i.resolved_at,
	       sla.response_due, sla.resolution_due, sla.response_state, sla.resolution_state,
	       i.estimate_minutes, COALESCE(tm.spent_minutes, 0),
	       i.iteration_id, itr.name,
	       ru.id, ru.display_name, ru.email,
	       au.id, au.display_name, au.email,
	       COALESCE(array(SELECT l.name FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id ORDER BY l.name), '{}') AS labels,
	       COALESCE(array(SELECT c.name FROM issue_components ic JOIN components c ON c.id = ic.component_id WHERE ic.issue_id = i.id ORDER BY c.name), '{}') AS components,
	       i.milestone_id, m.title, i.release_id, r.version,
	       (SELECT count(*) FROM issue_relations rel
	          JOIN issues b ON b.id = CASE WHEN rel.kind = 'blocks' AND rel.to_issue = i.id THEN rel.from_issue
	                                       WHEN rel.kind = 'blocked_by' AND rel.from_issue = i.id THEN rel.to_issue END
	         WHERE b.deleted_at IS NULL AND b.status NOT IN ('resolved','closed')) AS open_blockers
	FROM issues i
	JOIN projects p ON p.id = i.project_id
	LEFT JOIN users ru ON ru.id = i.reporter_id
	LEFT JOIN users au ON au.id = i.assignee_id
	LEFT JOIN milestones m ON m.id = i.milestone_id
	LEFT JOIN releases r ON r.id = i.release_id
	LEFT JOIN issue_sla sla ON sla.issue_id = i.id
	LEFT JOIN issue_time tm ON tm.issue_id = i.id
	LEFT JOIN iterations itr ON itr.id = i.iteration_id`

// selectLiveIssue re-reads one issue by id after a write. The deleted_at guard
// matters: every issue write is already conditioned on `deleted_at IS NULL`, so
// reading back without it could return a stale row for a write that never landed.
const selectLiveIssue = selectIssue + ` WHERE i.id = $1 AND i.deleted_at IS NULL`

type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(row scanner) (domain.Issue, error) {
	var i domain.Issue
	var sev *string
	var reporterID, assigneeID *uuid.UUID
	var reporterName, reporterEmail, assigneeName, assigneeEmail, milestoneTitle, releaseVersion *string
	var respState, resoState, iterationName *string
	var respDue, resoDue *time.Time
	err := row.Scan(
		&i.ID, &i.ProjectKey, &i.Number, &i.Type, &i.Title, &i.DescriptionMD, &i.Status, &sev, &i.Priority,
		&i.VersionAffected, &i.VersionFixed, &i.GitCommitSHA, &i.PullRequestURL,
		&i.ReproStepsMD, &i.ExpectedMD, &i.ActualMD, &i.EnvironmentMD, &i.Source,
		&i.CreatedAt, &i.UpdatedAt, &i.ArchivedAt, &i.SnoozedUntil, &i.SnoozeNote, &i.Rank,
		&i.DueAt, &i.FirstResponseAt, &i.ResolvedAt,
		&respDue, &resoDue, &respState, &resoState,
		&i.EstimateMinutes, &i.SpentMinutes,
		&i.IterationID, &iterationName,
		&reporterID, &reporterName, &reporterEmail,
		&assigneeID, &assigneeName, &assigneeEmail,
		&i.Labels, &i.Components,
		&i.MilestoneID, &milestoneTitle, &i.ReleaseID, &releaseVersion,
		&i.OpenBlockers,
	)
	if err != nil {
		return domain.Issue{}, err
	}
	i.Milestone = deref(milestoneTitle)
	i.Release = deref(releaseVersion)
	i.Iteration = deref(iterationName)
	// Both states come from the same LEFT JOIN row, so either both are present or the
	// project has no policy for this issue and it carries no SLA at all.
	if respState != nil && resoState != nil {
		i.SLA = &domain.IssueSLA{
			ResponseDue: respDue, ResolutionDue: resoDue,
			ResponseState: *respState, ResolutionState: *resoState,
			State: domain.WorseSLAState(*respState, *resoState),
		}
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
	return recordActivityChanges(ctx, tx, issueID, actor, verb, entityType, entityID, []byte("{}"))
}

// recordActivityChanges is recordActivity with a payload, for verbs whose line in the
// timeline needs more than the verb to make sense.
func recordActivityChanges(
	ctx context.Context, tx pgx.Tx, issueID, actor uuid.UUID,
	verb, entityType string, entityID uuid.UUID, changes []byte,
) error {
	if len(changes) == 0 {
		changes = []byte("{}")
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO activity (issue_id, actor_id, verb, entity_type, entity_id, changes)
		 VALUES ($1,$2,$3,$4,$5,$6)`, issueID, actor, verb, entityType, entityID, changes)
	return err
}

// componentLead returns the lead of the first named component that has one. Order
// follows the caller's list rather than the database's, so an issue filed against
// ["api", "web"] always routes to the api lead instead of whichever row came back
// first.
func componentLead(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, names []string) (*uuid.UUID, string, error) {
	rows, err := tx.Query(ctx,
		`SELECT name, lead_id FROM components
		 WHERE project_id = $1 AND lead_id IS NOT NULL AND name = ANY($2)`, projectID, names)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	leads := make(map[string]uuid.UUID, len(names))
	for rows.Next() {
		var name string
		var lead uuid.UUID
		if err := rows.Scan(&name, &lead); err != nil {
			return nil, "", err
		}
		leads[name] = lead
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	for _, name := range names {
		if lead, ok := leads[name]; ok {
			id := lead
			return &id, name, nil
		}
	}
	return nil, "", nil
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
	case "due":
		// Soonest first, and issues with no due date last — a queue sorted by deadline
		// that opens on the undated ones is answering a different question.
		return "i.due_at ASC NULLS LAST, i.created_at DESC"
	case "rank":
		// Board order. Unranked issues sort last and fall back to recency, so a
		// project that has never been reordered looks exactly as it does today and
		// needs no backfill.
		return "i.rank ASC NULLS LAST, i.updated_at DESC"
	default:
		return "i.created_at DESC"
	}
}

// clampLimit bounds a page size: unset/invalid falls back to the default, and anything
// over the ceiling clamps *down* to it (asking for 500 should give you 200, not silently
// drop you to the default).
func clampLimit(l int32) int32 {
	switch {
	case l <= 0:
		return 50
	case l > 200:
		return 200
	default:
		return l
	}
}

// clampOffset guards against a negative OFFSET (Postgres errors on it).
func clampOffset(o int32) int32 {
	if o < 0 {
		return 0
	}
	return o
}

var _ = time.Now // reserved for future time-based helpers

// SetIssueRank writes a card's manual board position. Ranking is a view preference
// rather than a change to the issue, so it records no activity and emits no event —
// a timeline full of "moved a card up" entries would bury the things that matter.
func (s *Store) SetIssueRank(ctx context.Context, id uuid.UUID, rank string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE issues SET rank = $2 WHERE id = $1 AND deleted_at IS NULL`, id, rank)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// NeighbourRanks returns the ranks of the issues either side of a drop position within
// one board column, identified by the keys the client saw. Reading them server-side
// keeps the decision on one side of the wire: the client says "between these two
// cards", not "here is the rank I computed".
func (s *Store) NeighbourRanks(ctx context.Context, projectKey, beforeKey, afterKey string) (string, string, error) {
	rankOf := func(key string) (string, error) {
		if key == "" {
			return "", nil
		}
		var rank string
		err := s.pool.QueryRow(ctx,
			`SELECT COALESCE(i.rank, '') FROM issues i JOIN projects p ON p.id = i.project_id
			  WHERE p.key || '-' || i.number = $1 AND i.deleted_at IS NULL`, key).Scan(&rank)
		if errors.Is(err, pgx.ErrNoRows) {
			// The neighbour was deleted or moved between render and drop. Treating it
			// as absent puts the card at that end of the column, which is closer to
			// what the user asked for than refusing the drag.
			return "", nil
		}
		return rank, err
	}
	before, err := rankOf(beforeKey)
	if err != nil {
		return "", "", err
	}
	after, err := rankOf(afterKey)
	return before, after, err
}
