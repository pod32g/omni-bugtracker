package service_test

// The authorization table test.
//
// Nothing in this repo constructed NewHTTPHandlers, so 139 routes and 101 handler
// methods — including canOnProject, the bulk-update permission loop, attachment
// upload and download, and every admin gate — were verified by nothing at all. A
// reordered check, a missing one, or a copy-pasted permission constant would have
// shipped green.
//
// It runs against a real Postgres rather than a fake because service.Repository has
// 164 methods and one implementation: a hand-written double would be a day's work,
// would drift immediately, and would prove only that the handler called the method the
// double expected. The real store proves the answer.
//
// Authentication is stubbed and authorization is not: the middleware here injects a
// principal directly, so what is under test is the role matrix in auth/rbac.go as the
// handlers actually apply it, not JWT parsing.
//
// package service_test (external) because internal/repo/pg imports internal/service —
// an in-package test importing the store would be an import cycle.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/repo/pg"
	"github.com/omni/bugtracker/internal/service"
)

// nopPublisher swallows outbox writes. The events themselves are covered by the store
// tests; here they would only add a River dependency to a permission test.
type nopPublisher struct{}

func (nopPublisher) PublishTx(context.Context, pgx.Tx, events.DomainEventArgs) error { return nil }
func (nopPublisher) EnqueueWebhook(context.Context, events.WebhookJobArgs) error     { return nil }
func (nopPublisher) EnqueueAutoArchive(context.Context) error                        { return nil }

type authzFixture struct {
	handler   http.Handler
	pool      *pgxpool.Pool
	project   string // project the users below are members of
	projectID uuid.UUID
	other     string // a second project, for cross-project checks
	otherID   uuid.UUID
	issue     string // an issue key in `project`
	users     map[domain.Role]uuid.UUID
}

func setupAuthz(t *testing.T) *authzFixture {
	t.Helper()
	dsn := os.Getenv("OMNI_BT_TEST_DSN")
	if dsn == "" {
		t.Skip("set OMNI_BT_TEST_DSN to run the authorization tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	store := pg.New(pool)
	handler := service.NewHTTPHandlers(store, nopPublisher{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
	f := &authzFixture{
		handler: handler,
		pool:    pool,
		project: "P" + strings.ToUpper(suffix[:4]),
		other:   "Q" + strings.ToUpper(suffix[:4]),
		users:   map[domain.Role]uuid.UUID{},
	}

	var projectID, otherID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Authz Test') RETURNING id`,
		f.project).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (key, name) VALUES ($1, 'Authz Other') RETURNING id`,
		f.other).Scan(&otherID); err != nil {
		t.Fatalf("seed other project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE id IN ($1, $2)`, projectID, otherID)
	})

	// One user per global role. Their global role is what the matrix is keyed on;
	// project_members can only add, never subtract (see canOnProject).
	for _, role := range []domain.Role{
		domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer,
		domain.RoleMember, domain.RoleReporter, domain.RoleBot,
	} {
		var id uuid.UUID
		name := string(role) + "-" + suffix
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (identity_sub, email, display_name, role)
			 VALUES ($1, $2, $3, $4::app_role) RETURNING id`,
			"authz:"+name, name+"@test.local", name, string(role)).Scan(&id); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
		f.users[role] = id
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id) })
	}

	f.projectID = projectID
	f.otherID = otherID
	f.issue = f.seedIssue(t)
	return f
}

// seedIssue adds an issue to the fixture project and returns its key.
//
// Destructive cases need one of these per role: the first role permitted to delete
// actually deletes, and every role tried afterwards would get 404-not-found rather
// than 403-forbidden — which reads as "allowed" to a test that only looks for 403,
// and would quietly turn the delete row of the matrix into an assertion about nothing.
func (f *authzFixture) seedIssue(t *testing.T) string {
	t.Helper()
	var number int
	if err := f.pool.QueryRow(context.Background(),
		`UPDATE projects SET next_issue_number = next_issue_number + 1
		  WHERE id = $1 RETURNING next_issue_number - 1`, f.projectID).Scan(&number); err != nil {
		t.Fatalf("reserve issue number: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO issues (project_id, number, type, title, status, priority, reporter_id)
		 VALUES ($1, $2, 'bug', 'authz target', 'open', 'p2', $3)`,
		f.projectID, number, f.users[domain.RoleOwner]); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return fmt.Sprintf("%s-%d", f.project, number)
}

// do issues one request as the given role and returns the status code.
func (f *authzFixture) do(t *testing.T, role domain.Role, method, path, body string) int {
	t.Helper()
	var rdr io.Reader = http.NoBody
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{
		UserID: f.users[role].String(),
		Email:  string(role) + "@test.local",
		Role:   role,
	}))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec.Code
}

// TestAuthzMatrix walks every mutating route that carries a permission check against
// every global role, and asserts only one thing: whether the caller was refused.
//
// It deliberately does not assert the success status. A 200 and a 422 both mean the
// permission gate let the request through, which is the question being asked, and
// pinning the happy-path body here would make this test fail for reasons that have
// nothing to do with authorization.
func TestAuthzMatrix(t *testing.T) {
	f := setupAuthz(t)

	// allowed lists the roles that must NOT be refused. Anything absent must get 403.
	cases := []struct {
		name   string
		method string
		// path is a function of the issue key so destructive cases can be handed a
		// fresh issue for every role.
		path         func(issue string) string
		body         string
		perRoleIssue bool
		allowed      []domain.Role
	}{
		{
			name: "create issue", method: "POST", path: func(string) string { return "/projects/" + f.project + "/issues" },
			body: `{"type":"bug","title":"from the authz test"}`,
			// issue:create — everyone except, notably, nobody. Reporters file bugs;
			// that is the entire point of the role.
			allowed: []domain.Role{
				domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer,
				domain.RoleMember, domain.RoleReporter, domain.RoleBot,
			},
		},
		{
			name: "update issue", method: "PATCH", path: func(i string) string { return "/issues/" + i },
			body: `{"title":"edited by the authz test"}`,
			// issue:update — a reporter may file and comment, not rewrite.
			allowed: []domain.Role{
				domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer,
				domain.RoleMember, domain.RoleBot,
			},
		},
		{
			name: "transition issue", method: "POST", path: func(i string) string { return "/issues/" + i + "/transition" },
			body: `{"to":"in_progress"}`,
			allowed: []domain.Role{
				domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer,
				domain.RoleMember, domain.RoleBot,
			},
		},
		{
			name: "comment", method: "POST", path: func(i string) string { return "/issues/" + i + "/comments" },
			body: `{"body_md":"from the authz test"}`,
			allowed: []domain.Role{
				domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer,
				domain.RoleMember, domain.RoleReporter, domain.RoleBot,
			},
		},
		{
			name: "delete issue", method: "DELETE",
			path:         func(i string) string { return "/issues/" + i },
			perRoleIssue: true,
			// issue:delete — destructive, so maintainer and up only. A bot that can
			// file and transition must not be able to erase.
			allowed: []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer},
		},
		{
			name: "create label", method: "POST", path: func(string) string { return "/projects/" + f.project + "/labels" },
			body:    `{"name":"authz-test","color":"#ff0000"}`,
			allowed: []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer},
		},
		{
			name: "create webhook", method: "POST", path: func(string) string { return "/webhooks" },
			body:    `{"url":"https://example.invalid/hook","events":["issue.created"]}`,
			allowed: []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer},
		},
		{
			name: "create automation rule", method: "POST", path: func(string) string { return "/automation/rules" },
			body: `{"project_key":"` + f.project + `","name":"authz","trigger":{"event":"issue.created"},` +
				`"actions":[{"kind":"add_label","value":"x"}]}`,
			allowed: []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer},
		},
		{
			name: "read the audit log", method: "GET", path: func(string) string { return "/audit" },
			// Reading who changed what reveals the shape of the team — admin only.
			allowed: []domain.Role{domain.RoleOwner, domain.RoleAdmin},
		},
	}

	for _, tc := range cases {
		for role := range f.users {
			t.Run(tc.name+"/"+string(role), func(t *testing.T) {
				issue := f.issue
				if tc.perRoleIssue {
					issue = f.seedIssue(t)
				}
				code := f.do(t, role, tc.method, tc.path(issue), tc.body)
				permitted := code != http.StatusForbidden
				want := false
				for _, r := range tc.allowed {
					if r == role {
						want = true
					}
				}
				if permitted != want {
					verb := "was refused"
					if permitted {
						verb = "was allowed"
					}
					t.Errorf("%s %s as %s %s (HTTP %d), want allowed=%v",
						tc.method, tc.path(issue), role, verb, code, want)
				}
			})
		}
	}
}

// The admin endpoints guard against locking yourself out, and against everybody else
// reaching them at all. Split out because they need a target user that is not the
// caller, which the table above has no room for.
func TestAuthzAdminUserEndpoints(t *testing.T) {
	f := setupAuthz(t)
	target := f.users[domain.RoleReporter]

	for role := range f.users {
		admin := role == domain.RoleOwner || role == domain.RoleAdmin
		for _, tc := range []struct{ name, path, body string }{
			{"role", "/users/" + target.String() + "/role", `{"role":"member"}`},
			{"active", "/users/" + target.String() + "/active", `{"is_active":false}`},
		} {
			code := f.do(t, role, "PATCH", tc.path, tc.body)
			if permitted := code != http.StatusForbidden; permitted != admin {
				t.Errorf("PATCH %s as %s: HTTP %d, want admin-only=%v", tc.name, role, code, admin)
			}
		}
	}

	// Restore: the loop above deactivated the reporter, and the fixture is shared.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE users SET is_active = TRUE WHERE id = $1`, target); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// An owner must not be able to lock themselves out of the install they administer.
	owner := f.users[domain.RoleOwner]
	for _, path := range []string{"/users/" + owner.String() + "/role", "/users/" + owner.String() + "/active"} {
		body := `{"role":"member"}`
		if strings.HasSuffix(path, "/active") {
			body = `{"is_active":false}`
		}
		if code := f.do(t, domain.RoleOwner, "PATCH", path, body); code != http.StatusConflict {
			t.Errorf("owner acting on themselves at %s: HTTP %d, want 409", path, code)
		}
	}
}

// Reads are ungated by design — there is no issue:read permission and export.go says
// so out loud. This pins that as a decision rather than an oversight, so that adding
// private projects later has to come here and change it deliberately.
func TestAuthzReadsAreUngated(t *testing.T) {
	f := setupAuthz(t)
	for role := range f.users {
		for _, path := range []string{
			"/projects/" + f.project + "/issues",
			"/issues/" + f.issue,
			"/users",
		} {
			if code := f.do(t, role, "GET", path, ""); code == http.StatusForbidden {
				t.Errorf("GET %s as %s was refused — reads are supposed to be open to any "+
					"authenticated principal; if that changed, this test is the place to say so",
					path, role)
			}
		}
	}
}

// include_inactive is admin-only, and silently ignored rather than refused for
// everybody else — so a non-admin must never see a deactivated account in the list.
func TestAuthzInactiveUsersAreAdminOnly(t *testing.T) {
	f := setupAuthz(t)
	ctx := context.Background()
	target := f.users[domain.RoleReporter]
	if _, err := f.pool.Exec(ctx, `UPDATE users SET is_active = FALSE WHERE id = $1`, target); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(ctx, `UPDATE users SET is_active = TRUE WHERE id = $1`, target)
	})

	listed := func(role domain.Role) bool {
		req := httptest.NewRequest("GET", "/users?include_inactive=1", http.NoBody)
		req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{
			UserID: f.users[role].String(), Role: role,
		}))
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		var body struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, u := range body.Items {
			if u.ID == target.String() {
				return true
			}
		}
		return false
	}

	if !listed(domain.RoleOwner) {
		t.Error("an owner cannot see the deactivated user — reactivation would be unreachable")
	}
	if listed(domain.RoleMember) {
		t.Error("a member saw a deactivated account via include_inactive")
	}
}

// TestCarryOverAuthorizesTheTargetIteration covers the half of the carry-over check
// that did not exist.
//
// carryOverIteration authorized the *source* iteration, then read body.to, parsed it
// as a UUID, and handed it to CarryOverIssues. The target was never resolved to a
// project and never authorized — so somebody with project:manage on one project could
// push its unfinished issues into an iteration belonging to a project they have no
// rights to. The issues stayed in their own project while pointing at a foreign
// iteration, which corrupts that iteration's burndown, velocity and effort rollups.
//
// The matrix test above cannot express this: it keys on global roles, and a global
// maintainer has project:manage everywhere. The exposure needs someone whose rights
// come from membership — global `member`, maintainer in the source project only.
func TestCarryOverAuthorizesTheTargetIteration(t *testing.T) {
	f := setupAuthz(t)
	ctx := context.Background()

	source := f.seedIteration(t, f.projectID, "source")
	sameProject := f.seedIteration(t, f.projectID, "sibling")
	foreign := f.seedIteration(t, f.otherID, "foreign")

	// Rights by membership, not by global role: maintainer of f.project, nothing at
	// all in f.other.
	member := f.users[domain.RoleMember]
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'maintainer')`,
		f.projectID, member); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	carry := func(target uuid.UUID) int {
		return f.do(t, domain.RoleMember, "POST",
			"/iterations/"+source.String()+"/carry-over",
			fmt.Sprintf(`{"to":%q}`, target))
	}

	if code := carry(foreign); code != http.StatusForbidden {
		t.Errorf("carry-over into another project's iteration = %d, want 403", code)
	}
	// The control: the same caller, the same route, a target they do own. If this
	// were also refused the check above would be proving nothing.
	if code := carry(sameProject); code == http.StatusForbidden {
		t.Errorf("carry-over within the caller's own project was refused (%d)", code)
	}
	// A target that does not exist is a 404 — not a 500 from CarryOverIssues, and not
	// a 403 that would confirm the id belongs to somebody.
	if code := carry(uuid.New()); code != http.StatusNotFound {
		t.Errorf("carry-over into a nonexistent iteration = %d, want 404", code)
	}
}

// seedIteration adds an iteration to the given project and returns its id.
func (f *authzFixture) seedIteration(t *testing.T, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO iterations (project_id, name, starts_on, ends_on)
		 VALUES ($1, $2, current_date, current_date + 14) RETURNING id`,
		projectID, name+"-"+strings.ReplaceAll(uuid.NewString(), "-", "")[:6]).Scan(&id); err != nil {
		t.Fatalf("seed iteration %s: %v", name, err)
	}
	return id
}

// TestBulkLabelEditIsAdditive is the regression test for the bulk endpoint eating
// labels.
//
// patch.labels goes to UpdateIssue, which treats the array as the complete set: the
// store deletes every row in issue_labels for the issue and re-inserts what was sent.
// Selecting fifty issues and "adding" one label therefore stripped every other label
// from all fifty, in one click, with no confirmation and no undo — and unlike the
// single-issue form, the user could not see what they were about to discard.
func TestBulkLabelEditIsAdditive(t *testing.T) {
	f := setupAuthz(t)
	ctx := context.Background()

	issueKey := f.seedIssue(t)
	var issueID uuid.UUID
	if err := f.pool.QueryRow(ctx,
		`SELECT i.id FROM issues i JOIN projects p ON p.id = i.project_id
		  WHERE p.key = $1 AND i.number = split_part($2, '-', 2)::int`,
		f.project, issueKey).Scan(&issueID); err != nil {
		t.Fatalf("resolve issue: %v", err)
	}

	labelsNow := func() []string {
		t.Helper()
		var names []string
		if err := f.pool.QueryRow(ctx,
			`SELECT COALESCE(array(SELECT l.name FROM issue_labels il JOIN labels l ON l.id = il.label_id
			                        WHERE il.issue_id = $1 ORDER BY l.name), '{}')`, issueID).Scan(&names); err != nil {
			t.Fatalf("read labels: %v", err)
		}
		return names
	}
	bulk := func(patch string) int {
		return f.do(t, domain.RoleOwner, "POST", "/issues/bulk",
			fmt.Sprintf(`{"ids":[%q],"patch":%s}`, issueID, patch))
	}

	// Two labels the caller of the "add one label" request does not know about.
	if code := bulk(`{"labels":["regression","backend"]}`); code != http.StatusOK {
		t.Fatalf("seed labels: %d", code)
	}
	if got := labelsNow(); len(got) != 2 {
		t.Fatalf("seeding produced %v, want two labels", got)
	}

	// The bug: this used to leave the issue with exactly ["urgent"].
	if code := bulk(`{"labels_add":["urgent"]}`); code != http.StatusOK {
		t.Fatalf("labels_add: %d", code)
	}
	if got := labelsNow(); len(got) != 3 {
		t.Errorf("labels_add produced %v — the labels it did not name were discarded", got)
	}

	// Removal takes one out and leaves the rest, case-insensitively.
	if code := bulk(`{"labels_remove":["BACKEND"]}`); code != http.StatusOK {
		t.Fatalf("labels_remove: %d", code)
	}
	got := labelsNow()
	if len(got) != 2 {
		t.Errorf("labels_remove produced %v, want two labels", got)
	}
	for _, n := range got {
		if strings.EqualFold(n, "backend") {
			t.Errorf("labels_remove left %q in place", n)
		}
	}

	// Replace still replaces — it is a real intention, just not the default one.
	if code := bulk(`{"labels":["only"]}`); code != http.StatusOK {
		t.Fatalf("labels replace: %d", code)
	}
	if got := labelsNow(); len(got) != 1 || got[0] != "only" {
		t.Errorf("replace produced %v, want [only]", got)
	}

	// Replace and edit together have no obvious meaning, so they are refused rather
	// than resolved by an argument order nobody can see.
	if code := bulk(`{"labels":["a"],"labels_add":["b"]}`); code != http.StatusUnprocessableEntity {
		t.Errorf("labels + labels_add = %d, want 422", code)
	}
	if code := bulk(`{"labels_add":["x"],"labels_remove":["X"]}`); code != http.StatusUnprocessableEntity {
		t.Errorf("the same label in add and remove = %d, want 422", code)
	}
}

// TestProjectPathAcceptsAUUID covers the 500 you got for guessing the obvious.
//
// Every /projects/{key}/... route treats the segment as the project KEY — issue
// creation allocates numbers with `UPDATE projects ... WHERE key = $1`. Posting to
// /projects/{uuid}/issues is a natural guess, because project objects expose `id`, and
// it made that UPDATE match nothing: 500 `allocate number: no rows in result set`.
func TestProjectPathAcceptsAUUID(t *testing.T) {
	f := setupAuthz(t)

	body := `{"type":"bug","title":"filed against a project uuid"}`
	if code := f.do(t, domain.RoleOwner, "POST",
		"/projects/"+f.projectID.String()+"/issues", body); code != http.StatusCreated {
		t.Errorf("POST /projects/{uuid}/issues = %d, want 201", code)
	}
	// Reads too — this one used to answer an empty list, which is quieter and worse.
	if code := f.do(t, domain.RoleOwner, "GET",
		"/projects/"+f.projectID.String()+"/issues", ""); code != http.StatusOK {
		t.Errorf("GET /projects/{uuid}/issues = %d, want 200", code)
	}
	// An id that resolves to nothing is a 404 naming the project, not a 500 out of
	// the depths of the store.
	if code := f.do(t, domain.RoleOwner, "GET",
		"/projects/"+uuid.NewString()+"/issues", ""); code != http.StatusNotFound {
		t.Errorf("GET /projects/{unknown uuid}/issues = %d, want 404", code)
	}
	// Keys still work, obviously.
	if code := f.do(t, domain.RoleOwner, "GET", "/projects/"+f.project+"/issues", ""); code != http.StatusOK {
		t.Errorf("GET /projects/{key}/issues = %d, want 200", code)
	}
}

// TestCrossProjectListHonoursProject covers ?project= being accepted and ignored: the
// response looked like a valid answer to the question that was asked, and was the
// answer to a wider one.
func TestCrossProjectListHonoursProject(t *testing.T) {
	f := setupAuthz(t)
	f.seedIssue(t)

	total := func(query string) float64 {
		t.Helper()
		var out struct {
			Total float64 `json:"total"`
		}
		if code := f.doJSON(t, domain.RoleOwner, "GET", "/issues"+query, "", &out); code != http.StatusOK {
			t.Fatalf("GET /issues%s = %d", query, code)
		}
		return out.Total
	}

	// f.other exists and has no issues, so it is the clean probe: before the fix this
	// reported the global total, because the parameter reached nothing.
	global := total("?limit=1")
	if global == 0 {
		t.Fatal("no issues at all — nothing to scope")
	}
	if empty := total("?limit=1&project=" + f.other); empty != 0 {
		t.Errorf("?project=%s reported %v of a global %v — the parameter is being ignored",
			f.other, empty, global)
	}
	scoped := total("?limit=1&project=" + f.project)
	if scoped == 0 {
		t.Errorf("?project=%s reported nothing, but the project has issues", f.project)
	}
	// Lower case is the same project. Callers type it either way.
	if lower := total("?limit=1&project=" + strings.ToLower(f.project)); lower != scoped {
		t.Errorf("lower-case project key reported %v, want %v", lower, scoped)
	}
	// A key that does not exist is a 404: "no issues" and "no such project" are
	// different answers, and returning the first for the second is how a typo reads
	// as a clean backlog.
	if code := f.do(t, domain.RoleOwner, "GET", "/issues?project=NOPE", ""); code != http.StatusNotFound {
		t.Errorf("?project=NOPE = %d, want 404", code)
	}
}

// TestTransitionNamesTheFieldYouGotWrong: an absent `to` used to fall through to the
// workflow graph and come back as `409 invalid transition: open → `, with nothing on
// the right-hand side — the caller was told their transition was illegal rather than
// that their request was malformed.
func TestTransitionNamesTheFieldYouGotWrong(t *testing.T) {
	f := setupAuthz(t)
	path := "/issues/" + f.seedIssue(t) + "/transition"

	for _, tc := range []struct{ name, body string }{
		{"empty body", `{}`},
		{"the field it is called everywhere else", `{"status":"closed"}`},
		{"blank target", `{"to":"  "}`},
	} {
		if code := f.do(t, domain.RoleOwner, "POST", path, tc.body); code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422", tc.name, code)
		}
	}
	// A status that is not a status is also the request's fault, not the graph's.
	if code := f.do(t, domain.RoleOwner, "POST", path, `{"to":"finished"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("unknown status = %d, want 422", code)
	}
	// ...and a genuinely illegal edge is still a 409, which is what that code is for.
	// open → reopened is not in the graph: you cannot reopen something still open.
	if code := f.do(t, domain.RoleOwner, "POST", path, `{"to":"reopened"}`); code != http.StatusConflict {
		t.Errorf("open → reopened = %d, want 409", code)
	}
}

// doJSON is `do` plus decoding the body, for the cases that need to read the answer
// rather than only the status.
func (f *authzFixture) doJSON(t *testing.T, role domain.Role, method, path, body string, out any) int {
	t.Helper()
	var rdr io.Reader = http.NoBody
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{
		UserID: f.users[role].String(),
		Email:  string(role) + "@test.local",
		Role:   role,
	}))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
	return rec.Code
}

// TestPatchRefusesWhatItCannotApply is the regression test for PATCH returning 200
// while discarding the write.
//
// The endpoint accepted status, git_commit_sha, pull_request_url and unknown component
// names, answered 200 with a full issue body, and applied none of them. A client had
// no way to learn which subset of the documented fields was actually writable except
// by reading back and diffing every time.
func TestPatchRefusesWhatItCannotApply(t *testing.T) {
	f := setupAuthz(t)
	path := "/issues/" + f.seedIssue(t)

	// A field that lives somewhere else says where, rather than being swallowed.
	for _, body := range []string{
		`{"status":"closed"}`,
		`{"fields":{"x":1}}`,
		`{"project_key":"OTHER"}`,
		`{"nonsense":true}`,
	} {
		if code := f.do(t, domain.RoleOwner, "PATCH", path, body); code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH %s = %d, want 422", body, code)
		}
	}

	// A component the project does not have used to be accepted and dropped by the
	// INSERT ... SELECT that could not match it.
	if code := f.do(t, domain.RoleOwner, "PATCH", path, `{"components":["no-such-component"]}`); code != http.StatusUnprocessableEntity {
		t.Errorf("PATCH unknown component = %d, want 422", code)
	}

	// And the two columns every issue response carries, which nothing had ever
	// written, are now writable — which is what the spec always said.
	var out struct {
		GitCommitSHA   string `json:"git_commit_sha"`
		PullRequestURL string `json:"pull_request_url"`
	}
	patch := `{"git_commit_sha":"deadbeef","pull_request_url":"https://example.test/pr/1"}`
	if code := f.doJSON(t, domain.RoleOwner, "PATCH", path, patch, &out); code != http.StatusOK {
		t.Fatalf("PATCH git fields = %d, want 200", code)
	}
	if out.GitCommitSHA != "deadbeef" || out.PullRequestURL != "https://example.test/pr/1" {
		t.Errorf("the response does not reflect the write: %+v", out)
	}
	// Read back, because "the response says so" and "it was stored" are the two
	// things this bug was about telling apart.
	f.doJSON(t, domain.RoleOwner, "GET", path, "", &out)
	if out.GitCommitSHA != "deadbeef" {
		t.Errorf("git_commit_sha did not survive the round trip: %q", out.GitCommitSHA)
	}
}
