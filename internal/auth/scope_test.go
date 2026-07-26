package auth

import (
	"testing"

	"github.com/omni/bugtracker/internal/domain"
)

// Scopes are stored on every API token and shown in the UI; before they were
// enforced, a token created with a narrow scope still carried its owner's whole role.
func TestTokenScopesNarrowPermissions(t *testing.T) {
	p := &Principal{
		Role:     domain.RoleMaintainer,
		ViaToken: true,
		Scopes:   []string{string(PermIssueCreate)},
	}
	if !p.Can(PermIssueCreate) {
		t.Error("scoped permission should be allowed")
	}
	if p.Can(PermIssueDelete) {
		t.Error("issue:delete is outside the token's scopes and must be denied")
	}
	if p.Can(PermProjectManage) {
		t.Error("project:manage is outside the token's scopes and must be denied")
	}
}

// An empty scope list is what every token issued before scopes were enforced
// carries, and what "create token with no scopes" means: act as me, unrestricted.
func TestEmptyScopesMeanUnrestricted(t *testing.T) {
	p := &Principal{Role: domain.RoleMaintainer, ViaToken: true}
	for _, perm := range []Permission{PermIssueCreate, PermIssueDelete, PermProjectManage} {
		if !p.Can(perm) {
			t.Errorf("empty scopes should allow %s", perm)
		}
	}
}

// Scopes only ever narrow — they can never grant something the role lacks.
func TestScopesCannotExceedRole(t *testing.T) {
	p := &Principal{
		Role:     domain.RoleReporter, // may create issues and comment, nothing more
		ViaToken: true,
		Scopes:   []string{string(PermAdmin)},
	}
	if p.Can(PermIssueDelete) {
		t.Error("a reporter's token must not gain issue:delete from an admin scope")
	}
	if !p.Can(PermIssueCreate) {
		t.Error("admin:all scope should not block a permission the role does grant")
	}
}

// Browser sessions carry no token, so scopes never apply to them.
func TestScopesIgnoredForSessionPrincipals(t *testing.T) {
	p := &Principal{Role: domain.RoleAdmin, ViaToken: false, Scopes: []string{"issue:create"}}
	if !p.Can(PermProjectManage) {
		t.Error("session principal should be governed by role alone")
	}
}

func TestValidScope(t *testing.T) {
	if !ValidScope("issue:create") || !ValidScope("*") || !ValidScope("admin:all") {
		t.Error("known scopes should validate")
	}
	if ValidScope("issue:frobnicate") {
		t.Error("unknown scope should be rejected")
	}
}
