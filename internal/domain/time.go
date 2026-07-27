package domain

import (
	"time"

	"github.com/google/uuid"
)

// TimeEntry is one logged stretch of work. Corrections are new entries, not edits to
// a running total, so the ledger explains how a number got where it is.
type TimeEntry struct {
	ID      uuid.UUID `json:"id"`
	IssueID uuid.UUID `json:"issue_id"`
	User    *User     `json:"user,omitempty"`
	Minutes int       `json:"minutes"`
	// SpentOn is the day the work happened, which is often not the day it was logged.
	SpentOn   string    `json:"spent_on"` // YYYY-MM-DD
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// EffortRollup is estimate-vs-spent for a set of issues — a milestone, a release, a
// component, or one person's open work.
type EffortRollup struct {
	// Issues is how many issues the rollup covers; Estimated is how many of them
	// actually carry an estimate. Reporting only the total would let "2 of 40 issues
	// estimated" read exactly like a plan.
	Issues          int `json:"issues"`
	Estimated       int `json:"estimated"`
	EstimateMinutes int `json:"estimate_minutes"`
	SpentMinutes    int `json:"spent_minutes"`
	// RemainingMinutes counts only unfinished issues — an over-run that has already
	// shipped is not work still to do.
	RemainingMinutes int `json:"remaining_minutes"`
}

// OverBudget reports whether spent time has passed the estimate. False when nothing is
// estimated: an unestimated issue cannot be over budget, only unplanned.
func (e EffortRollup) OverBudget() bool {
	return e.EstimateMinutes > 0 && e.SpentMinutes > e.EstimateMinutes
}
