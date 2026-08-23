package pg

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

const selectSLAPolicy = `
	SELECT s.id, p.key, s.severity, s.issue_type, s.response_minutes, s.resolution_minutes,
	       s.is_active, s.created_at
	  FROM sla_policies s JOIN projects p ON p.id = s.project_id`

func scanSLAPolicy(row scanner) (domain.SLAPolicy, error) {
	var pol domain.SLAPolicy
	var sev, typ *string
	if err := row.Scan(&pol.ID, &pol.ProjectKey, &sev, &typ, &pol.ResponseMinutes,
		&pol.ResolutionMinutes, &pol.IsActive, &pol.CreatedAt); err != nil {
		return domain.SLAPolicy{}, err
	}
	if sev != nil {
		s := domain.Severity(*sev)
		pol.Severity = &s
	}
	if typ != nil {
		t := domain.IssueType(*typ)
		pol.Type = &t
	}
	return pol, nil
}

// ListSLAPolicies returns a project's policies most-specific first, which is the order
// they are actually applied in — a settings page that listed them any other way would
// not show which row wins.
func (s *Store) ListSLAPolicies(ctx context.Context, projectKey string) ([]domain.SLAPolicy, error) {
	rows, err := s.pool.Query(ctx, selectSLAPolicy+`
		 WHERE p.key = $1
		 ORDER BY (s.severity IS NOT NULL)::INT + (s.issue_type IS NOT NULL)::INT DESC,
		          s.severity NULLS LAST, s.issue_type NULLS LAST`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SLAPolicy
	for rows.Next() {
		pol, err := scanSLAPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pol)
	}
	return out, rows.Err()
}

func (s *Store) CreateSLAPolicy(ctx context.Context, in service.SLAPolicyInput) (domain.SLAPolicy, error) {
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO sla_policies (project_id, severity, issue_type, response_minutes, resolution_minutes)
		 SELECT p.id, $2::severity, $3::issue_type, $4, $5 FROM projects p WHERE p.key = $1
		 RETURNING id`,
		in.ProjectKey, sevPtr(in.Severity), typePtr(in.Type), in.ResponseMinutes, in.ResolutionMinutes).
		Scan(&id); err != nil {
		return domain.SLAPolicy{}, err
	}
	return scanSLAPolicy(s.pool.QueryRow(ctx, selectSLAPolicy+` WHERE s.id = $1`, id))
}

func (s *Store) UpdateSLAPolicy(ctx context.Context, id uuid.UUID, in service.UpdateSLAPolicyInput) (domain.SLAPolicy, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sla_policies SET
		   response_minutes   = COALESCE($2, response_minutes),
		   resolution_minutes = COALESCE($3, resolution_minutes),
		   is_active          = COALESCE($4, is_active)
		 WHERE id = $1`, id, in.ResponseMinutes, in.ResolutionMinutes, in.IsActive)
	if err != nil {
		return domain.SLAPolicy{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.SLAPolicy{}, pgx.ErrNoRows
	}
	return scanSLAPolicy(s.pool.QueryRow(ctx, selectSLAPolicy+` WHERE s.id = $1`, id))
}

func (s *Store) DeleteSLAPolicy(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sla_policies WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimSLAEscalations finds open issues that have crossed a threshold and claims each
// (issue, kind) exactly once, returning only what this run is responsible for
// announcing. The claim is the INSERT: two workers racing on the same issue both run
// the SELECT, but only one gets the row back from ON CONFLICT DO NOTHING, so a
// breach is never announced twice.
// enqueue runs inside the claiming transaction, and that is the whole point: the claim
// used to commit and the caller then looped over the results inserting jobs, so an
// insert that failed partway left the remaining escalations claimed-but-unannounced.
// Permanently — the next sweep skips them precisely because they were claimed. The
// comment above once said "a re-run after a crash re-announces nothing", which was
// true and was also the bug.
func (s *Store) ClaimSLAEscalations(
	ctx context.Context, enqueue func(pgx.Tx, []service.SLAEscalation) error,
) ([]service.SLAEscalation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `
		WITH due AS (
			SELECT v.issue_id, k.kind
			  FROM issue_sla v
			  JOIN issues i ON i.id = v.issue_id
			  CROSS JOIN LATERAL (VALUES
			      ('response_warning',   v.response_state   = 'at_risk'),
			      ('response_breached',  v.response_state   = 'breached'),
			      ('resolution_warning', v.resolution_state = 'at_risk'),
			      ('resolution_breached', v.resolution_state = 'breached')
			  ) AS k(kind, hit)
			 WHERE k.hit
			   AND i.deleted_at IS NULL
			   AND i.archived_at IS NULL
			   AND i.status NOT IN ('resolved', 'closed')
		)
		INSERT INTO issue_sla_events (issue_id, kind)
		SELECT issue_id, kind FROM due
		ON CONFLICT (issue_id, kind) DO NOTHING
		RETURNING issue_id, kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []service.SLAEscalation
	for rows.Next() {
		var e service.SLAEscalation
		if err := rows.Scan(&e.IssueID, &e.Kind); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if len(out) > 0 && enqueue != nil {
		if err := enqueue(tx, out); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
