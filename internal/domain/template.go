package domain

import (
	"time"

	"github.com/google/uuid"
)

// IssueTemplate is a per-project, per-type starting point for a filed issue: a
// markdown body, some defaults, and optionally a set of headings that must be
// answered before the issue is accepted.
type IssueTemplate struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"project_key"`
	Name       string    `json:"name"`
	Type       IssueType `json:"type"`
	BodyMD     string    `json:"body_md"`
	// RequiredSections are heading texts that must appear in the description with
	// something under them. Matched case-insensitively — a template that rejects an
	// issue over the capitalisation of a heading is a template nobody uses twice.
	RequiredSections []string  `json:"required_sections"`
	DefaultLabels    []string  `json:"default_labels"`
	DefaultComponent string    `json:"default_component,omitempty"`
	DefaultPriority  *Priority `json:"default_priority,omitempty"`
	DefaultSeverity  *Severity `json:"default_severity,omitempty"`
	DefaultAssignee  *User     `json:"default_assignee,omitempty"`
	IsDefault        bool      `json:"is_default"`
	Position         int       `json:"position"`
	CreatedAt        time.Time `json:"created_at"`
}

// ChecklistProgress is how many `- [ ]` items in a body are ticked. Zero total means
// the body has no checklist, which is different from a checklist with nothing done.
type ChecklistProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
