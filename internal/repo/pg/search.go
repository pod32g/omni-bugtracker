package pg

import (
	"context"

	"github.com/omni/bugtracker/internal/domain"
)

// Search runs global full-text search across issues and comments using the
// generated fts columns (GIN-indexed). Snippets come from ts_headline with
// «…» match marks (not HTML — the source text is user content, so the client
// renders the snippet as plain text and styles the marked ranges itself).
func (s *Store) Search(ctx context.Context, query string, limit int32) ([]domain.SearchHit, error) {
	const q = `
		WITH tsq AS (SELECT websearch_to_tsquery('english', $1) AS query)
		(SELECT p.key || '-' || i.number AS issue_key, p.key AS project_key, i.title,
		        i.status::text, i.type::text,
		        ts_headline('english', i.title || ' — ' || left(i.description_md, 600), tsq.query,
		                    'MaxFragments=2, MaxWords=16, MinWords=4, StartSel=«, StopSel=»') AS snippet,
		        ts_rank(i.fts, tsq.query) AS rank, 'issue' AS matched_in
		   FROM issues i JOIN projects p ON p.id = i.project_id, tsq
		  WHERE i.fts @@ tsq.query AND i.deleted_at IS NULL)
		UNION ALL
		(SELECT p.key || '-' || i.number, p.key, i.title, i.status::text, i.type::text,
		        ts_headline('english', left(c.body_md, 600), tsq.query,
		                    'MaxFragments=2, MaxWords=16, MinWords=4, StartSel=«, StopSel=»'),
		        ts_rank(c.fts, tsq.query), 'comment'
		   FROM comments c
		   JOIN issues i ON i.id = c.issue_id
		   JOIN projects p ON p.id = i.project_id, tsq
		  WHERE c.fts @@ tsq.query AND c.deleted_at IS NULL AND i.deleted_at IS NULL)
		ORDER BY rank DESC
		LIMIT $2`
	rows, err := s.pool.Query(ctx, q, query, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SearchHit
	for rows.Next() {
		var h domain.SearchHit
		if err := rows.Scan(&h.IssueKey, &h.ProjectKey, &h.Title, &h.Status, &h.Type,
			&h.Snippet, &h.Rank, &h.MatchedIn); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
