package service

import (
	"testing"
	"time"

	"github.com/omni/bugtracker/internal/domain"
)

func TestParseFilterDueTerms(t *testing.T) {
	tests := []struct {
		term  string
		check func(IssueFilter) bool
	}{
		{"due:overdue", func(f IssueFilter) bool { return f.DueOverdue }},
		{"due:late", func(f IssueFilter) bool { return f.DueOverdue }},
		{"due:none", func(f IssueFilter) bool { return f.DueNone }},
		{"due:any", func(f IssueFilter) bool { return f.DueAny }},
		{"due:today", func(f IssueFilter) bool { return f.DueBefore != nil }},
		{"due:week", func(f IssueFilter) bool { return f.DueBefore != nil }},
		{"due:<7d", func(f IssueFilter) bool { return f.DueBefore != nil && f.DueAfter == nil }},
		{"due:>2w", func(f IssueFilter) bool { return f.DueAfter != nil && f.DueBefore == nil }},
		{"due:<48h", func(f IssueFilter) bool { return f.DueBefore != nil }},
	}
	for _, tc := range tests {
		f, bad := ParseFilter("BUG", tc.term, "")
		if len(bad) != 0 {
			t.Errorf("%s: unexpected validation errors: %v", tc.term, bad)
			continue
		}
		if !tc.check(f) {
			t.Errorf("%s: did not set the expected predicate (%+v)", tc.term, f)
		}
	}
}

// A relative window has to land in the right direction, or `due:<7d` quietly lists
// everything that is *not* due soon — a filter that looks like it works.
func TestParseFilterDueWindowDirection(t *testing.T) {
	before := time.Now().Add(7 * 24 * time.Hour)
	f, _ := ParseFilter("BUG", "due:<7d", "")
	if f.DueBefore == nil {
		t.Fatal("due:<7d must set DueBefore")
	}
	if d := f.DueBefore.Sub(before); d > time.Minute || d < -time.Minute {
		t.Errorf("due:<7d landed %v away from now+7d", d)
	}
	f, _ = ParseFilter("BUG", "due:>7d", "")
	if f.DueAfter == nil || f.DueBefore != nil {
		t.Errorf("due:>7d must set DueAfter and leave DueBefore unset, got %+v", f)
	}
}

func TestParseFilterDueRejectsGarbage(t *testing.T) {
	for _, term := range []string{"due:soon", "due:<0d", "due:7d", "due:<7y", "due:<-3d"} {
		_, bad := ParseFilter("BUG", term, "")
		if bad["due"] == "" {
			t.Errorf("%s should have been rejected", term)
		}
	}
}

func TestParseFilterSLATerms(t *testing.T) {
	for term, want := range map[string]string{
		"sla:breached": domain.SLABreached,
		"sla:at-risk":  domain.SLAAtRisk,
		"sla:at_risk":  domain.SLAAtRisk,
		"sla:ok":       domain.SLAOK,
		"sla:met":      domain.SLAMet,
	} {
		f, bad := ParseFilter("BUG", term, "")
		if len(bad) != 0 {
			t.Errorf("%s: unexpected validation errors: %v", term, bad)
			continue
		}
		if len(f.SLAStates) != 1 || f.SLAStates[0] != want {
			t.Errorf("%s: got %v, want [%s]", term, f.SLAStates, want)
		}
	}
	f, bad := ParseFilter("BUG", "sla:none", "")
	if len(bad) != 0 || !f.SLANone || len(f.SLAStates) != 0 {
		t.Errorf("sla:none should set SLANone alone, got %+v (%v)", f, bad)
	}
	if _, bad := ParseFilter("BUG", "sla:probably-fine", ""); bad["sla"] == "" {
		t.Error("an unknown sla value should be rejected, not ignored")
	}
}

// The SQL filter and the Go pill both collapse two states into one. If they disagree,
// `sla:breached` returns rows the list does not render as breached.
func TestWorseSLAStateOrdering(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{domain.SLAMet, domain.SLAOK, domain.SLAOK},
		{domain.SLAOK, domain.SLAAtRisk, domain.SLAAtRisk},
		{domain.SLAAtRisk, domain.SLABreached, domain.SLABreached},
		{domain.SLABreached, domain.SLAMet, domain.SLABreached},
		{domain.SLAMet, domain.SLAMet, domain.SLAMet},
	}
	for _, c := range cases {
		if got := domain.WorseSLAState(c.a, c.b); got != c.want {
			t.Errorf("WorseSLAState(%q,%q) = %q, want %q", c.a, c.b, got, c.want)
		}
		// Order of arguments must not matter — the SQL CASE has no notion of one side.
		if got := domain.WorseSLAState(c.b, c.a); got != c.want {
			t.Errorf("WorseSLAState(%q,%q) = %q, want %q (not symmetric)", c.b, c.a, got, c.want)
		}
	}
}

func TestParseDueAt(t *testing.T) {
	if _, clear, err := parseDueAt(""); err != nil || !clear {
		t.Errorf(`parseDueAt("") should mean "clear", got clear=%v err=%v`, clear, err)
	}
	got, clear, err := parseDueAt("2026-03-05")
	if err != nil || clear || got == nil {
		t.Fatalf("bare date should parse: %v %v %v", got, clear, err)
	}
	// End of day, not midnight: "due 5 March" delivered at 4pm on the 5th is on time.
	if got.Hour() != 23 || got.Minute() != 59 {
		t.Errorf("bare date should land at end of day, got %s", got)
	}
	if _, _, err := parseDueAt("next tuesday"); err == nil {
		t.Error("unparseable input should be rejected")
	}
}
