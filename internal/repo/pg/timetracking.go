package pg

import (
	"context"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

const selectTimeEntry = `
	SELECT t.id, t.issue_id, t.user_id, u.display_name, u.email,
	       t.minutes, to_char(t.spent_on, 'YYYY-MM-DD'), t.note, t.created_at
	  FROM time_entries t LEFT JOIN users u ON u.id = t.user_id`

func scanTimeEntry(row scanner) (domain.TimeEntry, error) {
	var e domain.TimeEntry
	var userID *uuid.UUID
	var name, email *string
	if err := row.Scan(&e.ID, &e.IssueID, &userID, &name, &email,
		&e.Minutes, &e.SpentOn, &e.Note, &e.CreatedAt); err != nil {
		return domain.TimeEntry{}, err
	}
	if userID != nil {
		e.User = &domain.User{ID: *userID, DisplayName: deref(name), Email: deref(email)}
	}
	return e, nil
}

func (s *Store) ListTimeEntries(ctx context.Context, issueID uuid.UUID) ([]domain.TimeEntry, error) {
	rows, err := s.pool.Query(ctx, selectTimeEntry+
		` WHERE t.issue_id = $1 ORDER BY t.spent_on DESC, t.created_at DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TimeEntry
	for rows.Next() {
		e, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LogTime records one entry and an activity line for it, in one transaction. Logging
// time is a fact about the issue that belongs in its timeline — otherwise the number
// on the rail changes and nothing says who moved it or when.
func (s *Store) LogTime(ctx context.Context, in service.TimeEntryInput) (domain.TimeEntry, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.TimeEntry{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO time_entries (issue_id, user_id, minutes, spent_on, note)
		 SELECT $1, $2, $3, COALESCE($4::date, CURRENT_DATE), $5
		   FROM issues WHERE id = $1 AND deleted_at IS NULL
		 RETURNING id`,
		in.IssueID, in.UserID, in.Minutes, nullIfEmpty(in.SpentOn), in.Note).Scan(&id); err != nil {
		return domain.TimeEntry{}, err
	}
	if err := recordActivity(ctx, tx, in.IssueID, in.UserID, "time.logged", "time_entry", id); err != nil {
		return domain.TimeEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.TimeEntry{}, err
	}
	return scanTimeEntry(s.pool.QueryRow(ctx, selectTimeEntry+` WHERE t.id = $1`, id))
}

// DeleteTimeEntry removes an entry the caller owns. Ownership is enforced in the
// statement rather than in a prior read, so a concurrent change cannot open a window
// where somebody deletes a row that stopped being theirs.
func (s *Store) DeleteTimeEntry(ctx context.Context, id, userID uuid.UUID, force bool) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM time_entries WHERE id = $1 AND ($3 OR user_id = $2)`, id, userID, force)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// nullIfEmpty lets an unset date fall through to the column default rather than
// failing to parse "" as a date.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// effortSelect is the rollup expression, written once. Every caller aggregates the
// same way, so a milestone and a release can never disagree about what "spent" means.
//
// `estimated` counts the issues that carry an estimate, not the issues in the set:
// "40 issues, 2 estimated" and "40 issues, 40 estimated" produce very different plans
// and would otherwise look identical.
const effortSelect = `
	count(*)::INT,
	count(*) FILTER (WHERE i.estimate_minutes IS NOT NULL)::INT,
	COALESCE(sum(i.estimate_minutes), 0)::INT,
	COALESCE(sum(tm.spent_minutes), 0)::INT,
	COALESCE(sum(i.estimate_minutes) FILTER (WHERE i.status NOT IN ('resolved','closed')), 0)::INT`

func scanEffort(row scanner) (domain.EffortRollup, error) {
	var e domain.EffortRollup
	err := row.Scan(&e.Issues, &e.Estimated, &e.EstimateMinutes, &e.SpentMinutes, &e.RemainingMinutes)
	return e, err
}

// EffortForIssues rolls up estimate-vs-spent over whatever the filter selects, so the
// same numbers are available for any queue the list can express.
func (s *Store) EffortForIssues(ctx context.Context, f service.IssueFilter) (domain.EffortRollup, error) {
	clause, args := issueWhere(f)
	return scanEffort(s.pool.QueryRow(ctx, `
		SELECT `+effortSelect+`
		  FROM issues i
		  JOIN projects p ON p.id = i.project_id
		  LEFT JOIN issue_time tm ON tm.issue_id = i.id
		 WHERE `+clause, args...))
}

// effortByOwner loads rollups keyed by a grouping column, for the list endpoints that
// return many milestones/releases/components at once. One query per list, not per row.
func (s *Store) effortByOwner(ctx context.Context, query string, args ...any) (map[uuid.UUID]domain.EffortRollup, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[uuid.UUID]domain.EffortRollup{}
	for rows.Next() {
		var id uuid.UUID
		var e domain.EffortRollup
		if err := rows.Scan(&id, &e.Issues, &e.Estimated, &e.EstimateMinutes,
			&e.SpentMinutes, &e.RemainingMinutes); err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, rows.Err()
}

// MilestoneEffort / ReleaseEffort / ComponentEffort roll up one project's groupings.
func (s *Store) MilestoneEffort(ctx context.Context, projectKey string) (map[uuid.UUID]domain.EffortRollup, error) {
	return s.effortByOwner(ctx, `
		SELECT i.milestone_id, `+effortSelect+`
		  FROM issues i
		  JOIN projects p ON p.id = i.project_id
		  LEFT JOIN issue_time tm ON tm.issue_id = i.id
		 WHERE p.key = $1 AND i.deleted_at IS NULL AND i.milestone_id IS NOT NULL
		 GROUP BY i.milestone_id`, projectKey)
}

func (s *Store) ReleaseEffort(ctx context.Context, projectKey string) (map[uuid.UUID]domain.EffortRollup, error) {
	return s.effortByOwner(ctx, `
		SELECT i.release_id, `+effortSelect+`
		  FROM issues i
		  JOIN projects p ON p.id = i.project_id
		  LEFT JOIN issue_time tm ON tm.issue_id = i.id
		 WHERE p.key = $1 AND i.deleted_at IS NULL AND i.release_id IS NOT NULL
		 GROUP BY i.release_id`, projectKey)
}

func (s *Store) ComponentEffort(ctx context.Context, projectKey string) (map[uuid.UUID]domain.EffortRollup, error) {
	return s.effortByOwner(ctx, `
		SELECT ic.component_id, `+effortSelect+`
		  FROM issue_components ic
		  JOIN issues i ON i.id = ic.issue_id
		  JOIN projects p ON p.id = i.project_id
		  LEFT JOIN issue_time tm ON tm.issue_id = i.id
		 WHERE p.key = $1 AND i.deleted_at IS NULL
		 GROUP BY ic.component_id`, projectKey)
}

// RemainingByAssignee is the dashboard's "who is carrying what", in minutes of
// estimated open work rather than a headcount of issues.
func (s *Store) RemainingByAssignee(ctx context.Context, projectKey string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(u.display_name, u.email), COALESCE(sum(i.estimate_minutes), 0)::INT
		  FROM issues i
		  JOIN projects p ON p.id = i.project_id
		  JOIN users u ON u.id = i.assignee_id
		 WHERE ($1 = '' OR p.key = $1)
		   AND i.deleted_at IS NULL AND i.archived_at IS NULL
		   AND i.status NOT IN ('resolved','closed')
		 GROUP BY 1 HAVING sum(i.estimate_minutes) > 0`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var name string
		var minutes int
		if err := rows.Scan(&name, &minutes); err != nil {
			return nil, err
		}
		out[name] = minutes
	}
	return out, rows.Err()
}
