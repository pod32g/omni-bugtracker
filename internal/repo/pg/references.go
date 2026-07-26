package pg

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/prose"
)

// syncReferences recomputes the cross-references carried by one piece of prose.
//
// Scope is (source issue, source comment): a comment's references are replaced when
// that comment is edited, and the issue body's when the body is edited, so editing one
// never drops the other's. Removing a key from the text removes the edge — the
// reference is derived from the prose, not a record of having once written it.
//
// The target gets an `issue.referenced` timeline entry, but only the first time, so
// re-saving a description does not re-announce every key it contains.
func syncReferences(
	ctx context.Context, tx pgx.Tx,
	sourceIssueID uuid.UUID, sourceCommentID *uuid.UUID, sourceKey string,
	actor uuid.UUID, text string,
) error {
	targets, err := resolveKeys(ctx, tx, prose.ParseKeys(text))
	if err != nil {
		return err
	}

	previous, err := existingReferenceTargets(ctx, tx, sourceIssueID, sourceCommentID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM issue_references
		 WHERE source_issue_id = $1 AND source_comment_id IS NOT DISTINCT FROM $2`,
		sourceIssueID, sourceCommentID); err != nil {
		return err
	}

	for _, target := range targets {
		if target == sourceIssueID { // an issue referring to itself is not a link
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO issue_references (source_issue_id, target_issue_id, source_comment_id)
			 VALUES ($1,$2,$3)`, sourceIssueID, target, sourceCommentID); err != nil {
			return err
		}
		if previous[target] || sourceKey == "" {
			continue
		}
		changes, err := json.Marshal(map[string]string{"from": sourceKey})
		if err != nil {
			return err
		}
		if err := recordActivityChanges(ctx, tx, target, actor,
			"issue.referenced", "issue", sourceIssueID, changes); err != nil {
			return err
		}
	}
	return nil
}

// issueProse returns an issue's key and every narrative field concatenated — the text
// a cross-reference can be written in. Used where the caller holds a partial patch and
// so cannot know what the body says after the update.
func issueProse(ctx context.Context, tx pgx.Tx, issueID uuid.UUID) (key, text string, err error) {
	var projectKey string
	var number int32
	err = tx.QueryRow(ctx,
		`SELECT p.key, i.number,
		        concat_ws(E'\n', i.description_md, i.repro_steps_md, i.expected_md,
		                  i.actual_md, i.environment_md)
		   FROM issues i JOIN projects p ON p.id = i.project_id
		  WHERE i.id = $1`, issueID).Scan(&projectKey, &number, &text)
	if err != nil {
		return "", "", err
	}
	return domain.IssueKey(projectKey, number), text, nil
}

// resolveKeys turns parsed keys into issue ids, dropping the ones that name no issue —
// "RFC-2119" in a sentence costs one lookup and links nothing.
func resolveKeys(ctx context.Context, tx pgx.Tx, keys []prose.Key) ([]uuid.UUID, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	projectKeys := make([]string, 0, len(keys))
	numbers := make([]int32, 0, len(keys))
	for _, k := range keys {
		projectKeys = append(projectKeys, k.ProjectKey)
		numbers = append(numbers, k.Number)
	}

	// unnest pairs the two arrays so the join hits the (project_id, number) index
	// instead of scanning every issue.
	rows, err := tx.Query(ctx,
		`SELECT i.id
		   FROM unnest($1::text[], $2::int[]) AS want(key, number)
		   JOIN projects p ON p.key = want.key
		   JOIN issues i ON i.project_id = p.id AND i.number = want.number
		  WHERE i.deleted_at IS NULL`, projectKeys, numbers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func existingReferenceTargets(
	ctx context.Context, tx pgx.Tx, sourceIssueID uuid.UUID, sourceCommentID *uuid.UUID,
) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx,
		`SELECT target_issue_id FROM issue_references
		 WHERE source_issue_id = $1 AND source_comment_id IS NOT DISTINCT FROM $2`,
		sourceIssueID, sourceCommentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ListReferencedBy returns the live issues whose prose mentions this one, newest first.
// One row per referencing issue even when it mentions this one several times.
func (s *Store) ListReferencedBy(ctx context.Context, issueID uuid.UUID) ([]domain.IssueReference, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (src.id)
		        src.id, p.key, src.number, src.title, src.status,
		        r.source_comment_id IS NOT NULL, r.created_at
		   FROM issue_references r
		   JOIN issues src ON src.id = r.source_issue_id
		   JOIN projects p ON p.id = src.project_id
		  WHERE r.target_issue_id = $1 AND src.deleted_at IS NULL
		  ORDER BY src.id, r.created_at DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.IssueReference
	for rows.Next() {
		var ref domain.IssueReference
		var number int32
		var projectKey string
		if err := rows.Scan(&ref.IssueID, &projectKey, &number, &ref.Title,
			&ref.Status, &ref.InComment, &ref.CreatedAt); err != nil {
			return nil, err
		}
		ref.IssueKey = domain.IssueKey(projectKey, number)
		out = append(out, ref)
	}
	return out, rows.Err()
}
