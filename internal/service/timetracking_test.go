package service

import (
	"testing"
	"time"

	"github.com/omni/bugtracker/internal/domain"
)

func rollup(estimate, spent int) domain.EffortRollup {
	return domain.EffortRollup{EstimateMinutes: estimate, SpentMinutes: spent}
}

// A day is 8 hours and a week is 5 days. Counting a day as 24 hours would treble every
// estimate written down in days, and nobody would notice until a milestone rollup did.
func TestParseDurationWorkingUnits(t *testing.T) {
	cases := map[string]int{
		"90m":  90,
		"90":   90,
		"1.5h": 90,
		"2h":   120,
		"1d":   8 * 60,
		"2d":   16 * 60,
		"1w":   5 * 8 * 60,
		" 3H ": 180,
	}
	for in, want := range cases {
		got, ok := ParseDuration(in)
		if !ok || got != want {
			t.Errorf("ParseDuration(%q) = %d, %v; want %d, true", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "soon", "1h30m", "-2h", "2 hours", "h", "1y"} {
		if got, ok := ParseDuration(bad); ok {
			t.Errorf("ParseDuration(%q) should have failed, got %d", bad, got)
		}
	}
}

// FormatDuration must round-trip through ParseDuration, or the value shown on screen
// is not the value that would be stored if somebody typed it back in.
func TestFormatDurationRoundTrips(t *testing.T) {
	for _, minutes := range []int{1, 45, 60, 90, 480, 960, 2400, 4801} {
		s := FormatDuration(minutes)
		got, ok := ParseDuration(s)
		if !ok || got != minutes {
			t.Errorf("FormatDuration(%d) = %q, which parses back to %d (ok=%v)", minutes, s, got, ok)
		}
	}
}

func TestParseSpendCommands(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	got := ParseSpendCommands("/spend 90m yesterday chasing the retry loop", now)
	if len(got) != 1 {
		t.Fatalf("expected one command, got %d", len(got))
	}
	if got[0].Minutes != 90 {
		t.Errorf("minutes = %d, want 90", got[0].Minutes)
	}
	if got[0].SpentOn != "2026-07-25" {
		t.Errorf("spent_on = %q, want 2026-07-25", got[0].SpentOn)
	}
	if got[0].Note != "chasing the retry loop" {
		t.Errorf("note = %q", got[0].Note)
	}

	// Multiple lines in one comment, and an explicit date.
	multi := ParseSpendCommands("looked into it\n/spend 2h 2026-07-20 repro\n/spend 30m\n", now)
	if len(multi) != 2 {
		t.Fatalf("expected two commands, got %d: %+v", len(multi), multi)
	}
	if multi[0].SpentOn != "2026-07-20" || multi[0].Minutes != 120 {
		t.Errorf("first command wrong: %+v", multi[0])
	}
	if multi[1].SpentOn != "" || multi[1].Minutes != 30 {
		t.Errorf("second command should default to today: %+v", multi[1])
	}
}

// The command has to be a line of its own. Somebody writing "we should /spend less
// time on this" is making a joke, not logging four hours.
func TestParseSpendCommandsIgnoresMidSentence(t *testing.T) {
	for _, body := range []string{
		"we should /spend 4h less time on this",
		"see the docs on /spending",
		"/spend later",
		"/spend",
	} {
		if got := ParseSpendCommands(body, time.Now()); len(got) != 0 {
			t.Errorf("%q should not log time, got %+v", body, got)
		}
	}
}

func TestValidateEntry(t *testing.T) {
	if p := validateEntry(90, ""); len(p) != 0 {
		t.Errorf("a plain 90-minute entry should be valid, got %v", p)
	}
	if p := validateEntry(0, ""); p["minutes"] == "" {
		t.Error("zero minutes should be rejected")
	}
	if p := validateEntry(maxEntryMinutes+1, ""); p["minutes"] == "" {
		t.Error("an absurd single entry should be rejected — it is a unit typo")
	}
	future := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	if p := validateEntry(60, future); p["spent_on"] == "" {
		t.Error("logging time against next week should be rejected")
	}
	if p := validateEntry(60, "not-a-date"); p["spent_on"] == "" {
		t.Error("an unparseable date should be rejected")
	}
}

func TestParseFilterEffortTerms(t *testing.T) {
	f, bad := ParseFilter("BUG", "estimate:none spent:>8h over-budget:true", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected validation errors: %v", bad)
	}
	if !f.EstimateNone {
		t.Error("estimate:none should set EstimateNone")
	}
	if f.SpentOver == nil || *f.SpentOver != 480 {
		t.Errorf("spent:>8h should be 480 minutes, got %v", f.SpentOver)
	}
	if f.OverBudget == nil || !*f.OverBudget {
		t.Errorf("over-budget:true should be true, got %v", f.OverBudget)
	}

	// `spent:` without a comparison has no obvious meaning; guessing one would
	// silently return the wrong set.
	if _, bad := ParseFilter("BUG", "spent:8h", ""); bad["spent"] == "" {
		t.Error("spent: without a comparison should be rejected")
	}
	if _, bad := ParseFilter("BUG", "estimate:big", ""); bad["estimate"] == "" {
		t.Error("an unknown estimate value should be rejected")
	}
}

func TestEffortRollupOverBudget(t *testing.T) {
	// An unestimated set cannot be over budget — it is unplanned, which is different.
	if (rollup(0, 500)).OverBudget() {
		t.Error("no estimate means not over budget")
	}
	if (rollup(600, 500)).OverBudget() {
		t.Error("under the estimate is not over budget")
	}
	if !(rollup(400, 500)).OverBudget() {
		t.Error("spending past the estimate is over budget")
	}
}
