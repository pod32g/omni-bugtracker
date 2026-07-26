package service

import (
	"strings"
	"testing"

	"github.com/omni/bugtracker/internal/domain"
)

func sev(s domain.Severity) *domain.Severity { return &s }

func issue(key string, t domain.IssueType, title string, s *domain.Severity, labels ...string) domain.Issue {
	return domain.Issue{Key: key, Type: t, Title: title, Severity: s, Labels: labels}
}

func TestComposeReleaseNotesGroupsByType(t *testing.T) {
	notes := ComposeReleaseNotes(
		domain.Release{Version: "2.1.0", Name: "Paging", GitTag: "v2.1.0"},
		[]domain.Issue{
			issue("BUG-1", domain.TypeBug, "Crash on startup", sev(domain.SeverityHigh)),
			issue("BUG-2", domain.TypeFeature, "Add export", nil),
			issue("BUG-3", domain.TypeImprovement, "Faster board", nil),
			issue("BUG-4", domain.TypeTask, "Bump deps", nil),
		},
	)

	for _, want := range []string{
		"## 2.1.0 — Paging", "Tag `v2.1.0`.",
		"### Features", "- Add export ([BUG-2](/issues/BUG-2))",
		"### Improvements", "- Faster board ([BUG-3](/issues/BUG-3))", "- Bump deps ([BUG-4](/issues/BUG-4))",
		"### Fixes", "- Crash on startup ([BUG-1](/issues/BUG-1))",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}

	// Section order is the reader's order, not the database's.
	if strings.Index(notes, "### Features") > strings.Index(notes, "### Fixes") {
		t.Error("Features should come before Fixes")
	}
}

// The worst thing in a section is the thing most worth reading first.
func TestComposeReleaseNotesOrdersBySeverity(t *testing.T) {
	notes := ComposeReleaseNotes(domain.Release{Version: "1.0"}, []domain.Issue{
		issue("BUG-1", domain.TypeBug, "Cosmetic", sev(domain.SeverityLow)),
		issue("BUG-2", domain.TypeBug, "Data loss", sev(domain.SeverityCritical)),
		issue("BUG-3", domain.TypeBug, "Unclassified", nil),
	})
	crit, low, none := strings.Index(notes, "Data loss"), strings.Index(notes, "Cosmetic"), strings.Index(notes, "Unclassified")
	if !(crit < low && low < none) {
		t.Errorf("want critical < low < unclassified, got %d/%d/%d:\n%s", crit, low, none, notes)
	}
}

func TestComposeReleaseNotesHonoursExcludeLabel(t *testing.T) {
	notes := ComposeReleaseNotes(domain.Release{Version: "1.0"}, []domain.Issue{
		issue("BUG-1", domain.TypeBug, "Public fix", nil),
		issue("BUG-2", domain.TypeBug, "Internal churn", nil, "changelog:exclude"),
	})
	if strings.Contains(notes, "Internal churn") {
		t.Errorf("changelog:exclude issue leaked into the notes:\n%s", notes)
	}
	if !strings.Contains(notes, "Public fix") {
		t.Errorf("exclusion took the wrong issue:\n%s", notes)
	}
}

// An empty release should say so rather than emit a heading with nothing under it.
func TestComposeReleaseNotesEmptyRelease(t *testing.T) {
	notes := ComposeReleaseNotes(domain.Release{Version: "1.0"}, nil)
	if strings.Contains(notes, "###") {
		t.Errorf("empty release should have no sections:\n%s", notes)
	}
	if !strings.Contains(notes, "No issues are targeted") {
		t.Errorf("empty release should explain itself:\n%s", notes)
	}
}

// Every issue excluded is the same as no issues at all, and must not render an empty
// "Fixes" heading.
func TestComposeReleaseNotesAllExcluded(t *testing.T) {
	notes := ComposeReleaseNotes(domain.Release{Version: "1.0"}, []domain.Issue{
		issue("BUG-1", domain.TypeBug, "Internal", nil, "changelog:exclude"),
	})
	if strings.Contains(notes, "###") {
		t.Errorf("all-excluded release should have no sections:\n%s", notes)
	}
}

func TestComposeReleaseNotesOmitsTagWhenAbsent(t *testing.T) {
	notes := ComposeReleaseNotes(domain.Release{Version: "1.0"}, nil)
	if strings.Contains(notes, "Tag") {
		t.Errorf("no git tag should mean no tag line:\n%s", notes)
	}
}
