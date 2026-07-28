package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID        uuid.UUID  `json:"id"`
	IssueID   uuid.UUID  `json:"issue_id"`
	Author    *User      `json:"author,omitempty"`
	BodyMD    string     `json:"body_md"`
	EditedAt  *time.Time `json:"edited_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	// ProjectKey is resolved via join for permission checks; not serialized.
	ProjectKey string `json:"-"`
}

type Label struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description string    `json:"description,omitempty"`
	// IssueCount is how many issues carry the label — the number that says whether
	// a label is load-bearing or a typo somebody made once.
	IssueCount int `json:"issue_count"`
	// IsGlobal labels belong to no project and are offered in every one; editing
	// them is an install-wide change, so it takes admin rather than project:manage.
	IsGlobal bool `json:"is_global"`
}

type LinkedCommit struct {
	SHA       string    `json:"sha"`
	Repo      string    `json:"repo"`
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	URL       string    `json:"url"`
	Verb      string    `json:"verb"`
	CreatedAt time.Time `json:"created_at"`
}

// BoardColumn maps a Kanban column to one or more workflow statuses; dropping
// a card transitions to Statuses[0].
type BoardColumn struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Statuses []string  `json:"statuses"`
	WipLimit *int      `json:"wip_limit,omitempty"`
	Position int       `json:"position"`
}

// Board is a project's configurable Kanban view.
type Board struct {
	ID         uuid.UUID     `json:"id"`
	ProjectKey string        `json:"project_key"`
	Name       string        `json:"name"`
	Swimlane   string        `json:"swimlane"` // none | assignee | priority
	Columns    []BoardColumn `json:"columns"`
	CreatedAt  time.Time     `json:"created_at"`
}

// AutomationRule is a trigger→actions rule evaluated against issue events.
// Trigger: {"event": "issue.created"|"*", "conditions": {type?, severity?,
// priority?, label?, component?, source?}} — all set conditions must match.
// Actions: ordered [{"kind": "...", "value": "..."}].
type AutomationRule struct {
	ID         uuid.UUID       `json:"id"`
	ProjectKey string          `json:"project_key,omitempty"` // empty = all projects
	Name       string          `json:"name"`
	IsActive   bool            `json:"is_active"`
	Priority   int             `json:"priority"` // lower runs first
	Trigger    json.RawMessage `json:"trigger"`
	Actions    json.RawMessage `json:"actions"`
	CreatedAt  time.Time       `json:"created_at"`
}

// AutomationRun is one evaluation record of a rule against an issue.
type AutomationRun struct {
	ID       uuid.UUID       `json:"id"`
	RuleID   uuid.UUID       `json:"rule_id"`
	RuleName string          `json:"rule_name,omitempty"`
	IssueKey string          `json:"issue_key,omitempty"`
	Status   string          `json:"status"` // matched | error
	Log      json.RawMessage `json:"log"`
	RanAt    time.Time       `json:"ran_at"`
}

// Webhook is an outbound HTTP subscription to domain events.
type Webhook struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"project_key,omitempty"` // empty = all projects
	URL        string    `json:"url"`
	HasSecret  bool      `json:"has_secret"`
	Events     []string  `json:"events"` // empty = all events
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
}

// WebhookDelivery is one delivery attempt record for a webhook.
type WebhookDelivery struct {
	ID           uuid.UUID       `json:"id"`
	EventType    string          `json:"event_type"`
	Status       string          `json:"status"` // pending | success | failed | dead
	ResponseCode *int            `json:"response_code,omitempty"`
	Attempt      int             `json:"attempt"`
	Payload      json.RawMessage `json:"-"` // used for redelivery, not serialized
	WebhookID    uuid.UUID       `json:"-"`
	CreatedAt    time.Time       `json:"created_at"`
}

// IssueReference is an incidental mention of one issue in another's prose, derived
// from the text rather than created deliberately. Distinct from IssueRelation, which
// is a typed link somebody chose to make.
type IssueReference struct {
	IssueID   uuid.UUID   `json:"issue_id"`
	IssueKey  string      `json:"issue_key"`
	Title     string      `json:"title"`
	Status    IssueStatus `json:"status"`
	InComment bool        `json:"in_comment"`
	CreatedAt time.Time   `json:"created_at"`
}

// SimilarIssue is a duplicate candidate surfaced while an issue is being written.
type SimilarIssue struct {
	IssueKey  string      `json:"issue_key"`
	Title     string      `json:"title"`
	Status    IssueStatus `json:"status"`
	Type      IssueType   `json:"type"`
	Score     float64     `json:"score"`
	CreatedAt time.Time   `json:"created_at"`
}

// Notification is one entry in a user's in-app inbox.
type Notification struct {
	ID          uuid.UUID  `json:"id"`
	EventType   string     `json:"event_type"`
	IssueKey    string     `json:"issue_key"`
	IssueTitle  string     `json:"issue_title"`
	IssueStatus string     `json:"issue_status"`
	Actor       *User      `json:"actor,omitempty"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Reaction is one emoji on a comment or on the issue body, aggregated across everyone
// who left it. CommentID is nil when the target is the issue body itself.
type Reaction struct {
	CommentID *uuid.UUID `json:"comment_id,omitempty"`
	Emoji     string     `json:"emoji"`
	Count     int        `json:"count"`
	Users     []string   `json:"users"`
	// Mine is whether the requesting user is among them, so the toggle renders right.
	Mine bool `json:"mine"`
}

// AuditEntry is one privileged action, as read back from the log.
type AuditEntry struct {
	ID          uuid.UUID       `json:"id"`
	ActorName   string          `json:"actor_name,omitempty"`
	ActorEmail  string          `json:"actor_email"`
	Action      string          `json:"action"`
	TargetType  string          `json:"target_type"`
	TargetID    string          `json:"target_id,omitempty"`
	TargetLabel string          `json:"target_label,omitempty"`
	Details     json.RawMessage `json:"details,omitempty"`
	IP          string          `json:"ip,omitempty"`
	ViaToken    bool            `json:"via_token"`
	CreatedAt   time.Time       `json:"created_at"`
}

// SavedSearch is a named filter-grammar string. Personal by default; a shared one
// belongs to a project and is visible to every member of it.
type SavedSearch struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Query       string    `json:"query"`
	Description string    `json:"description,omitempty"`
	Sort        string    `json:"sort,omitempty"`
	IsShared    bool      `json:"is_shared"`
	IsDefault   bool      `json:"is_default"`
	ProjectKey  string    `json:"project_key,omitempty"`
	Position    int       `json:"position"`
	// Author is who created a shared view — worth knowing before you rely on it.
	Author    *User     `json:"author,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// IssueRelation is one edge of the relation graph as seen from a given issue.
// Kind is the stored canonical kind; Direction says whether this issue is the
// from-side ("out") or to-side ("in") — the UI renders the inverse label for "in".
type IssueRelation struct {
	ID        uuid.UUID   `json:"id"`
	Kind      string      `json:"kind"`
	Direction string      `json:"direction"` // out | in
	IssueKey  string      `json:"issue_key"` // the other issue
	Title     string      `json:"title"`
	Status    IssueStatus `json:"status"`
}

// Attachment is file metadata; bytes live in the configured storage backend
// (local disk) under ObjectKey.
type Attachment struct {
	ID          uuid.UUID  `json:"id"`
	IssueID     *uuid.UUID `json:"issue_id,omitempty"`
	Uploader    *User      `json:"uploader,omitempty"`
	Filename    string     `json:"filename"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	ObjectKey   string     `json:"-"` // storage-internal, never exposed
	ProjectKey  string     `json:"-"` // for permission checks, resolved via join
	CreatedAt   time.Time  `json:"created_at"`
}

// SearchHit is one global-search result row (Postgres FTS over issues + comments).
type SearchHit struct {
	IssueKey   string  `json:"issue_key"`
	ProjectKey string  `json:"project_key"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Type       string  `json:"type"`
	Snippet    string  `json:"snippet"` // plain text with «…» match marks
	Rank       float32 `json:"rank"`
	MatchedIn  string  `json:"matched_in"` // issue | comment
}

type Activity struct {
	ID         uuid.UUID       `json:"id"`
	IssueID    *uuid.UUID      `json:"issue_id,omitempty"`
	IssueKey   string          `json:"issue_key,omitempty"`
	Actor      *User           `json:"actor,omitempty"`
	Verb       string          `json:"verb"`
	EntityType string          `json:"entity_type"`
	Changes    json.RawMessage `json:"changes,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
}
