package pg

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/prose"
)

// syncMentions recomputes the @mentions carried by one piece of prose, scoped to
// (issue, comment) exactly as syncReferences is.
//
// A mentioned user is added as a watcher, so the follow-up conversation reaches them
// without anyone having to curate a watch list. Rows already present are left alone:
// notified_at lives on them, and re-inserting would make an edit re-notify.
//
// Self-mentions are recorded but never notify — you know what you just wrote.
func syncMentions(
	ctx context.Context, tx pgx.Tx,
	issueID uuid.UUID, sourceCommentID *uuid.UUID, actor uuid.UUID, text string,
) error {
	handles := prose.ParseHandles(text)
	mentioned, err := resolveHandles(ctx, tx, issueID, handles)
	if err != nil {
		return err
	}

	// Drop mentions the edit removed, keeping the ones it did not, so notified_at
	// survives and an unrelated edit cannot re-announce an old mention.
	keep := make([]uuid.UUID, 0, len(mentioned))
	for _, m := range mentioned {
		keep = append(keep, m.userID)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM issue_mentions
		  WHERE issue_id = $1 AND source_comment_id IS NOT DISTINCT FROM $2
		    AND NOT (user_id = ANY($3::uuid[]))`,
		issueID, sourceCommentID, keep); err != nil {
		return err
	}

	for _, m := range mentioned {
		var inserted bool
		// notified_at is pre-stamped for a self-mention: the row exists for the
		// inbox, but the dispatcher will never pick it up.
		err := tx.QueryRow(ctx,
			`INSERT INTO issue_mentions (issue_id, source_comment_id, user_id, mentioned_by, notified_at)
			 SELECT $1, $2, $3, $4, CASE WHEN $3 = $4 THEN now() END
			  WHERE NOT EXISTS (
			        SELECT 1 FROM issue_mentions
			         WHERE issue_id = $1 AND source_comment_id IS NOT DISTINCT FROM $2 AND user_id = $3)
			 RETURNING TRUE`, issueID, sourceCommentID, m.userID, actor).Scan(&inserted)
		if err == pgx.ErrNoRows {
			continue // already mentioned here; nothing new to announce
		}
		if err != nil {
			return err
		}
		if err := addWatcher(ctx, tx, issueID, m.userID); err != nil {
			return err
		}
		if m.userID == actor {
			continue
		}
		changes, err := json.Marshal(map[string]string{"handle": m.handle})
		if err != nil {
			return err
		}
		if err := recordActivityChanges(ctx, tx, issueID, actor,
			"user.mentioned", "user", m.userID, changes); err != nil {
			return err
		}
	}
	return nil
}

type mention struct {
	userID uuid.UUID
	handle string
}

// resolveHandles maps @handles to users, matching either a display name or an email
// local-part. Only people who can see the issue are mentionable — mentioning someone
// into a project they have no access to would notify them about something they cannot
// open. A handle that names nobody is silently dropped.
func resolveHandles(ctx context.Context, tx pgx.Tx, issueID uuid.UUID, handles []string) ([]mention, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	// Global roles above member already see every project, so membership is only
	// consulted for the rest.
	rows, err := tx.Query(ctx,
		`SELECT u.id, lower(u.display_name), lower(split_part(u.email, '@', 1))
		   FROM users u
		  WHERE (lower(u.display_name) = ANY($1) OR lower(split_part(u.email, '@', 1)) = ANY($1))
		    AND (u.role <> 'member'
		         OR EXISTS (SELECT 1
		                      FROM project_members pm
		                      JOIN issues i ON i.project_id = pm.project_id
		                     WHERE i.id = $2 AND pm.user_id = u.id))`, handles, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHandle := map[string]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var displayName, localPart string
		if err := rows.Scan(&id, &displayName, &localPart); err != nil {
			return nil, err
		}
		byHandle[displayName] = id
		// A display name wins over a local-part collision: it is what the UI shows
		// and therefore what the author meant.
		if _, taken := byHandle[localPart]; !taken {
			byHandle[localPart] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	seen := map[uuid.UUID]bool{}
	out := make([]mention, 0, len(handles))
	for _, h := range handles {
		id, ok := byHandle[h]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, mention{userID: id, handle: h})
	}
	return out, nil
}

// ClaimPendingMentions stamps and returns the email addresses of everyone newly
// mentioned on an issue. Called by the dispatcher, which turns each into a notify job.
// The UPDATE ... RETURNING is the claim: two dispatcher runs cannot both take a row, so
// a mention notifies exactly once even if several events fire for the same issue.
func (s *Store) ClaimPendingMentions(ctx context.Context, issueID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE issue_mentions m
		    SET notified_at = now()
		   FROM users u
		  WHERE m.user_id = u.id AND m.issue_id = $1 AND m.notified_at IS NULL
		 RETURNING u.email`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}
