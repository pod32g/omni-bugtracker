package service

import (
	"testing"

	"github.com/omni/bugtracker/internal/domain"
)

// ptr lives in settings_test.go.

func TestIssueEnumProblemsAcceptsValidValues(t *testing.T) {
	got := issueEnumProblems(
		ptr(domain.TypeBug), ptr(domain.SeverityCritical), ptr(domain.P0), ptr(domain.StatusInProgress))
	if len(got) != 0 {
		t.Fatalf("valid values rejected: %v", got)
	}
}

// Absence is not a validation failure — update treats a nil pointer as "leave it".
func TestIssueEnumProblemsIgnoresAbsentValues(t *testing.T) {
	if got := issueEnumProblems(nil, nil, nil, nil); len(got) != 0 {
		t.Fatalf("nil pointers rejected: %v", got)
	}
	empty := issueEnumProblems(
		ptr(domain.IssueType("")), ptr(domain.Severity("")), ptr(domain.Priority("")), ptr(domain.IssueStatus("")))
	if len(empty) != 0 {
		t.Fatalf("empty strings rejected: %v", empty)
	}
}

// The reported case: {"type":"epic"} reached the Postgres enum and returned 500.
func TestIssueEnumProblemsRejectsUnknownValues(t *testing.T) {
	got := issueEnumProblems(
		ptr(domain.IssueType("epic")),
		ptr(domain.Severity("blocker")),
		ptr(domain.Priority("p9")),
		ptr(domain.IssueStatus("done")),
	)
	for _, field := range []string{"type", "severity", "priority", "status"} {
		if got[field] == "" {
			t.Errorf("%s: unknown value accepted", field)
		}
	}
}

// A SQL-shaped value must be refused like any other unknown one — it never reaches a
// query, but the 422 is what stops a client retrying forever against a 500.
func TestIssueEnumProblemsRejectsInjectionShapedValues(t *testing.T) {
	got := issueEnumProblems(ptr(domain.IssueType("bug'; DROP TABLE issues;--")), nil, nil, nil)
	if got["type"] == "" {
		t.Fatal("injection-shaped type accepted")
	}
}
