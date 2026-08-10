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
