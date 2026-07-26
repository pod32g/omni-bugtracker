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
		  WHERE i.fts @@ tsq.query AND i.deleted_at IS NULL AND i.archived_at IS NULL)
		UNION ALL
		(SELECT p.key || '-' || i.number, p.key, i.title, i.status::text, i.type::text,
		        ts_headline('english', left(c.body_md, 600), tsq.query,
		                    'MaxFragments=2, MaxWords=16, MinWords=4, StartSel=«, StopSel=»'),
		        ts_rank(c.fts, tsq.query), 'comment'
		   FROM comments c
		   JOIN issues i ON i.id = c.issue_id
		   JOIN projects p ON p.id = i.project_id, tsq
		  WHERE c.fts @@ tsq.query AND c.deleted_at IS NULL AND i.deleted_at IS NULL AND i.archived_at IS NULL)
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

// FindSimilarIssues ranks live issues in a project against a full-text query built from
// a draft title. Scoped to the project because a duplicate in another project is not a
// duplicate; archived and deleted issues are excluded because filing against them is
// not the mistake being prevented.
//
// excludeKey drops one issue from the results — the issue being edited, which would
// otherwise always rank first against its own title.
func (s *Store) FindSimilarIssues(
	ctx context.Context, projectKey, query, excludeKey string, limit int32,
) ([]domain.SimilarIssue, error) {
	const q = `
		WITH tsq AS (SELECT websearch_to_tsquery('english', $2) AS query)
		SELECT p.key || '-' || i.number AS issue_key, i.title, i.status::text, i.type::text,
		       ts_rank(i.fts, tsq.query) AS score, i.created_at
		  FROM issues i JOIN projects p ON p.id = i.project_id, tsq
		 WHERE p.key = $1
		   AND i.fts @@ tsq.query
		   AND i.deleted_at IS NULL AND i.archived_at IS NULL
		   AND ($3 = '' OR p.key || '-' || i.number <> $3)
		 ORDER BY score DESC, i.created_at DESC
		 LIMIT $4`
	rows, err := s.pool.Query(ctx, q, projectKey, query, excludeKey, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SimilarIssue
	for rows.Next() {
		var si domain.SimilarIssue
		if err := rows.Scan(&si.IssueKey, &si.Title, &si.Status, &si.Type,
			&si.Score, &si.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}
