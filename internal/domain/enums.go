// Package domain holds the core entities, enums and business invariants. It has no
// dependency on the database or transport layers — pure types and rules.
package domain

// IssueType discriminates the unified issues table.
type IssueType string

const (
	TypeBug         IssueType = "bug"
	TypeTask        IssueType = "task"
	TypeFeature     IssueType = "feature"
	TypeImprovement IssueType = "improvement"
)

// IssueStatus — intentionally small workflow.
type IssueStatus string

const (
	StatusOpen           IssueStatus = "open"
	StatusInProgress     IssueStatus = "in_progress"
	StatusBlocked        IssueStatus = "blocked"
	StatusReadyForReview IssueStatus = "ready_for_review"
	StatusResolved       IssueStatus = "resolved"
	StatusClosed         IssueStatus = "closed"
	StatusReopened       IssueStatus = "reopened"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

type Priority string

const (
	P0 Priority = "p0"
	P1 Priority = "p1"
	P2 Priority = "p2"
	P3 Priority = "p3"
)

type IssueSource string

const (
	SourceHuman      IssueSource = "human"
	SourceLogging    IssueSource = "logging"
	SourceMetrics    IssueSource = "metrics"
	SourceAPI        IssueSource = "api"
	SourceAutomation IssueSource = "automation"
	SourceGit        IssueSource = "git"
)

type RelationKind string

const (
	RelBlocks     RelationKind = "blocks"
	RelBlockedBy  RelationKind = "blocked_by"
	RelDuplicates RelationKind = "duplicates"
	RelRelates    RelationKind = "relates"
	RelCausedBy   RelationKind = "caused_by"
)

// Role is the RBAC role, ordered from most to least privileged.
type Role string

const (
	RoleOwner      Role = "owner"
	RoleAdmin      Role = "admin"
	RoleMaintainer Role = "maintainer"
	RoleMember     Role = "member"
	RoleReporter   Role = "reporter"
	RoleBot        Role = "bot"
)

// validTransitions defines the allowed status graph. Empty target set == terminal-ish.
var validTransitions = map[IssueStatus]map[IssueStatus]bool{
	StatusOpen:           {StatusInProgress: true, StatusBlocked: true, StatusResolved: true, StatusClosed: true},
	StatusInProgress:     {StatusBlocked: true, StatusReadyForReview: true, StatusResolved: true, StatusOpen: true},
	StatusBlocked:        {StatusInProgress: true, StatusOpen: true, StatusClosed: true},
	StatusReadyForReview: {StatusInProgress: true, StatusResolved: true, StatusClosed: true},
	StatusResolved:       {StatusClosed: true, StatusReopened: true},
	StatusClosed:         {StatusReopened: true},
	StatusReopened:       {StatusInProgress: true, StatusResolved: true, StatusClosed: true, StatusBlocked: true},
}

// CanTransition reports whether from → to is a legal status change.
func CanTransition(from, to IssueStatus) bool {
	if from == to {
		return true
	}
	targets, ok := validTransitions[from]
	return ok && targets[to]
}
