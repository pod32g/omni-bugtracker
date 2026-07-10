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

// Can reports whether the principal may perform the permission (global role).
func (p *Principal) Can(perm Permission) bool {
	if p == nil {
		return false
	}
	return RoleCan(p.Role, perm)
}
