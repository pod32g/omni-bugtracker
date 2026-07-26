package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
)

func statusSet(t *testing.T, f IssueFilter) map[domain.IssueStatus]bool {
	t.Helper()
	set := map[domain.IssueStatus]bool{}
	for _, s := range f.Statuses {
		set[s] = true
	}
	return set
}

// `is:open` is the default filter in the UI and the definition the dashboard's
// open_issues KPI uses. Matching only the literal "open" status hid in-progress,
// blocked, in-review and reopened work from the default list.
func TestParseFilterIsOpenCoversEveryUnfinishedStatus(t *testing.T) {
	f, bad := ParseFilter("BUG", "is:open", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected validation errors: %v", bad)
	}
	got := statusSet(t, f)
	for _, want := range domain.OpenStatuses {
		if !got[want] {
			t.Errorf("is:open should match %q", want)
		}
	}
	for _, notWant := range domain.ClosedStatuses {
		if got[notWant] {
			t.Errorf("is:open must not match %q", notWant)
		}
	}
}

func TestParseFilterIsClosed(t *testing.T) {
	f, _ := ParseFilter("BUG", "is:closed", "")
	got := statusSet(t, f)
	if !got[domain.StatusResolved] || !got[domain.StatusClosed] {
		t.Fatalf("is:closed should cover resolved+closed, got %v", f.Statuses)
	}
	if got[domain.StatusOpen] {
		t.Error("is:closed must not match open")
	}
}

func TestParseFilterExactStatusStillNarrows(t *testing.T) {
	f, bad := ParseFilter("BUG", "status:blocked", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected errors: %v", bad)
	}
	if len(f.Statuses) != 1 || f.Statuses[0] != domain.StatusBlocked {
		t.Fatalf("want [blocked], got %v", f.Statuses)
	}
}

// `status:` is an exact match while `is:` is the lifecycle predicate. Collapsing
// the two left no way to ask for issues that are literally in the "open" status.
func TestParseFilterStatusOpenIsExactNotTheOpenSet(t *testing.T) {
	f, bad := ParseFilter("BUG", "status:open", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected errors: %v", bad)
	}
	if len(f.Statuses) != 1 || f.Statuses[0] != domain.StatusOpen {
		t.Fatalf("status:open must match only the open status, got %v", f.Statuses)
	}

	if g, _ := ParseFilter("BUG", "is:open", ""); len(g.Statuses) != len(domain.OpenStatuses) {
		t.Fatalf("is:open must stay the lifecycle set, got %v", g.Statuses)
	}
}

func TestParseFilterStatusClosedIsExactNotTheClosedSet(t *testing.T) {
	f, _ := ParseFilter("BUG", "status:closed", "")
	if len(f.Statuses) != 1 || f.Statuses[0] != domain.StatusClosed {
		t.Fatalf("status:closed must match only the closed status, got %v", f.Statuses)
	}
}

// `archived` is only meaningful on `is:` — it is not a status value.
func TestParseFilterStatusArchivedIsRejected(t *testing.T) {
	_, bad := ParseFilter("BUG", "status:archived", "")
	if _, ok := bad["status"]; !ok {
		t.Fatalf("status:archived should be a validation error, got %v", bad)
	}
}

func TestParseFilterDedupesOverlappingStatusTerms(t *testing.T) {
	f, _ := ParseFilter("BUG", "is:open status:blocked", "")
	seen := map[domain.IssueStatus]int{}
	for _, s := range f.Statuses {
		seen[s]++
	}
	if seen[domain.StatusBlocked] != 1 {
		t.Fatalf("blocked listed %d times: %v", seen[domain.StatusBlocked], f.Statuses)
	}
}

// Unknown enum values used to be forwarded to Postgres, where comparing them
// against an enum column fails the whole query — the caller saw a 500 for a typo.
func TestParseFilterRejectsUnknownEnumValues(t *testing.T) {
	for _, tc := range []struct{ raw, field string }{
		{"is:opne", "is"},
		{"status:wat", "status"},
		{"severity:hgih", "severity"},
		{"type:buggy", "type"},
		{"milestone:not-a-uuid", "milestone"},
		{"release:nope", "release"},
	} {
		_, bad := ParseFilter("BUG", tc.raw, "")
		if _, ok := bad[tc.field]; !ok {
			t.Errorf("%q should report a %q validation error, got %v", tc.raw, tc.field, bad)
		}
	}
}

// A label or component containing a space was split by the tokenizer, so the
// filter silently matched the wrong thing and leaked the rest into full-text.
func TestParseFilterKeepsQuotedValuesIntact(t *testing.T) {
	f, bad := ParseFilter("BUG", `label:"needs triage" component:"api gateway" crash`, "")
	if len(bad) != 0 {
		t.Fatalf("unexpected errors: %v", bad)
	}
	if f.Label != "needs triage" {
		t.Errorf("label = %q, want %q", f.Label, "needs triage")
	}
	if f.Component != "api gateway" {
		t.Errorf("component = %q, want %q", f.Component, "api gateway")
	}
	if f.Query != "crash" {
		t.Errorf("free text = %q, want %q", f.Query, "crash")
	}
}

func TestParseFilterAssigneeMe(t *testing.T) {
	me := uuid.New()
	f, bad := ParseFilter("BUG", "assignee:@me", me.String())
	if len(bad) != 0 {
		t.Fatalf("unexpected errors: %v", bad)
	}
	if f.AssigneeID == nil || *f.AssigneeID != me {
		t.Fatalf("assignee = %v, want %v", f.AssigneeID, me)
	}
}

// Failing open here showed every issue in the project instead of the caller's,
// which reads as "you have 400 issues assigned" rather than as an error.
func TestParseFilterAssigneeFailsClosed(t *testing.T) {
	f, bad := ParseFilter("BUG", "assignee:@me", "not-a-uuid")
	if _, ok := bad["assignee"]; !ok {
		t.Fatalf("unresolvable @me should be a validation error, got %v", bad)
	}
	if f.AssigneeID != nil {
		t.Error("assignee filter must not be silently dropped")
	}
}

func TestParseFilterArchivedIsOrthogonalToStatus(t *testing.T) {
	f, bad := ParseFilter("BUG", "is:archived", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected errors: %v", bad)
	}
	if !f.ShowArchived {
		t.Error("is:archived should set ShowArchived")
	}
	if len(f.Statuses) != 0 {
		t.Errorf("is:archived should not constrain status, got %v", f.Statuses)
	}
}

func TestParseFilterFreeTextPassesThrough(t *testing.T) {
	f, bad := ParseFilter("BUG", "null pointer unknown:thing", "")
	if len(bad) != 0 {
		t.Fatalf("unexpected errors: %v", bad)
	}
	if f.Query != "null pointer unknown:thing" {
		t.Errorf("query = %q", f.Query)
	}
}
