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
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
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
