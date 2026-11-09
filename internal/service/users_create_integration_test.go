package service_test

// Local users: POST /users creates an account that exists only in this tracker, with
// no Omni-Identity login behind it. Before this, users were created only by SyncUser
// on sign-in, so an admin had no way to add somebody, put them on a project, or
// assign them work. Runs against the same real-Postgres fixture as the authz table
// (setupAuthz), for the same reason.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
)

// doWithBody is f.do plus the response body, for cases that assert what came back.
func (f *authzFixture) doWithBody(t *testing.T, role domain.Role, method, path, body string) (int, []byte) {
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
	return rec.Code, rec.Body.Bytes()
}

// localEmail returns an address unique to this run, so the case-insensitive email
// check never trips over rows another run left behind, and registers cleanup for
// whatever user ends up holding it.
func (f *authzFixture) localEmail(t *testing.T, label string) string {
	t.Helper()
	email := label + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10] + "@local.test"
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(),
			`DELETE FROM users WHERE lower(email) = lower($1)`, email)
	})
	return email
}

func TestCreateLocalUserIsAdminOnly(t *testing.T) {
	f := setupAuthz(t)
	for role := range f.users {
		admin := role == domain.RoleOwner || role == domain.RoleAdmin
		body := `{"display_name":"Gate ` + string(role) + `","email":"` + f.localEmail(t, "gate-"+string(role)) + `"}`
		code := f.do(t, role, "POST", "/users", body)
		if permitted := code != http.StatusForbidden; permitted != admin {
			t.Errorf("POST /users as %s: HTTP %d, want admin-only=%v", role, code, admin)
		}
		if admin && code != http.StatusCreated {
			t.Errorf("POST /users as %s: HTTP %d, want 201", role, code)
		}
	}
}

func TestCreateLocalUser(t *testing.T) {
	f := setupAuthz(t)
	email := f.localEmail(t, "Pat")

	code, raw := f.doWithBody(t, domain.RoleOwner, "POST", "/users",
		`{"display_name":"  Pat Local  ","email":"`+email+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: HTTP %d %s", code, raw)
	}
	var created domain.User
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !created.IsLocal {
		t.Errorf("is_local = false, want true")
	}
	if created.Role != domain.RoleMember {
		t.Errorf("role = %q, want member (the default)", created.Role)
	}
	if created.DisplayName != "Pat Local" {
		t.Errorf("display_name = %q, want trimmed %q", created.DisplayName, "Pat Local")
	}
	if created.Email != email {
		t.Errorf("email = %q, want %q", created.Email, email)
	}

	// The subject is synthetic and can never be presented by a login.
	var sub string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT identity_sub FROM users WHERE id = $1`, created.ID).Scan(&sub); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(sub, domain.LocalIdentityPrefix) {
		t.Errorf("identity_sub = %q, want prefix %q", sub, domain.LocalIdentityPrefix)
	}

	// It shows up where admins and assignee pickers look, flagged as local.
	code, raw = f.doWithBody(t, domain.RoleMember, "GET", "/users", "")
	if code != http.StatusOK {
		t.Fatalf("list: HTTP %d", code)
	}
	var list struct{ Items []domain.User }
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, u := range list.Items {
		if u.ID == created.ID {
			found = true
			if !u.IsLocal {
				t.Errorf("listed user is_local = false, want true")
			}
		} else if u.ID == f.users[domain.RoleOwner] && u.IsLocal {
			t.Errorf("a login-backed user is flagged is_local")
		}
	}
	if !found {
		t.Errorf("created user missing from GET /users")
	}

	// The point of the feature: they can be put on a project before (or without) ever
	// signing in.
	if code := f.do(t, domain.RoleOwner, "PUT",
		"/projects/"+f.project+"/members/"+created.ID.String(), `{"role":"member"}`); code/100 != 2 {
		t.Errorf("add local user as project member: HTTP %d, want 2xx", code)
	}

	// The email check is case-insensitive.
	if code := f.do(t, domain.RoleOwner, "POST", "/users",
		`{"display_name":"Pat Again","email":"`+strings.ToUpper(email)+`"}`); code != http.StatusConflict {
		t.Errorf("duplicate email: HTTP %d, want 409", code)
	}
}

func TestCreateLocalUserValidation(t *testing.T) {
	f := setupAuthz(t)
	for _, tc := range []struct{ name, body, field string }{
		{"missing name", `{"display_name":"   ","email":"` + f.localEmail(t, "noname") + `"}`, "display_name"},
		{"not an email", `{"display_name":"X","email":"not-an-email"}`, "email"},
		{"decorated email", `{"display_name":"X","email":"X <` + f.localEmail(t, "decorated") + `>"}`, "email"},
		{"unknown role", `{"display_name":"X","email":"` + f.localEmail(t, "badrole") + `","role":"god"}`, "role"},
	} {
		code, raw := f.doWithBody(t, domain.RoleOwner, "POST", "/users", tc.body)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%s: HTTP %d, want 422", tc.name, code)
			continue
		}
		var problem struct{ Errors map[string]string }
		if err := json.Unmarshal(raw, &problem); err != nil {
			t.Fatalf("%s: decode problem: %v", tc.name, err)
		}
		if _, ok := problem.Errors[tc.field]; !ok {
			t.Errorf("%s: errors %v, want one on %q", tc.name, problem.Errors, tc.field)
		}
	}

	// An explicit valid role is honoured rather than replaced by the default.
	code, raw := f.doWithBody(t, domain.RoleAdmin, "POST", "/users",
		`{"display_name":"Rita","email":"`+f.localEmail(t, "rita")+`","role":"reporter"}`)
	if code != http.StatusCreated {
		t.Fatalf("create reporter: HTTP %d %s", code, raw)
	}
	var u domain.User
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.Role != domain.RoleReporter {
		t.Errorf("role = %q, want reporter", u.Role)
	}
}
