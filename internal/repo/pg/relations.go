package pg

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
)

// ── issue relations ──

// ListRelations returns both directions of the relation graph for an issue:
// rows where it is the from-side (direction "out") and the to-side ("in").
// The stored kind is always reported; the client renders the inverse label
// for incoming edges.
func (s *Store) ListRelations(ctx context.Context, issueID uuid.UUID) ([]domain.IssueRelation, error) {
	const q = `
		SELECT r.id, r.kind::text, 'out' AS direction, p.key || '-' || i.number, i.title, i.status
		FROM issue_relations r
		JOIN issues i ON i.id = r.to_issue
		JOIN projects p ON p.id = i.project_id
		WHERE r.from_issue = $1 AND i.deleted_at IS NULL
		UNION ALL
		SELECT r.id, r.kind::text, 'in', p.key || '-' || i.number, i.title, i.status
		FROM issue_relations r
		JOIN issues i ON i.id = r.from_issue
		JOIN projects p ON p.id = i.project_id
		WHERE r.to_issue = $1 AND i.deleted_at IS NULL
		ORDER BY 3, 2, 4`
	rows, err := s.pool.Query(ctx, q, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IssueRelation
	for rows.Next() {
		var rel domain.IssueRelation
		if err := rows.Scan(&rel.ID, &rel.Kind, &rel.Direction, &rel.IssueKey, &rel.Title, &rel.Status); err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

// CreateRelation links two issues and records an activity entry on both sides.
func (s *Store) CreateRelation(ctx context.Context, fromIssue, toIssue uuid.UUID, kind string, actor uuid.UUID) (domain.IssueRelation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.IssueRelation{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO issue_relations (from_issue, to_issue, kind)
		 VALUES ($1, $2, $3::relation_kind) RETURNING id`, fromIssue, toIssue, kind).Scan(&id); err != nil {
		return domain.IssueRelation{}, err
	}
	if err := recordActivity(ctx, tx, fromIssue, actor, "issue.linked", "relation", id); err != nil {
		return domain.IssueRelation{}, err
	}
	if err := recordActivity(ctx, tx, toIssue, actor, "issue.linked", "relation", id); err != nil {
		return domain.IssueRelation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.IssueRelation{}, err
	}

	const q = `
		SELECT r.id, r.kind::text, 'out', p.key || '-' || i.number, i.title, i.status
		FROM issue_relations r
		JOIN issues i ON i.id = r.to_issue
		JOIN projects p ON p.id = i.project_id
		WHERE r.id = $1`
	var rel domain.IssueRelation
	err = s.pool.QueryRow(ctx, q, id).
		Scan(&rel.ID, &rel.Kind, &rel.Direction, &rel.IssueKey, &rel.Title, &rel.Status)
	return rel, err
}

// GetRelationProjectKeys resolves both sides' project keys for permission checks.
func (s *Store) GetRelationProjectKeys(ctx context.Context, id uuid.UUID) (string, string, error) {
	const q = `
		SELECT pf.key, pt.key
		FROM issue_relations r
		JOIN issues f ON f.id = r.from_issue
		JOIN projects pf ON pf.id = f.project_id
		JOIN issues t ON t.id = r.to_issue
		JOIN projects pt ON pt.id = t.project_id
		WHERE r.id = $1`
	var fromKey, toKey string
	err := s.pool.QueryRow(ctx, q, id).Scan(&fromKey, &toKey)
	return fromKey, toKey, err
}

// DeleteRelation removes the link and records activity on both sides.
func (s *Store) DeleteRelation(ctx context.Context, id, actor uuid.UUID) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var fromIssue, toIssue uuid.UUID
	err = tx.QueryRow(ctx,
		`DELETE FROM issue_relations WHERE id = $1 RETURNING from_issue, to_issue`, id).
		Scan(&fromIssue, &toIssue)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := recordActivity(ctx, tx, fromIssue, actor, "issue.unlinked", "relation", id); err != nil {
		return false, err
	}
	if err := recordActivity(ctx, tx, toIssue, actor, "issue.unlinked", "relation", id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
