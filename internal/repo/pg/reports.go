package pg

import (
	"context"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// Reports answers the questions the dashboard cannot: it is entirely point-in-time, so
// "are we keeping up", "is the backlog getting older" and "how long do people wait" have
// no answer there. All of this is SQL over created_at / resolved_at plus the comment
// timeline — no new storage, so it works on history already recorded.
//
// Every query takes the same (project, since, until) scope; an empty project key means
// every project, matching the list endpoints.
func (s *Store) Report(ctx context.Context, f service.ReportFilter) (domain.Report, error) {
	var rep domain.Report
	scope := `($1 = '' OR p.key = $1)`
	args := []any{f.ProjectKey, f.Since, f.Until}

	// Created vs resolved per week. generate_series supplies the weeks so a quiet week
	// is a zero rather than a gap — a line chart that skips empty periods lies about
	// the shape of the trend.
	rows, err := s.pool.Query(ctx, `
		WITH weeks AS (
		  SELECT generate_series(date_trunc('week', $2::timestamptz),
		                         date_trunc('week', $3::timestamptz), interval '1 week') AS week
		)
		SELECT w.week,
		       (SELECT count(*) FROM issues i JOIN projects p ON p.id = i.project_id
		         WHERE `+scope+` AND i.deleted_at IS NULL
		           AND date_trunc('week', i.created_at) = w.week),
		       (SELECT count(*) FROM issues i JOIN projects p ON p.id = i.project_id
		         WHERE `+scope+` AND i.deleted_at IS NULL AND i.resolved_at IS NOT NULL
		           AND date_trunc('week', i.resolved_at) = w.week)
		  FROM weeks w ORDER BY w.week`, args...)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var p domain.ReportPoint
		if err := rows.Scan(&p.Period, &p.Created, &p.Resolved); err != nil {
			rows.Close()
			return rep, err
		}
		rep.Flow = append(rep.Flow, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	// Age of the open backlog, bucketed, split by severity. "Old and critical" is the
	// combination worth seeing, and either dimension alone hides it.
	rows, err = s.pool.Query(ctx, `
		SELECT CASE
		         WHEN i.created_at > now() - interval '7 days'  THEN '0-7d'
		         WHEN i.created_at > now() - interval '30 days' THEN '7-30d'
		         WHEN i.created_at > now() - interval '90 days' THEN '30-90d'
		         ELSE '90d+' END AS bucket,
		       COALESCE(i.severity::text, 'none'), count(*)
		  FROM issues i JOIN projects p ON p.id = i.project_id
		 WHERE `+scope+` AND i.deleted_at IS NULL AND i.archived_at IS NULL
		   AND i.status NOT IN ('resolved','closed')
		 GROUP BY 1, 2 ORDER BY 1, 2`, f.ProjectKey)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var a domain.ReportAge
		if err := rows.Scan(&a.Bucket, &a.Severity, &a.Count); err != nil {
			rows.Close()
			return rep, err
		}
		rep.Age = append(rep.Age, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	// Percentiles, not means. A mean hides the tail, and the tail is what people
	// complain about. First response is the first comment by somebody other than the
	// reporter — a reporter adding detail to their own issue is not a response.
	err = s.pool.QueryRow(ctx, `
		WITH resolved AS (
		  SELECT extract(epoch FROM (i.resolved_at - i.created_at)) / 3600 AS hours
		    FROM issues i JOIN projects p ON p.id = i.project_id
		   WHERE `+scope+` AND i.deleted_at IS NULL AND i.resolved_at IS NOT NULL
		     AND i.resolved_at BETWEEN $2 AND $3
		), responses AS (
		  SELECT extract(epoch FROM (min(c.created_at) - i.created_at)) / 3600 AS hours
		    FROM issues i
		    JOIN projects p ON p.id = i.project_id
		    JOIN comments c ON c.issue_id = i.id
		         AND c.deleted_at IS NULL
		         AND c.author_id IS DISTINCT FROM i.reporter_id
		   WHERE `+scope+` AND i.deleted_at IS NULL AND i.created_at BETWEEN $2 AND $3
		   GROUP BY i.id, i.created_at
		)
		SELECT COALESCE((SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY hours) FROM resolved), 0),
		       COALESCE((SELECT percentile_cont(0.9) WITHIN GROUP (ORDER BY hours) FROM resolved), 0),
		       COALESCE((SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY hours) FROM responses), 0),
		       COALESCE((SELECT percentile_cont(0.9) WITHIN GROUP (ORDER BY hours) FROM responses), 0),
		       (SELECT count(*) FROM resolved), (SELECT count(*) FROM responses)`, args...).
		Scan(&rep.ResolveP50, &rep.ResolveP90, &rep.RespondP50, &rep.RespondP90,
			&rep.ResolvedCount, &rep.RespondedCount)
	if err != nil {
		return rep, err
	}

	// Throughput: who closed what over the range.
	rows, err = s.pool.Query(ctx, `
		SELECT COALESCE(u.display_name, 'Unassigned'), count(*)
		  FROM issues i
		  JOIN projects p ON p.id = i.project_id
		  LEFT JOIN users u ON u.id = i.assignee_id
		 WHERE `+scope+` AND i.deleted_at IS NULL AND i.resolved_at BETWEEN $2 AND $3
		 GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, args...)
	if err != nil {
		return rep, err
	}
	defer rows.Close()
	for rows.Next() {
		var t domain.ReportThroughput
		if err := rows.Scan(&t.Name, &t.Count); err != nil {
			return rep, err
		}
		rep.Throughput = append(rep.Throughput, t)
	}
	return rep, rows.Err()
}
