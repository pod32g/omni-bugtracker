package pg

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests exercise the SQL itself against a real Postgres, because the class of bug
// they exist for is invisible to the compiler and to every non-DB test: Postgres cannot
// infer a type for a bare parameter, so `$3 = $4` resolved uuid against text and every
// comment carrying a resolvable @mention failed with 42883. The same shape (42P08) had
// already been hit three times in enum updates.
//
// Opt-in: set OMNI_BT_TEST_DSN to a migrated database. Skipped otherwise, so `go test
// ./...` stays hermetic.
//
//	OMNI_BT_TEST_DSN="postgres://omni:omni@localhost:15432/omni_bugtracker?sslmode=disable" \
//	  go test ./internal/repo/pg/ -run Integration
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("OMNI_BT_TEST_DSN")
	if dsn == "" {
		t.Skip("set OMNI_BT_TEST_DSN to run the SQL integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestIntegrationSyncMentionsTypes is the regression test for the 42883 above. It runs
// every branch — mentioning someone else, mentioning yourself, and re-running over the
// same text — inside a transaction that is always rolled back.
func TestIntegrationSyncMentionsTypes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // the test never commits

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	var actorID, otherID, projectID, issueID uuid.UUID

	for _, u := range []struct {
		into  *uuid.UUID
		name  string
		email string
	}{
		{&actorID, "tester-" + suffix, "tester-" + suffix + "@test.local"},
		{&otherID, "other-" + suffix, "other-" + suffix + "@test.local"},
	} {
		if err := tx.QueryRow(ctx,
			`INSERT INTO users (identity_sub, email, display_name, role)
			 VALUES ($1, $2, $3, 'owner') RETURNING id`,
			"test:"+u.name, u.email, u.name).Scan(u.into); err != nil {
			t.Fatalf("seed user %s: %v", u.name, err)
		}
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Mention Test') RETURNING id`,
		"T"+strings.ToUpper(suffix[:4])).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
		 VALUES ($1, 1, 'bug', 'mention target', 'open', 'p2', $2) RETURNING id`,
		projectID, actorID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	other := "other-" + suffix
	self := "tester-" + suffix

	// Mentioning somebody else leaves notified_at NULL for the dispatcher to claim.
	if err := syncMentions(ctx, tx, issueID, nil, actorID, "cc @"+other); err != nil {
		t.Fatalf("mention another user: %v", err)
	}
	assertMention(t, ctx, tx, issueID, otherID, false)

	// A self-mention is the branch that stamped notified_at in SQL and blew up.
	if err := syncMentions(ctx, tx, issueID, nil, actorID, "cc @"+other+" and @"+self); err != nil {
		t.Fatalf("self-mention: %v", err)
	}
	assertMention(t, ctx, tx, issueID, actorID, true)

	// Re-running over unchanged text must not duplicate or re-arm anything.
	if err := syncMentions(ctx, tx, issueID, nil, actorID, "cc @"+other+" and @"+self); err != nil {
		t.Fatalf("re-sync: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM issue_mentions WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("re-sync produced %d rows, want 2", count)
	}

	// Dropping a handle from the text drops the mention.
	if err := syncMentions(ctx, tx, issueID, nil, actorID, "cc @"+self); err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM issue_mentions WHERE issue_id = $1 AND user_id = $2`,
		issueID, otherID).Scan(&count); err != nil {
		t.Fatalf("count after shrink: %v", err)
	}
	if count != 0 {
		t.Errorf("removing the handle left %d rows, want 0", count)
	}
}

func assertMention(t *testing.T, ctx context.Context, tx pgx.Tx, issueID, userID uuid.UUID, wantNotified bool) {
	t.Helper()
	var notified bool
	if err := tx.QueryRow(ctx,
		`SELECT notified_at IS NOT NULL FROM issue_mentions WHERE issue_id = $1 AND user_id = $2`,
		issueID, userID).Scan(&notified); err != nil {
		t.Fatalf("read mention: %v", err)
	}
	if notified != wantNotified {
		t.Errorf("notified_at set = %v, want %v (a self-mention is pre-stamped so the "+
			"dispatcher never picks it up)", notified, wantNotified)
	}
}

// TestIntegrationCrossReferenceTypes covers the same risk in syncReferences, whose
// resolveKeys passes text[] and int[] through unnest.
func TestIntegrationCrossReferenceTypes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	key := "R" + strings.ToUpper(suffix[:4])

	var actorID, projectID, source, target uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"test:ref-"+suffix, "ref-"+suffix+"@test.local", "ref-"+suffix).Scan(&actorID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Ref Test') RETURNING id`, key).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	for i, into := range []*uuid.UUID{&source, &target} {
		if err := tx.QueryRow(ctx,
			`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
			 VALUES ($1, $2, 'bug', 'ref test', 'open', 'p2', $3) RETURNING id`,
			projectID, i+1, actorID).Scan(into); err != nil {
			t.Fatalf("seed issue %d: %v", i+1, err)
		}
	}

	if err := syncReferences(ctx, tx, source, nil, key+"-1", actorID,
		"same root cause as "+key+"-2"); err != nil {
		t.Fatalf("sync references: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM issue_references WHERE source_issue_id = $1 AND target_issue_id = $2`,
		source, target).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("got %d reference rows, want 1", count)
	}

	// A key naming no issue costs a lookup and links nothing.
	if err := syncReferences(ctx, tx, source, nil, key+"-1", actorID, "see RFC-2119"); err != nil {
		t.Fatalf("unresolvable key: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM issue_references WHERE source_issue_id = $1`, source).Scan(&count); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if count != 0 {
		t.Errorf("unresolvable key left %d rows, want 0", count)
	}
}

// TestIntegrationSoftDeletedCommentDropsDerivedRows pins the follow-up to the above.
// Comments are soft-deleted, so the ON DELETE CASCADE on source_comment_id never fires:
// deleting a comment used to leave its cross-reference showing on the target, pointing
// at text nobody can read.
func TestIntegrationSoftDeletedCommentDropsDerivedRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := &Store{pool: pool}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	key := "D" + strings.ToUpper(suffix[:4])

	var actorID, projectID, source, target, commentID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"test:del-"+suffix, "del-"+suffix+"@test.local", "del-"+suffix).Scan(&actorID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Delete Test') RETURNING id`, key).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	for i, into := range []*uuid.UUID{&source, &target} {
		if err := tx.QueryRow(ctx,
			`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
			 VALUES ($1, $2, 'bug', 'delete test', 'open', 'p2', $3) RETURNING id`,
			projectID, i+1, actorID).Scan(into); err != nil {
			t.Fatalf("seed issue %d: %v", i+1, err)
		}
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO comments (issue_id, author_id, body_md) VALUES ($1,$2,$3) RETURNING id`,
		source, actorID, "see "+key+"-2 and cc @del-"+suffix).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	if err := syncReferences(ctx, tx, source, &commentID, key+"-1", actorID,
		"see "+key+"-2"); err != nil {
		t.Fatalf("sync references: %v", err)
	}
	if err := syncMentions(ctx, tx, source, &commentID, actorID, "cc @del-"+suffix); err != nil {
		t.Fatalf("sync mentions: %v", err)
	}
	if got := countBy(t, ctx, tx, `SELECT count(*) FROM issue_references WHERE source_comment_id = $1`, commentID); got != 1 {
		t.Fatalf("setup: want 1 reference, got %d", got)
	}

	// Commit so SoftDeleteComment (which opens its own transaction) can see the rows,
	// then clean up by hand — the seed lives outside this test's rollback from here on.
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})

	if _, err := store.SoftDeleteComment(ctx, commentID, actorID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	for _, table := range []string{"issue_references", "issue_mentions"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE source_comment_id = $1`, commentID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d row(s) for a deleted comment — the panel would point at "+
				"text nobody can read", table, n)
		}
	}
}

func countBy(t *testing.T, ctx context.Context, tx pgx.Tx, q string, args ...any) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestIntegrationClaimSLAEscalationsIsExactlyOnce is the guard on the escalation claim.
// The point of it is that a breach is announced once: a second sweep over the same
// still-breached issues must return nothing, or every worker run re-pages the team
// about the same issue for as long as it stays late.
//
// Unlike the tests above this one commits — the claim is an INSERT whose whole job is
// to be visible to the next caller — so it cleans up after itself instead.
func TestIntegrationClaimSLAEscalationsIsExactlyOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := New(pool)

	// Snapshot what was already claimed so the test can restore it, then start clean.
	var preserved [][2]string
	rows, err := pool.Query(ctx, `SELECT issue_id::text, kind FROM issue_sla_events`)
	if err != nil {
		t.Fatalf("read existing: %v", err)
	}
	for rows.Next() {
		var e [2]string
		if err := rows.Scan(&e[0], &e[1]); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		preserved = append(preserved, e)
	}
	rows.Close()
	if _, err := pool.Exec(ctx, `DELETE FROM issue_sla_events`); err != nil {
		t.Fatalf("clear: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM issue_sla_events`); err != nil {
			t.Logf("cleanup: %v", err)
		}
		for _, e := range preserved {
			if _, err := pool.Exec(context.Background(),
				`INSERT INTO issue_sla_events (issue_id, kind) VALUES ($1::uuid, $2)`, e[0], e[1]); err != nil {
				t.Logf("restore: %v", err)
			}
		}
	})

	first, err := s.ClaimSLAEscalations(ctx)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if len(first) == 0 {
		t.Skip("no issues past an SLA threshold in this database — nothing to claim")
	}
	second, err := s.ClaimSLAEscalations(ctx)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second sweep re-claimed %d escalations; each (issue, kind) must be claimed once", len(second))
	}
}
