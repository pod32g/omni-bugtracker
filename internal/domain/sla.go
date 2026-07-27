package domain

import (
	"time"

	"github.com/google/uuid"
)

// SLA states, worst last. A "met" target was hit inside its budget; "breached" covers
// both a budget that has run out and one that was overrun before the work was done —
// an issue answered two days late did breach, and saying otherwise the moment somebody
// finally replies would make the whole measure useless.
const (
	SLAMet      = "met"
	SLAOK       = "ok"
	SLAAtRisk   = "at_risk"
	SLABreached = "breached"
)

// SLAPolicy is one project's commitment for a slice of its issues. A nil Severity or
// Type means "any" — a project can set one catch-all and override just critical bugs.
type SLAPolicy struct {
	ID                uuid.UUID  `json:"id"`
	ProjectKey        string     `json:"project_key"`
	Severity          *Severity  `json:"severity,omitempty"`
	Type              *IssueType `json:"type,omitempty"`
	ResponseMinutes   int        `json:"response_minutes"`
	ResolutionMinutes int        `json:"resolution_minutes"`
	IsActive          bool       `json:"is_active"`
	CreatedAt         time.Time  `json:"created_at"`
}

// IssueSLA is an issue's standing against whichever policy applies to it. Absent
// (nil on the issue) when the project has no matching policy — most projects will
// never define one, and every issue would otherwise carry a meaningless "ok".
type IssueSLA struct {
	ResponseDue     *time.Time `json:"response_due,omitempty"`
	ResolutionDue   *time.Time `json:"resolution_due,omitempty"`
	ResponseState   string     `json:"response_state"`
	ResolutionState string     `json:"resolution_state"`
	// State is the worse of the two, which is what a single pill in a list renders.
	State string `json:"state"`
}

// WorseSLAState returns the more urgent of two states.
func WorseSLAState(a, b string) string {
	if slaRank(a) >= slaRank(b) {
		return a
	}
	return b
}

func slaRank(s string) int {
	switch s {
	case SLABreached:
		return 3
	case SLAAtRisk:
		return 2
	case SLAOK:
		return 1
	default: // met
		return 0
	}
}
