package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/service"
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

	first, err := s.ClaimSLAEscalations(ctx, nil)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if len(first) == 0 {
		t.Skip("no issues past an SLA threshold in this database — nothing to claim")
	}
	second, err := s.ClaimSLAEscalations(ctx, nil)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second sweep re-claimed %d escalations; each (issue, kind) must be claimed once", len(second))
	}

	// The claim is only worth making if it can be announced. An enqueue that fails
	// must take the claim with it, or the escalation is recorded as told and never
	// told — which the next sweep will not fix, because it skips claimed rows.
	if _, err := pool.Exec(ctx, `DELETE FROM issue_sla_events`); err != nil {
		t.Fatalf("reset for rollback check: %v", err)
	}
	boom := errors.New("enqueue exploded")
	if _, err := s.ClaimSLAEscalations(ctx, func(pgx.Tx, []service.SLAEscalation) error {
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	again, err := s.ClaimSLAEscalations(ctx, nil)
	if err != nil {
		t.Fatalf("sweep after rollback: %v", err)
	}
	if len(again) != len(first) {
		t.Errorf("after a failed enqueue %d escalations are re-claimable, want %d — the "+
			"rest were consumed with nobody told", len(again), len(first))
	}
}

// TestIntegrationTransitionIssueComment is the regression test for the API
// accepting a `comment` on a transition and silently discarding it. It also
// pins the two properties that made the fix worth doing at the repo layer
// rather than as a second call from the handler: the comment is atomic with the
// status change, and the activity row records what the status actually became.
func TestIntegrationTransitionIssueComment(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := &Store{pool: pool}

	seed := func(t *testing.T) (issueID, actorID uuid.UUID) {
		t.Helper()
		suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
		var projectID uuid.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (identity_sub, email, display_name, role)
			 VALUES ($1, $2, $3, 'owner') RETURNING id`,
			"test:"+suffix, suffix+"@test.local", "tester-"+suffix).Scan(&actorID); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO projects (key, name) VALUES ($1, 'Transition Test') RETURNING id`,
			"X"+strings.ToUpper(suffix[:4])).Scan(&projectID); err != nil {
			t.Fatalf("seed project: %v", err)
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
			 VALUES ($1, 1, 'bug', 'transition target', 'open', 'p2', $2) RETURNING id`,
			projectID, actorID).Scan(&issueID); err != nil {
			t.Fatalf("seed issue: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actorID)
		})
		return issueID, actorID
	}

	commentBodies := func(t *testing.T, issueID uuid.UUID) []string {
		t.Helper()
		rows, err := pool.Query(ctx,
			`SELECT body_md FROM comments WHERE issue_id = $1 AND deleted_at IS NULL`, issueID)
		if err != nil {
			t.Fatalf("read comments: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var b string
			if err := rows.Scan(&b); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, b)
		}
		return out
	}

	changesOf := func(t *testing.T, issueID uuid.UUID, verb string) string {
		t.Helper()
		var changes []byte
		if err := pool.QueryRow(ctx,
			`SELECT changes FROM activity WHERE issue_id = $1 AND verb = $2
			 ORDER BY occurred_at DESC LIMIT 1`, issueID, verb).Scan(&changes); err != nil {
			t.Fatalf("read activity %s: %v", verb, err)
		}
		return string(changes)
	}

	t.Run("comment is recorded", func(t *testing.T) {
		issueID, actorID := seed(t)
		changes := []byte(`{"status":{"from":"open","to":"resolved"}}`)
		issue, err := store.TransitionIssue(ctx, issueID, domain.StatusOpen, domain.StatusResolved, actorID,
			"fixed in a0cf1ff", changes, nil)
		if err != nil {
			t.Fatalf("transition: %v", err)
		}
		if issue.Status != domain.StatusResolved {
			t.Fatalf("status = %q", issue.Status)
		}
		got := commentBodies(t, issueID)
		if len(got) != 1 || got[0] != "fixed in a0cf1ff" {
			t.Fatalf("comment not recorded: %#v", got)
		}
		// The timeline must say what the status became, not just that it changed.
		// Compare parsed, since jsonb round-trips with its own spacing and key order.
		var recorded struct {
			Status struct{ From, To string } `json:"status"`
		}
		raw := changesOf(t, issueID, "issue.status_changed")
		if err := json.Unmarshal([]byte(raw), &recorded); err != nil {
			t.Fatalf("activity changes not JSON: %s", raw)
		}
		if recorded.Status.From != "open" || recorded.Status.To != "resolved" {
			t.Fatalf("activity changes = %s", raw)
		}
		// The comment gets a timeline entry of its own, exactly as AddComment would.
		if changesOf(t, issueID, "comment.created") == "" {
			t.Fatal("no comment.created activity entry")
		}
	})

	t.Run("empty and whitespace comments create nothing", func(t *testing.T) {
		for _, body := range []string{"", "   ", "\n\t "} {
			issueID, actorID := seed(t)
			if _, err := store.TransitionIssue(ctx, issueID, domain.StatusOpen, domain.StatusResolved, actorID,
				body, nil, nil); err != nil {
				t.Fatalf("transition: %v", err)
			}
			if got := commentBodies(t, issueID); len(got) != 0 {
				t.Fatalf("body %q produced comments: %#v", body, got)
			}
		}
	})

	// The reason this belongs in one transaction: if publishing fails after the
	// status update, neither the status change nor its explanation may survive.
	t.Run("a failure rolls back both", func(t *testing.T) {
		issueID, actorID := seed(t)
		boom := errors.New("publish exploded")
		_, err := store.TransitionIssue(ctx, issueID, domain.StatusOpen, domain.StatusResolved, actorID,
			"should not survive", nil, func(pgx.Tx) error { return boom })
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM issues WHERE id = $1`, issueID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != "open" {
			t.Errorf("status committed despite failure: %q", status)
		}
		if got := commentBodies(t, issueID); len(got) != 0 {
			t.Errorf("comment committed despite failure: %#v", got)
		}
	})
}

// TestIntegrationArchiveStaleClosedSkipsReopened is the regression test for auto-archive
// hiding live work.
//
// closed_at is never cleared on reopen — both write sites keep the old value with
// `CASE WHEN ... THEN now() ELSE closed_at END` — so an issue that was closed months
// ago and reopened yesterday still carries a stale closed_at. Selecting on that
// timestamp alone archived it: the bug disappeared from every list and from search
// while somebody was assigned to it, and nothing in the UI said why.
func TestIntegrationArchiveStaleClosedSkipsReopened(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := &Store{pool: pool}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	var actorID, projectID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"test:"+suffix, suffix+"@test.local", "tester-"+suffix).Scan(&actorID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Archive Test') RETURNING id`,
		"A"+strings.ToUpper(suffix[:4])).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})

	// Every row carries the same long-stale closed_at. Status is the only difference,
	// which is exactly what the predicate under test has to notice.
	newIssue := func(t *testing.T, number int, status string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id, closed_at)
			 VALUES ($1, $2, 'bug', $3, $4::issue_status, 'p2', $5, now() - interval '400 days')
			 RETURNING id`,
			projectID, number, status+" issue", status, actorID).Scan(&id); err != nil {
			t.Fatalf("seed %s issue: %v", status, err)
		}
		return id
	}
	closed := newIssue(t, 1, "closed")
	resolved := newIssue(t, 2, "resolved")
	reopened := newIssue(t, 3, "reopened")
	inProgress := newIssue(t, 4, "in_progress")

	if _, err := store.ArchiveStaleClosed(ctx, 30, actorID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	archived := func(t *testing.T, id uuid.UUID) bool {
		t.Helper()
		var got bool
		if err := pool.QueryRow(ctx,
			`SELECT archived_at IS NOT NULL FROM issues WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read archived_at: %v", err)
		}
		return got
	}

	for _, tc := range []struct {
		name string
		id   uuid.UUID
		want bool
	}{
		{"closed", closed, true},
		{"resolved", resolved, true},
		// The bug: both of these carry a stale closed_at but are live work.
		{"reopened", reopened, false},
		{"in_progress", inProgress, false},
	} {
		if got := archived(t, tc.id); got != tc.want {
			t.Errorf("%s issue: archived = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIntegrationDeactivationRevokesAccess covers the offboarding path end to end.
//
// users.is_active existed in the schema from the start and was written by nothing and
// read only by the assignee picker, so "deactivating" somebody removed them from a
// dropdown and left every credential they held working. The two assertions that matter
// are that an existing token stops resolving and that reactivation is not a one-way
// door.
func TestIntegrationDeactivationRevokesAccess(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := &Store{pool: pool}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"test:"+suffix, suffix+"@test.local", "leaver-"+suffix).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) })

	tokenHash := []byte("hash-" + suffix + "-0123456789abcdef")
	if _, err := pool.Exec(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash, scopes) VALUES ($1, 'ci', $2, '{}')`,
		userID, tokenHash); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if _, err := store.GetUserByToken(ctx, tokenHash); err != nil {
		t.Fatalf("token should resolve while the user is active: %v", err)
	}

	if _, err := store.SetUserActive(ctx, userID, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// The token is revoked in the same transaction *and* the join filters on
	// is_active, so this fails for two independent reasons — deliberately, because
	// tokens issued before this existed are only caught by the second.
	if _, err := store.GetUserByToken(ctx, tokenHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deactivated user's token still resolves: err = %v", err)
	}
	var revoked bool
	if err := pool.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM api_tokens WHERE token_hash = $1`, tokenHash).Scan(&revoked); err != nil {
		t.Fatalf("read token: %v", err)
	}
	if !revoked {
		t.Error("deactivation left the API token unrevoked")
	}

	// A deactivated user must not be offered as an assignee, but must still be
	// visible to the admin screen — otherwise reactivation is unreachable.
	active, err := store.ListUsers(ctx, 500, false)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	for _, u := range active {
		if u.ID == userID {
			t.Error("deactivated user appears in the default (assignee) listing")
		}
	}
	all, err := store.ListUsers(ctx, 500, true)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	found := false
	for _, u := range all {
		if u.ID == userID {
			found = true
			if u.IsActive == nil || *u.IsActive {
				t.Error("deactivated user reports IsActive != false in the admin listing")
			}
		}
	}
	if !found {
		t.Error("deactivated user is invisible to the admin listing — reactivation is unreachable")
	}

	// Reactivation restores the account. The revoked token stays revoked: it was
	// exposed by the departure, and handing it back would defeat the point.
	u, err := store.SetUserActive(ctx, userID, true)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if u.IsActive == nil || !*u.IsActive {
		t.Error("reactivation did not set is_active")
	}
	if _, err := store.GetUserByToken(ctx, tokenHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Error("reactivation un-revoked a token that was revoked on departure")
	}
}

// TestIntegrationTransitionIssueCompareAndSet covers the lost-update window on status.
//
// The workflow graph was enforced in Go against a status read in a separate query, and
// the UPDATE that followed matched on id alone. Two callers could therefore both read
// `open`, both validate their edge, and both write — the second silently overwriting
// the first, producing a state the graph forbids and two contradictory
// issue.status_changed events for one issue. The predicate makes the read part of the
// write, so the loser is told rather than ignored.
func TestIntegrationTransitionIssueCompareAndSet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := &Store{pool: pool}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	var actorID, projectID, issueID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"test:"+suffix, suffix+"@test.local", "tester-"+suffix).Scan(&actorID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'CAS Test') RETURNING id`,
		"C"+strings.ToUpper(suffix[:4])).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actorID)
	})
	if err := pool.QueryRow(ctx,
		`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
		 VALUES ($1, 1, 'bug', 'cas target', 'open', 'p2', $2) RETURNING id`,
		projectID, actorID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	// First writer wins: open → in_progress.
	if _, err := store.TransitionIssue(ctx, issueID,
		domain.StatusOpen, domain.StatusInProgress, actorID, "", nil, nil); err != nil {
		t.Fatalf("first transition: %v", err)
	}

	// Second writer is working from the stale `open` it read before the first landed.
	// blocked is a legal target *from open*, so the Go-side check passes and only the
	// predicate can stop it.
	_, err := store.TransitionIssue(ctx, issueID,
		domain.StatusOpen, domain.StatusBlocked, actorID, "", nil, nil)
	if !errors.Is(err, service.ErrStaleStatus) {
		t.Fatalf("stale transition: err = %v, want ErrStaleStatus", err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM issues WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "in_progress" {
		t.Errorf("status = %q, want in_progress — the stale write overwrote the live one", status)
	}

	// A deleted issue is still ErrNoRows, not a conflict: nothing to retry against.
	if _, err := pool.Exec(ctx, `UPDATE issues SET deleted_at = now() WHERE id = $1`, issueID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := store.TransitionIssue(ctx, issueID,
		domain.StatusInProgress, domain.StatusResolved, actorID, "", nil, nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("deleted issue: err = %v, want pgx.ErrNoRows", err)
	}
}

// TestIntegrationUpdateCommentPublishes covers the mention that an edit records and
// nobody hears about.
//
// syncMentions writes issue_mentions rows with notified_at NULL for the dispatcher to
// claim, and the dispatcher only runs off an enqueued event. UpdateComment was the one
// write in this file with no publish hook, so editing a comment to add "@alex can you
// look" recorded the mention and notified nobody — it sat unclaimed until some
// unrelated event on the same issue swept it up carrying that event's payload.
func TestIntegrationUpdateCommentPublishes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := &Store{pool: pool}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	var authorID, targetID, projectID, issueID, commentID uuid.UUID
	for _, u := range []struct {
		into *uuid.UUID
		name string
	}{{&authorID, "author-" + suffix}, {&targetID, "target-" + suffix}} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (identity_sub, email, display_name, role)
			 VALUES ($1, $2, $3, 'member') RETURNING id`,
			"test:"+u.name, u.name+"@test.local", u.name).Scan(u.into); err != nil {
			t.Fatalf("seed user %s: %v", u.name, err)
		}
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Edit Test') RETURNING id`,
		"E"+strings.ToUpper(suffix[:4])).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, authorID, targetID)
	})
	// resolveHandles only resolves a plain `member` who actually belongs to the
	// project — you cannot mention somebody into a project they cannot see. Both users
	// here are members, so both need the row.
	if _, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'member')`,
		projectID, authorID, targetID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
		 VALUES ($1, 1, 'bug', 'edit target', 'open', 'p2', $2) RETURNING id`,
		projectID, authorID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO comments (issue_id, author_id, body_md) VALUES ($1, $2, 'no mention here') RETURNING id`,
		issueID, authorID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	// Editing in a mention must both record the row and run the hook, in one tx.
	published := 0
	if _, err := store.UpdateComment(ctx, commentID, authorID,
		"actually @target-"+suffix+" should look at this",
		func(pgx.Tx) error { published++; return nil }); err != nil {
		t.Fatalf("update comment: %v", err)
	}
	if published != 1 {
		t.Fatalf("publish ran %d times, want 1 — without it the mention below notifies nobody", published)
	}

	var pending int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM issue_mentions
		  WHERE issue_id = $1 AND user_id = $2 AND notified_at IS NULL`,
		issueID, targetID).Scan(&pending); err != nil {
		t.Fatalf("read mentions: %v", err)
	}
	if pending != 1 {
		t.Errorf("pending mentions = %d, want 1", pending)
	}

	// A failing hook must take the edit with it — a body claiming to mention somebody
	// while the mention was rolled back is the worse of the two outcomes.
	boom := errors.New("publish exploded")
	if _, err := store.UpdateComment(ctx, commentID, authorID, "rolled back",
		func(pgx.Tx) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	var body string
	if err := pool.QueryRow(ctx, `SELECT body_md FROM comments WHERE id = $1`, commentID).Scan(&body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if body == "rolled back" {
		t.Error("edit committed despite the publish failing")
	}
}

// TestIntegrationResolvedAtRestampsOnReResolution pins the CASE in setIssueStatusSQL.
//
// The old condition was `AND resolved_at IS NULL`, so an issue resolved in January,
// reopened in June and resolved again in July still reported January — and every
// metric derived from resolved_at was wrong for exactly the issues most worth
// measuring, the ones that regressed.
//
// Runs the real statement (the same const the two write paths use) inside a
// transaction that is always rolled back.
func TestIntegrationResolvedAtRestampsOnReResolution(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // the test never commits

	issueID := seedStatusIssue(t, ctx, tx)

	setStatus := func(to string) {
		t.Helper()
		if _, err := tx.Exec(ctx, setIssueStatusSQL, issueID, to); err != nil {
			t.Fatalf("set status %s: %v", to, err)
		}
	}
	resolvedAt := func() *time.Time {
		t.Helper()
		var at *time.Time
		if err := tx.QueryRow(ctx, `SELECT resolved_at FROM issues WHERE id = $1`, issueID).Scan(&at); err != nil {
			t.Fatalf("read resolved_at: %v", err)
		}
		return at
	}
	// now() is the *transaction* timestamp, so two writes in this one transaction
	// stamp the identical value and "did it move?" cannot be asked directly. Backdating
	// between the writes gives the question an answer: a re-stamp lands on the
	// transaction clock, and not re-stamping leaves January in place.
	backdate := func(d time.Duration) time.Time {
		t.Helper()
		var at time.Time
		if err := tx.QueryRow(ctx,
			`UPDATE issues SET resolved_at = now() - $2::interval WHERE id = $1 RETURNING resolved_at`,
			issueID, fmt.Sprintf("%d seconds", int(d.Seconds()))).Scan(&at); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		return at
	}

	if at := resolvedAt(); at != nil {
		t.Fatalf("a freshly opened issue has resolved_at = %v", at)
	}

	setStatus("resolved")
	if resolvedAt() == nil {
		t.Fatal("resolving did not stamp resolved_at")
	}

	// resolved → closed is not a new resolution. The issue finished once.
	january := backdate(200 * 24 * time.Hour)
	setStatus("closed")
	if got := resolvedAt(); !got.Equal(january) {
		t.Errorf("closing an already-resolved issue moved resolved_at: %v -> %v", january, *got)
	}

	// Reopening leaves the date in place — the reports series depends on it surviving,
	// and issue_sla already gates on status rather than on this column.
	setStatus("open")
	if got := resolvedAt(); got == nil || !got.Equal(january) {
		t.Errorf("reopening cleared resolved_at: %v", got)
	}

	// ...and resolving again re-stamps it. This is the assertion the bug was about:
	// before the fix, `AND resolved_at IS NULL` left January here forever.
	setStatus("resolved")
	second := resolvedAt()
	if second == nil {
		t.Fatal("re-resolution left resolved_at NULL")
	}
	if !second.After(january) {
		t.Errorf("re-resolution kept the first date: %v (first was %v)", *second, january)
	}
}

// seedStatusIssue inserts a throwaway project + reporter + open issue and returns the
// issue id. Everything lands in the caller's transaction, so nothing survives.
func seedStatusIssue(t *testing.T, ctx context.Context, tx pgx.Tx) uuid.UUID {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]

	var reporterID, projectID, issueID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"test:"+suffix, suffix+"@test.local", "status-"+suffix).Scan(&reporterID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Status Test') RETURNING id`,
		"S"+strings.ToUpper(suffix[:4])).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
		 VALUES ($1, 1, 'bug', 'status target', 'open', 'p2', $2) RETURNING id`,
		projectID, reporterID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return issueID
}

// TestIntegrationVelocityUsesTheCommitmentSnapshot is the regression test for velocity
// reporting every carried-over sprint as 100% delivered.
//
// Both numbers came from issues whose iteration_id *currently* points at the
// iteration, and CarryOverIssues moves out exactly the issues that are not resolved or
// closed. So the moment a sprint was closed with carry-over the only issues left
// pointing at it were the finished ones, planned collapsed onto done, and the panel
// rendered "2/2 done" for a sprint that delivered two of four.
func TestIntegrationVelocityUsesTheCommitmentSnapshot(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := New(pool)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	projectKey := "V" + strings.ToUpper(suffix[:4])

	var reporterID, projectID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (identity_sub, email, display_name, role)
		 VALUES ($1, $2, $3, 'owner') RETURNING id`,
		"vel:"+suffix, suffix+"@test.local", "vel-"+suffix).Scan(&reporterID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Velocity Test') RETURNING id`,
		projectKey).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, reporterID)
	})

	var sprint, next uuid.UUID
	for _, it := range []struct {
		into *uuid.UUID
		name string
	}{{&sprint, "sprint-1"}, {&next, "sprint-2"}} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO iterations (project_id, name, starts_on, ends_on)
			 VALUES ($1, $2, current_date - 14, current_date) RETURNING id`,
			projectID, it.name+"-"+suffix).Scan(it.into); err != nil {
			t.Fatalf("seed iteration %s: %v", it.name, err)
		}
	}

	// Four issues committed to the sprint; two of them will get finished.
	for n := 1; n <= 4; n++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id,
			                     iteration_id, estimate_minutes)
			 VALUES ($1, $2, 'bug', 'velocity target', 'open', 'p2', $3, $4, 60)`,
			projectID, n, reporterID, sprint); err != nil {
			t.Fatalf("seed issue %d: %v", n, err)
		}
	}

	active := domain.IterationActive
	if _, err := store.UpdateIteration(ctx, sprint, service.UpdateIterationInput{State: &active}); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Two get done, two do not — and the two that do not are carried into the next
	// sprint, which is what removes the evidence.
	if _, err := pool.Exec(ctx,
		`UPDATE issues SET status = 'resolved', resolved_at = now()
		  WHERE iteration_id = $1 AND number <= 2`, sprint); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	moved, err := store.CarryOverIssues(ctx, sprint, &next)
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 2 {
		t.Fatalf("carried over %d issues, want 2", moved)
	}

	completed := domain.IterationCompleted
	if _, err := store.UpdateIteration(ctx, sprint, service.UpdateIterationInput{State: &completed}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	history, err := store.IterationVelocity(ctx, projectKey, 5)
	if err != nil {
		t.Fatalf("velocity: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("velocity returned %d rows, want 1", len(history))
	}
	v := history[0]
	if v.DoneIssues != 2 {
		t.Errorf("done_issues = %d, want 2", v.DoneIssues)
	}
	// The assertion the bug was about: before the snapshot this was 2, and the sprint
	// reported 2/2 — a perfect record for delivering half the work.
	if v.PlannedIssues != 4 {
		t.Errorf("planned_issues = %d, want 4 — the commitment is being re-derived from "+
			"what survived carry-over", v.PlannedIssues)
	}
	if v.PlannedMinutes != 240 {
		t.Errorf("planned_minutes = %d, want 240", v.PlannedMinutes)
	}
	if !v.Committed {
		t.Error("committed = false, but this iteration was activated and snapshotted")
	}
}
