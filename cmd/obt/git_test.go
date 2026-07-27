package main

import "testing"

func TestIssueKeyFromBranch(t *testing.T) {
	cases := map[string]string{
		"bug-42-fix-paging":       "BUG-42",
		"BUG-42":                  "BUG-42",
		"feature/BUG-42_paging":   "BUG-42",
		"pod32g/bug-7-retry-loop": "BUG-7",
		"fix/umaos-471":           "UMAOS-471",
		"main":                    "",
		// A valid key shape even though it was probably meant as a release branch —
		// RELEASE-2026 is a legitimate key. The CLI announces what it inferred rather
		// than trying to out-guess the grammar.
		"release-2026": "RELEASE-2026",
		"":             "",
	}
	for branch, want := range cases {
		if got := IssueKeyFromBranch(branch); got != want {
			t.Errorf("IssueKeyFromBranch(%q) = %q, want %q", branch, got, want)
		}
	}
}

// A branch with no key-shaped segment must return "" rather than a guess: acting on
// whatever issue happens to be numbered like today's date is worse than asking.
func TestIssueKeyFromBranchDoesNotGuess(t *testing.T) {
	for _, branch := range []string{"main", "develop", "wip", "hotfix", "refactor/the-parser"} {
		if got := IssueKeyFromBranch(branch); got != "" {
			t.Errorf("branch %q should name no issue, got %q", branch, got)
		}
	}
}

func TestLooksLikeKey(t *testing.T) {
	for _, s := range []string{"BUG-42", "bug-1", "UMAOS-471"} {
		if !looksLikeKey(s) {
			t.Errorf("%q should look like a key", s)
		}
	}
	for _, s := range []string{"in_progress", "resolved", "90m", "-", "BUG-", "-42", ""} {
		if looksLikeKey(s) {
			t.Errorf("%q should not look like a key — it would swallow the next argument", s)
		}
	}
}
