package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
)

// ── observability alert ingest ──

// ensureObsBot upserts the system user that observability-created issues are
// attributed to (reporter/actor columns require a real users row).
func ensureObsBot(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO users (identity_sub, email, display_name, role)
		VALUES ('obs|system', 'observability@system.local', 'Observability', 'bot')
		ON CONFLICT (identity_sub) DO UPDATE SET last_seen_at = now()
		RETURNING id`).Scan(&id)
	return id, err
}

// IngestObsAlert creates an issue for a new alert fingerprint, or bumps the
// existing live issue: occurrence counter incremented, and resolved/closed
// issues reopened (an alert firing again after resolution is a regression).
func (s *Store) IngestObsAlert(ctx context.Context, in service.ObsAlertInput, publish service.ObsPublishFn) (domain.Issue, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Issue{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	botID, err := ensureObsBot(ctx, tx)
	if err != nil {
		return domain.Issue{}, false, err
	}

	// Existing live issue for this fingerprint? Lock it to serialize bumps.
	var issueID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `
		SELECT i.id, i.status FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE p.key = $1 AND i.dedupe_key = $2 AND i.deleted_at IS NULL
		FOR UPDATE OF i`, in.ProjectKey, in.Fingerprint).Scan(&issueID, &status)

	switch {
	case err == nil: // bump
		reopened := status == "resolved" || status == "closed"
		if _, err := tx.Exec(ctx, `
			UPDATE issues SET
			  fields = jsonb_set(fields, '{occurrences}',
			           to_jsonb(COALESCE((fields->>'occurrences')::int, 1) + 1), true),
			  status = CASE WHEN status IN ('resolved','closed') THEN 'reopened'::issue_status ELSE status END,
			  updated_at = now()
			WHERE id = $1`, issueID); err != nil {
			return domain.Issue{}, false, err
		}
		verb := "alert.repeated"
		eventType := "issue.updated"
		if reopened {
			verb = "alert.regressed"
			eventType = "issue.reopened"
		}
		if err := recordActivity(ctx, tx, issueID, botID, verb, "issue", issueID); err != nil {
			return domain.Issue{}, false, err
		}
		if publish != nil {
			if err := publish(tx, domain.Issue{ID: issueID}, eventType); err != nil {
				return domain.Issue{}, false, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Issue{}, false, err
		}
		issue, err := s.GetIssueByID(ctx, issueID)
		return issue, false, err

	case errors.Is(err, pgx.ErrNoRows): // create
		var projectID uuid.UUID
		var number int32
		if err := tx.QueryRow(ctx, `
			UPDATE projects SET next_issue_number = next_issue_number + 1, updated_at = now()
			WHERE key = $1 RETURNING id, next_issue_number - 1`, in.ProjectKey).
			Scan(&projectID, &number); err != nil {
			return domain.Issue{}, false, fmt.Errorf("allocate number: %w", err)
		}

		title := strings.TrimSpace(in.Title)
		if title == "" {
			title = "[" + in.Source + "] " + in.Rule
		}
		desc := in.DetailsMD
		if in.Rule != "" {
			desc = "Alert rule: `" + in.Rule + "`\n\n" + desc
		}
		if in.StackTrace != "" {
			desc += "\n\n```\n" + in.StackTrace + "\n```"
		}
		newID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO issues (id, project_id, number, type, title, description_md, status,
			                    severity, priority, reporter_id, source, dedupe_key, fields)
			VALUES ($1, $2, $3, 'bug', $4, $5, 'open', $6::severity,
			        CASE WHEN $6::text IN ('critical','high') THEN 'p1'::priority ELSE 'p2'::priority END,
			        $7, $8::issue_source, $9, '{"occurrences": 1}')`,
			newID, projectID, number, title, desc, sevPtr(in.Severity), botID, in.Source, in.Fingerprint); err != nil {
			return domain.Issue{}, false, fmt.Errorf("insert issue: %w", err)
		}
		if err := recordActivity(ctx, tx, newID, botID, "issue.created", "issue", newID); err != nil {
			return domain.Issue{}, false, err
		}
		if publish != nil {
			if err := publish(tx, domain.Issue{ID: newID}, "issue.created"); err != nil {
				return domain.Issue{}, false, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Issue{}, false, err
		}
		issue, err := s.GetIssueByID(ctx, newID)
		return issue, true, err

	default:
		return domain.Issue{}, false, err
	}
}
