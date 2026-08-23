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
	// Two phases: rank first, then render snippets for the survivors.
	//
	// ts_headline is expensive — it re-parses the document and finds the best
	// fragments — and it used to sit in the target list of both branches, so it ran
	// once per *match* and then LIMIT threw nearly all of it away. A query for a
	// common word matches most of the corpus, so the work scaled with how ordinary
	// the search term was rather than with how many results were wanted.
	//
	// Postgres will defer a projection past a LIMIT on its own when it can, which is
	// why a single SELECT of this shape is already fast — but it cannot do that
	// through the UNION ALL, and the UNION ALL is the whole point of searching issues
	// and comments together. Hence doing it by hand.
	//
	// Measured on 20k issues / 40k comments: a selective query 2.4ms -> 0.7ms, a query
	// matching the whole corpus 334ms -> 55ms. Result sets are byte-identical.
	const q = `
		WITH tsq AS (SELECT websearch_to_tsquery('english', $1) AS query),
		hits AS (
		    (SELECT i.id AS issue_id, NULL::UUID AS comment_id,
		            ts_rank(i.fts, tsq.query) AS rank, 'issue' AS matched_in
		       FROM issues i, tsq
		      WHERE i.fts @@ tsq.query AND i.deleted_at IS NULL AND i.archived_at IS NULL)
		    UNION ALL
		    (SELECT c.issue_id, c.id, ts_rank(c.fts, tsq.query), 'comment'
		       FROM comments c JOIN issues i ON i.id = c.issue_id, tsq
		      WHERE c.fts @@ tsq.query AND c.deleted_at IS NULL
		        AND i.deleted_at IS NULL AND i.archived_at IS NULL)
		    ORDER BY rank DESC
		    LIMIT $2
		)
		SELECT p.key || '-' || i.number AS issue_key, p.key AS project_key, i.title,
		       i.status::text, i.type::text,
		       ts_headline('english',
		                   CASE WHEN h.matched_in = 'comment' THEN left(c.body_md, 600)
		                        ELSE i.title || ' — ' || left(i.description_md, 600) END,
		                   tsq.query,
		                   'MaxFragments=2, MaxWords=16, MinWords=4, StartSel=«, StopSel=»') AS snippet,
		       h.rank, h.matched_in
		  FROM hits h
		  JOIN issues i ON i.id = h.issue_id
		  JOIN projects p ON p.id = i.project_id
		  LEFT JOIN comments c ON c.id = h.comment_id, tsq
		 ORDER BY h.rank DESC`
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
