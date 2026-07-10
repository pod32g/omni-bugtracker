package pg

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── automation rules ──

const selectRule = `
	SELECT r.id, COALESCE(p.key, ''), r.name, r.is_active, r.priority, r.trigger, r.actions, r.created_at
	FROM automation_rules r
	LEFT JOIN projects p ON p.id = r.project_id`

func scanRule(row scanner) (domain.AutomationRule, error) {
	var r domain.AutomationRule
	err := row.Scan(&r.ID, &r.ProjectKey, &r.Name, &r.IsActive, &r.Priority, &r.Trigger, &r.Actions, &r.CreatedAt)
	return r, err
}

func (s *Store) ListAutomationRules(ctx context.Context) ([]domain.AutomationRule, error) {
	rows, err := s.pool.Query(ctx, selectRule+` ORDER BY r.priority, r.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AutomationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MatchingAutomationRules returns active rules whose scope covers the project
// and whose trigger event matches (or is the "*" wildcard), ordered by priority.
func (s *Store) MatchingAutomationRules(ctx context.Context, projectKey, event string) ([]domain.AutomationRule, error) {
	rows, err := s.pool.Query(ctx, selectRule+`
		WHERE r.is_active
		  AND (r.project_id IS NULL OR p.key = $1)
		  AND (r.trigger->>'event' = $2 OR r.trigger->>'event' = '*')
		ORDER BY r.priority, r.created_at`, projectKey, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AutomationRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateAutomationRule(ctx context.Context, in service.CreateAutomationRuleInput) (domain.AutomationRule, error) {
	const q = `
		INSERT INTO automation_rules (project_id, name, priority, trigger, actions, created_by)
		VALUES ((SELECT id FROM projects WHERE key = NULLIF($1, '')), $2, $3, $4, $5, $6)
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, strings.TrimSpace(in.ProjectKey), in.Name, in.Priority,
		in.Trigger, in.Actions, in.CreatedBy).Scan(&id); err != nil {
		return domain.AutomationRule{}, err
	}
	return scanRule(s.pool.QueryRow(ctx, selectRule+` WHERE r.id = $1`, id))
}

func (s *Store) UpdateAutomationRule(ctx context.Context, in service.UpdateAutomationRuleInput) (domain.AutomationRule, error) {
	const q = `
		UPDATE automation_rules SET
		  name       = COALESCE($2, name),
		  priority   = COALESCE($3, priority),
		  is_active  = COALESCE($4, is_active),
		  trigger    = COALESCE($5, trigger),
		  actions    = COALESCE($6, actions),
		  updated_at = now()
		WHERE id = $1
		RETURNING id`
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, q, in.ID, in.Name, in.Priority, in.IsActive,
		in.Trigger, in.Actions).Scan(&id); err != nil {
		return domain.AutomationRule{}, err
	}
	return scanRule(s.pool.QueryRow(ctx, selectRule+` WHERE r.id = $1`, id))
}

func (s *Store) DeleteAutomationRule(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM automation_rules WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) ListAutomationRuns(ctx context.Context, limit int32) ([]domain.AutomationRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT run.id, run.rule_id, r.name, COALESCE(p.key || '-' || i.number, ''), run.status, run.log, run.ran_at
		FROM automation_runs run
		JOIN automation_rules r ON r.id = run.rule_id
		LEFT JOIN issues i ON i.id = run.issue_id
		LEFT JOIN projects p ON p.id = i.project_id
		ORDER BY run.ran_at DESC LIMIT $1`, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AutomationRun
	for rows.Next() {
		var r domain.AutomationRun
		if err := rows.Scan(&r.ID, &r.RuleID, &r.RuleName, &r.IssueKey, &r.Status, &r.Log, &r.RanAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RecordAutomationRun(ctx context.Context, ruleID, issueID uuid.UUID, status string, log []byte) error {
	if len(log) == 0 {
		log = []byte(`{}`)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO automation_runs (rule_id, issue_id, status, log) VALUES ($1, $2, $3, $4)`,
		ruleID, issueID, status, log)
	return err
}

// EnsureBotUser upserts a system account (automation/observability actors).
func (s *Store) EnsureBotUser(ctx context.Context, identitySub, displayName, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (identity_sub, email, display_name, role)
		VALUES ($1, $2, $3, 'bot')
		ON CONFLICT (identity_sub) DO UPDATE SET last_seen_at = now()
		RETURNING id`, identitySub, email, displayName).Scan(&id)
	return id, err
}
