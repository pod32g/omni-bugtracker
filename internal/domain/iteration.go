package domain

import (
	"time"

	"github.com/google/uuid"
)

// Iteration states. Exactly one iteration per project may be active at a time —
// enforced by a partial unique index, because "the current iteration" has to resolve
// to one thing or `iteration:current` means nothing.
const (
	IterationPlanned   = "planned"
	IterationActive    = "active"
	IterationCompleted = "completed"
)

// Iteration is a time-boxed slice of work.
type Iteration struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"project_key"`
	Name       string    `json:"name"`
	StartsOn   string    `json:"starts_on"` // YYYY-MM-DD
	EndsOn     string    `json:"ends_on"`   // YYYY-MM-DD
	State      string    `json:"state"`
	Goal       string    `json:"goal,omitempty"`
	// Effort and the issue counts describe the committed scope as it stands now.
	Issues      int          `json:"issues"`
	DoneIssues  int          `json:"done_issues"`
	Effort      EffortRollup `json:"effort"`
	DoneMinutes int          `json:"done_minutes"`
	CreatedAt   time.Time    `json:"created_at"`
}

// BurndownPoint is one stored day of an iteration. Read back, never recomputed —
// see the migration.
type BurndownPoint struct {
	Date             string `json:"date"` // YYYY-MM-DD
	RemainingIssues  int    `json:"remaining_issues"`
	RemainingMinutes int    `json:"remaining_minutes"`
	TotalIssues      int    `json:"total_issues"`
	TotalMinutes     int    `json:"total_minutes"`
}

// Velocity is what one finished iteration actually delivered.
type Velocity struct {
	IterationID    uuid.UUID `json:"iteration_id"`
	Name           string    `json:"name"`
	EndsOn         string    `json:"ends_on"`
	DoneIssues     int       `json:"done_issues"`
	DoneMinutes    int       `json:"done_minutes"`
	PlannedIssues  int       `json:"planned_issues"`
	PlannedMinutes int       `json:"planned_minutes"`
}

// ValidIterationState guards a value about to reach the Postgres enum column, where
// an unknown value fails the whole query rather than matching nothing.
func ValidIterationState(s string) bool {
	switch s {
	case IterationPlanned, IterationActive, IterationCompleted:
		return true
	}
	return false
}

// AverageVelocity is the rolling mean over the most recent finished iterations, which
// is the number planning actually needs — a single iteration is noise.
//
// Returns (issues, minutes, sampled). Sampled is reported rather than hidden: an
// average over one iteration is not an average, and the caller has to be able to say so.
func AverageVelocity(history []Velocity, window int) (float64, float64, int) {
	if window <= 0 || len(history) == 0 {
		return 0, 0, 0
	}
	if window > len(history) {
		window = len(history)
	}
	var issues, minutes int
	for _, v := range history[:window] {
		issues += v.DoneIssues
		minutes += v.DoneMinutes
	}
	return float64(issues) / float64(window), float64(minutes) / float64(window), window
}
