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

// selectIteration carries the scope rollup with the iteration itself. The counts are
// what every caller wants next, and a second round trip per row to get them is the
// kind of thing that turns a planning page into forty queries.
const selectIteration = `
	SELECT it.id, p.key, it.name, to_char(it.starts_on, 'YYYY-MM-DD'),
	       to_char(it.ends_on, 'YYYY-MM-DD'), it.state::text, it.goal, it.created_at,
	       COALESCE(agg.issues, 0), COALESCE(agg.done_issues, 0),
	       COALESCE(agg.estimated, 0), COALESCE(agg.estimate_minutes, 0),
	       COALESCE(agg.spent_minutes, 0), COALESCE(agg.remaining_minutes, 0),
	       COALESCE(agg.done_minutes, 0)
	  FROM iterations it
	  JOIN projects p ON p.id = it.project_id
	  LEFT JOIN LATERAL (
	      SELECT count(*)::INT AS issues,
	             count(*) FILTER (WHERE i.status IN ('resolved','closed'))::INT AS done_issues,
	             count(*) FILTER (WHERE i.estimate_minutes IS NOT NULL)::INT AS estimated,
	             COALESCE(sum(i.estimate_minutes), 0)::INT AS estimate_minutes,
	             COALESCE(sum(tm.spent_minutes), 0)::INT AS spent_minutes,
	             COALESCE(sum(i.estimate_minutes)
	                      FILTER (WHERE i.status NOT IN ('resolved','closed')), 0)::INT AS remaining_minutes,
	             COALESCE(sum(i.estimate_minutes)
	                      FILTER (WHERE i.status IN ('resolved','closed')), 0)::INT AS done_minutes
	        FROM issues i
	        LEFT JOIN issue_time tm ON tm.issue_id = i.id
	       WHERE i.iteration_id = it.id AND i.deleted_at IS NULL
	  ) agg ON TRUE`

func scanIteration(row scanner) (domain.Iteration, error) {
	var it domain.Iteration
	err := row.Scan(&it.ID, &it.ProjectKey, &it.Name, &it.StartsOn, &it.EndsOn,
		&it.State, &it.Goal, &it.CreatedAt,
		&it.Effort.Issues, &it.DoneIssues, &it.Effort.Estimated, &it.Effort.EstimateMinutes,
		&it.Effort.SpentMinutes, &it.Effort.RemainingMinutes, &it.DoneMinutes)
	it.Issues = it.Effort.Issues
	return it, err
}

func (s *Store) ListIterations(ctx context.Context, projectKey string) ([]domain.Iteration, error) {
	rows, err := s.pool.Query(ctx, selectIteration+
		` WHERE p.key = $1 ORDER BY it.starts_on DESC`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Iteration
	for rows.Next() {
		it, err := scanIteration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) GetIteration(ctx context.Context, id uuid.UUID) (domain.Iteration, error) {
	return scanIteration(s.pool.QueryRow(ctx, selectIteration+` WHERE it.id = $1`, id))
}

func (s *Store) CreateIteration(ctx context.Context, in service.IterationInput) (domain.Iteration, error) {
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO iterations (project_id, name, starts_on, ends_on, goal)
		 SELECT p.id, $2, $3::date, $4::date, $5 FROM projects p WHERE p.key = $1
		 RETURNING id`,
		in.ProjectKey, in.Name, in.StartsOn, in.EndsOn, in.Goal).Scan(&id); err != nil {
		return domain.Iteration{}, err
	}
	return s.GetIteration(ctx, id)
}

// UpdateIteration applies a partial edit. Activating one is not a plain field write:
// a project may only have one active iteration, so the previous holder is stood down
// in the same transaction rather than left to collide with the unique index.
func (s *Store) UpdateIteration(ctx context.Context, id uuid.UUID, in service.UpdateIterationInput) (domain.Iteration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Iteration{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if in.State != nil && *in.State == domain.IterationActive {
		if _, err := tx.Exec(ctx,
			`UPDATE iterations SET state = 'completed'
			  WHERE state = 'active' AND id <> $1
			    AND project_id = (SELECT project_id FROM iterations WHERE id = $1)`, id); err != nil {
			return domain.Iteration{}, err
		}
		// Snapshot the commitment as the sprint starts: what the team signed up for,
		// before anything is finished and before carry-over removes the evidence.
		//
		// Only on the first activation — `committed_at IS NULL` — because re-activating
		// a sprint that was closed early must not quietly rewrite what was promised
		// into what was left.
		if _, err := tx.Exec(ctx, `
			UPDATE iterations it SET
			  committed_issues  = c.n,
			  committed_minutes = c.minutes,
			  committed_at      = now()
			 FROM (SELECT count(*)::INT AS n,
			              COALESCE(sum(estimate_minutes), 0)::INT AS minutes
			         FROM issues WHERE iteration_id = $1 AND deleted_at IS NULL) c
			WHERE it.id = $1 AND it.committed_at IS NULL`, id); err != nil {
			return domain.Iteration{}, fmt.Errorf("snapshot commitment: %w", err)
		}
	}
	tag, err := tx.Exec(ctx,
		`UPDATE iterations SET
		   name      = COALESCE($2, name),
		   starts_on = COALESCE($3::date, starts_on),
		   ends_on   = COALESCE($4::date, ends_on),
		   goal      = COALESCE($5, goal),
		   state     = COALESCE($6::iteration_state, state)
		 WHERE id = $1`, id, in.Name, in.StartsOn, in.EndsOn, in.Goal, in.State)
	if err != nil {
		return domain.Iteration{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Iteration{}, pgx.ErrNoRows
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Iteration{}, err
	}
	return s.GetIteration(ctx, id)
}

func (s *Store) DeleteIteration(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM iterations WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// SetIssueIteration moves an issue into an iteration, or out of one when iteration is
// nil. The target must belong to the issue's own project — an issue in an iteration
// from somewhere else would break every rollup that assumes the two agree.
func (s *Store) SetIssueIteration(ctx context.Context, issueID uuid.UUID, iteration *uuid.UUID, actor uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if iteration != nil {
		var ok bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM iterations it JOIN issues i ON i.project_id = it.project_id
			  WHERE it.id = $1 AND i.id = $2)`, *iteration, issueID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("iteration does not belong to the issue's project")
		}
	}
	tag, err := tx.Exec(ctx,
		`UPDATE issues SET iteration_id = $2, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL`, issueID, iteration)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	verb := "issue.iteration_removed"
	if iteration != nil {
		verb = "issue.iteration_set"
	}
	if err := recordActivity(ctx, tx, issueID, actor, verb, "issue", issueID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CarryOverIssues moves every unfinished issue out of one iteration and into another
// (or to no iteration when target is nil), returning how many moved.
//
// Carry-over is an explicit action, never a side effect of closing an iteration:
// silently sweeping unfinished work forward is how a team stops noticing that it
// happens every time.
func (s *Store) CarryOverIssues(ctx context.Context, from uuid.UUID, to *uuid.UUID) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE issues SET iteration_id = $2, updated_at = now()
		  WHERE iteration_id = $1 AND deleted_at IS NULL
		    AND status NOT IN ('resolved','closed')`, from, to)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) IterationBurndown(ctx context.Context, id uuid.UUID) ([]domain.BurndownPoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT to_char(on_date, 'YYYY-MM-DD'), remaining_issues, remaining_minutes,
		        total_issues, total_minutes
		   FROM iteration_snapshots WHERE iteration_id = $1 ORDER BY on_date`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.BurndownPoint
	for rows.Next() {
		var p domain.BurndownPoint
		if err := rows.Scan(&p.Date, &p.RemainingIssues, &p.RemainingMinutes,
			&p.TotalIssues, &p.TotalMinutes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SnapshotIterations records today's remaining work for every active iteration.
//
// Upsert on (iteration, date) so re-running the job — after a crash, or twice in one
// day — overwrites rather than duplicating. The snapshot is of *now*, so the last run
// of the day is the one that should win.
func (s *Store) SnapshotIterations(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO iteration_snapshots
		    (iteration_id, on_date, remaining_issues, remaining_minutes, total_issues, total_minutes)
		SELECT it.id, CURRENT_DATE,
		       count(*) FILTER (WHERE i.status NOT IN ('resolved','closed'))::INT,
		       COALESCE(sum(i.estimate_minutes)
		                FILTER (WHERE i.status NOT IN ('resolved','closed')), 0)::INT,
		       count(i.id)::INT,
		       COALESCE(sum(i.estimate_minutes), 0)::INT
		  FROM iterations it
		  LEFT JOIN issues i ON i.iteration_id = it.id AND i.deleted_at IS NULL
		 WHERE it.state = 'active'
		 GROUP BY it.id
		ON CONFLICT (iteration_id, on_date) DO UPDATE SET
		    remaining_issues  = EXCLUDED.remaining_issues,
		    remaining_minutes = EXCLUDED.remaining_minutes,
		    total_issues      = EXCLUDED.total_issues,
		    total_minutes     = EXCLUDED.total_minutes`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// IterationVelocity returns finished iterations newest-first, so the caller can take a
// rolling window off the front.
func (s *Store) IterationVelocity(ctx context.Context, projectKey string, limit int) ([]domain.Velocity, error) {
	// The denominator is the commitment snapshot taken when the sprint was activated,
	// not a count of what still points at the iteration now. Carry-over moves out
	// exactly the unfinished issues, so the live count collapses onto the done count
	// and every sprint that carried anything over reported 100%.
	//
	// GREATEST(..., done) because an issue can be added mid-sprint and finished: the
	// team delivered more than it committed to, and a denominator smaller than the
	// numerator would render as over 100% rather than as the good news it is. Sprints
	// with no snapshot — everything from before the column existed — fall back to the
	// old derivation and say so in `committed`, rather than being handed a baseline
	// nobody recorded.
	rows, err := s.pool.Query(ctx, `
		SELECT it.id, it.name, to_char(it.ends_on, 'YYYY-MM-DD'),
		       count(*) FILTER (WHERE i.status IN ('resolved','closed'))::INT AS done,
		       COALESCE(sum(i.estimate_minutes)
		                FILTER (WHERE i.status IN ('resolved','closed')), 0)::INT AS done_minutes,
		       GREATEST(COALESCE(it.committed_issues, count(i.id)),
		                count(*) FILTER (WHERE i.status IN ('resolved','closed')))::INT,
		       GREATEST(COALESCE(it.committed_minutes, COALESCE(sum(i.estimate_minutes), 0)),
		                COALESCE(sum(i.estimate_minutes)
		                         FILTER (WHERE i.status IN ('resolved','closed')), 0))::INT,
		       it.committed_at IS NOT NULL
		  FROM iterations it
		  JOIN projects p ON p.id = it.project_id
		  LEFT JOIN issues i ON i.iteration_id = it.id AND i.deleted_at IS NULL
		 WHERE p.key = $1 AND it.state = 'completed'
		 GROUP BY it.id, it.name, it.ends_on, it.committed_issues, it.committed_minutes, it.committed_at
		 ORDER BY it.ends_on DESC
		 LIMIT $2`, projectKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Velocity
	for rows.Next() {
		var v domain.Velocity
		if err := rows.Scan(&v.IterationID, &v.Name, &v.EndsOn, &v.DoneIssues,
			&v.DoneMinutes, &v.PlannedIssues, &v.PlannedMinutes, &v.Committed); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ResolveIteration turns an `iteration:` filter value into an id.
//
// "current" is the active iteration, "next" the soonest planned one; anything else is
// matched by name, then as a raw uuid. Returns found=false when nothing matches — the
// filter must then match nothing rather than dropping the constraint, or
// `iteration:current` on a project between iterations would silently list everything.
func (s *Store) ResolveIteration(ctx context.Context, projectKey, ref string) (uuid.UUID, bool, error) {
	var q string
	var args []any
	switch strings.ToLower(ref) {
	case "current", "active":
		q = `SELECT it.id FROM iterations it JOIN projects p ON p.id = it.project_id
		      WHERE p.key = $1 AND it.state = 'active'`
		args = []any{projectKey}
	case "next":
		q = `SELECT it.id FROM iterations it JOIN projects p ON p.id = it.project_id
		      WHERE p.key = $1 AND it.state = 'planned' AND it.ends_on >= CURRENT_DATE
		      ORDER BY it.starts_on LIMIT 1`
		args = []any{projectKey}
	default:
		// Name first: people type the name they see, and a uuid is what the UI's own
		// deep links carry. $2::uuid stays NULL for a non-uuid rather than erroring.
		q = `SELECT it.id FROM iterations it JOIN projects p ON p.id = it.project_id
		      WHERE p.key = $1 AND (lower(it.name) = lower($2) OR it.id = $3::uuid)
		      LIMIT 1`
		var asUUID *uuid.UUID
		if parsed, err := uuid.Parse(ref); err == nil {
			asUUID = &parsed
		}
		args = []any{projectKey, ref, asUUID}
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, q, args...).Scan(&id)
	if err == pgx.ErrNoRows {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
}
