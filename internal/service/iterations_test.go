package service

import (
	"testing"

	"github.com/omni/bugtracker/internal/domain"
)

func TestParseFilterIterationTerms(t *testing.T) {
	// `current` and `next` need the database, so ParseFilter records them verbatim
	// and the handler resolves them — the parser stays pure and unit-testable.
	for _, term := range []string{"iteration:current", "sprint:current"} {
		f, bad := ParseFilter("BUG", term, "")
		if len(bad) != 0 {
			t.Errorf("%s: unexpected validation errors: %v", term, bad)
		}
		if f.IterationRef != "current" {
			t.Errorf("%s: IterationRef = %q, want current", term, f.IterationRef)
		}
	}
	f, _ := ParseFilter("BUG", `iteration:"2026-W31"`, "")
	if f.IterationRef != "2026-W31" {
		t.Errorf("a quoted name should survive unquoting, got %q", f.IterationRef)
	}
	f, _ = ParseFilter("BUG", "iteration:none", "")
	if !f.IterationNone || f.IterationRef != "" {
		t.Errorf("iteration:none should set IterationNone alone, got %+v", f)
	}
}

// AverageVelocity has to report its sample size. An average over one iteration is a
// single fortnight wearing a trend's clothes, and the planning page has to be able to
// say so rather than present it as settled.
func TestAverageVelocityReportsSampleSize(t *testing.T) {
	history := []domain.Velocity{
		{DoneIssues: 10, DoneMinutes: 600},
		{DoneIssues: 6, DoneMinutes: 400},
	}
	issues, minutes, sampled := domain.AverageVelocity(history, 3)
	if sampled != 2 {
		t.Errorf("sampled = %d, want 2 — the window is larger than the history", sampled)
	}
	if issues != 8 {
		t.Errorf("average issues = %v, want 8", issues)
	}
	if minutes != 500 {
		t.Errorf("average minutes = %v, want 500", minutes)
	}

	// The window truncates: only the most recent iterations count.
	issues, _, sampled = domain.AverageVelocity(history, 1)
	if sampled != 1 || issues != 10 {
		t.Errorf("window of 1 should take the newest only, got %v over %d", issues, sampled)
	}

	if _, _, sampled := domain.AverageVelocity(nil, 3); sampled != 0 {
		t.Errorf("no history means no average, got a sample of %d", sampled)
	}
}

func TestParseIterationDates(t *testing.T) {
	problems := map[string]string{}
	start, end := parseIterationDates("2026-08-03", "2026-08-14", problems)
	if len(problems) != 0 || start != "2026-08-03" || end != "2026-08-14" {
		t.Errorf("a normal fortnight should parse cleanly: %v %q %q", problems, start, end)
	}

	problems = map[string]string{}
	parseIterationDates("2026-08-14", "2026-08-03", problems)
	if problems["ends_on"] == "" {
		t.Error("an iteration that ends before it starts should be rejected")
	}

	// A mistyped year is the common way this goes wrong, and it produces a burndown
	// nobody can read rather than an obvious error.
	problems = map[string]string{}
	parseIterationDates("2026-08-03", "2027-08-03", problems)
	if problems["ends_on"] == "" {
		t.Error("an iteration longer than a quarter should be rejected")
	}
}
