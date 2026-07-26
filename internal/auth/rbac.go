package auth

import "github.com/omni/bugtracker/internal/domain"

// Permission is a coarse capability. Kept as an explicit matrix rather than a policy
// DSL — auditable and simple, matching the "no enterprise bloat" philosophy.
type Permission string

const (
	PermProjectManage   Permission = "project:manage"
	PermIssueCreate     Permission = "issue:create"
	PermIssueUpdate     Permission = "issue:update"
	PermIssueDelete     Permission = "issue:delete"
	PermIssueTransition Permission = "issue:transition"
	PermCommentCreate   Permission = "comment:create"
	PermAutomationEdit  Permission = "automation:edit"
	PermWebhookEdit     Permission = "webhook:edit"
	PermAdmin           Permission = "admin:all"
)

// rolePermissions is the role → permission matrix. Higher roles inherit lower ones
// via explicit listing (kept flat for readability).
var rolePermissions = map[domain.Role]map[Permission]bool{
	domain.RoleOwner: {PermAdmin: true},
	domain.RoleAdmin: {PermAdmin: true},
	domain.RoleMaintainer: {
		PermProjectManage: true, PermIssueCreate: true, PermIssueUpdate: true,
		PermIssueDelete: true, PermIssueTransition: true, PermCommentCreate: true,
		PermAutomationEdit: true, PermWebhookEdit: true,
	},
	domain.RoleMember: {
		PermIssueCreate: true, PermIssueUpdate: true, PermIssueTransition: true,
		PermCommentCreate: true,
	},
	domain.RoleReporter: {
		PermIssueCreate: true, PermCommentCreate: true,
	},
	domain.RoleBot: {
		PermIssueCreate: true, PermIssueUpdate: true, PermIssueTransition: true,
		PermCommentCreate: true,
	},
}

// RoleCan reports whether a role carries the permission. Exported so per-project
// role checks (project_members) can reuse the same matrix.
func RoleCan(role domain.Role, perm Permission) bool {
	perms := rolePermissions[role]
	if perms[PermAdmin] {
		return true
	}
	return perms[perm]
}

// Can reports whether the principal may perform the permission: their global role
// must grant it AND, for API tokens, the token's scopes must allow it. Scopes only
// ever narrow — a token can never exceed its owner's role.
func (p *Principal) Can(perm Permission) bool {
	if p == nil {
		return false
	}
	return RoleCan(p.Role, perm) && p.ScopeAllows(perm)
}

// ScopeAllows applies an API token's scope list. An empty scope list means
// "unrestricted" (that is what every token issued before scopes were enforced
// carries, and it keeps `create token` with no scopes meaning "acts as me").
// A non-empty list is a whitelist: the permission itself, or the "admin:all"
// wildcard, must appear in it.
func (p *Principal) ScopeAllows(perm Permission) bool {
	if !p.ViaToken || len(p.Scopes) == 0 {
		return true
	}
	for _, s := range p.Scopes {
		if s == string(perm) || s == string(PermAdmin) || s == "*" {
			return true
		}
	}
	return false
}

// AllPermissions lists every permission a token may be scoped to.
var AllPermissions = []Permission{
	PermProjectManage, PermIssueCreate, PermIssueUpdate, PermIssueDelete,
	PermIssueTransition, PermCommentCreate, PermAutomationEdit, PermWebhookEdit, PermAdmin,
}

// ValidScope reports whether a requested token scope names a real permission.
func ValidScope(s string) bool {
	if s == "*" {
		return true
	}
	for _, p := range AllPermissions {
		if string(p) == s {
			return true
		}
	}
	return false
}
