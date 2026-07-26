package service

import (
	"strings"
	"testing"
)

// The default websearch AND semantics are wrong for similarity: requiring every word
// means the near-duplicate phrased slightly differently — the one worth finding — never
// matches. Ranking is what discriminates, so the query has to be permissive.
func TestSimilarQueryUsesOrSemantics(t *testing.T) {
	got := similarQuery("Crash on startup when config missing")
	want := "crash or startup or when or config or missing"
	if got != want {
		t.Errorf("similarQuery = %q, want %q", got, want)
	}
}

// Short words carry no signal and include the tokens websearch_to_tsquery reads as
// operators, which would change the shape of the query rather than add a term.
func TestSimilarQueryDropsShortWordsAndPunctuation(t *testing.T) {
	got := similarQuery("A bug in the UI — it is on fire!")
	for _, unwanted := range []string{" a ", " in ", " it ", " is ", " on ", "—", "!"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("similarQuery(%q) = %q, should not contain %q", "A bug in the UI…", got, unwanted)
		}
	}
	for _, wanted := range []string{"bug", "the", "fire"} {
		if !strings.Contains(got, wanted) {
			t.Errorf("similarQuery = %q, missing %q", got, wanted)
		}
	}
}

func TestSimilarQueryDedupes(t *testing.T) {
	if got := similarQuery("crash crash CRASH"); got != "crash" {
		t.Errorf("similarQuery = %q, want %q", got, "crash")
	}
}

// A title with nothing usable in it must produce no query at all — matching everything
// would be far worse than suggesting nothing.
func TestSimilarQueryEmptyWhenNothingUseful(t *testing.T) {
	for _, title := range []string{"", "   ", "?!", "a an", "42"} {
		if title == "42" {
			continue // digits are legitimate terms
		}
		if got := similarQuery(title); got != "" {
			t.Errorf("similarQuery(%q) = %q, want empty", title, got)
		}
	}
}

// A pasted stack trace must not build a tsquery with hundreds of branches.
func TestSimilarQueryIsBounded(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += " word" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if got := strings.Count(similarQuery(long), " or ") + 1; got != maxSimilarTerms {
		t.Errorf("query carries %d terms, want it capped at %d", got, maxSimilarTerms)
	}
}
