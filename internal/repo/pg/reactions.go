package pg

import (
	"context"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
)

// ToggleReaction adds a reaction or removes it if the same person already left it.
// Returns whether the reaction is now present.
//
// Delete-then-insert rather than an upsert: the natural gesture is a toggle, and a
// second click has to mean "take it back". The unique index makes both halves exact.
func (s *Store) ToggleReaction(
	ctx context.Context, issueID uuid.UUID, commentID *uuid.UUID, userID uuid.UUID, emoji string,
) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM reactions
		  WHERE issue_id = $1 AND comment_id IS NOT DISTINCT FROM $2
		    AND user_id = $3 AND emoji = $4`, issueID, commentID, userID, emoji)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO reactions (issue_id, comment_id, user_id, emoji) VALUES ($1,$2,$3,$4)`,
		issueID, commentID, userID, emoji)
	return err == nil, err
}

// ListReactions returns every reaction on an issue and its comments in one query,
// grouped by target and emoji, with the reactors named — the hover tooltip needs them
// and fetching them separately would be a request per group.
//
// viewerID is folded into the query rather than compared client-side so the "you
// reacted" state arrives with the counts and cannot disagree with them.
func (s *Store) ListReactions(ctx context.Context, issueID, viewerID uuid.UUID) ([]domain.Reaction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT r.comment_id, r.emoji, count(*), array_agg(u.display_name ORDER BY u.display_name),
		        bool_or(r.user_id = $2)
		   FROM reactions r JOIN users u ON u.id = r.user_id
		  WHERE r.issue_id = $1
		  GROUP BY r.comment_id, r.emoji
		  ORDER BY r.comment_id NULLS FIRST, min(r.created_at)`,
		issueID, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Reaction
	for rows.Next() {
		var r domain.Reaction
		if err := rows.Scan(&r.CommentID, &r.Emoji, &r.Count, &r.Users, &r.Mine); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
