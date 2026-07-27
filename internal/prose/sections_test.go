package prose

import (
	"strings"
	"testing"
)

const bugReport = `## Steps to reproduce

1. Open the app
2. Click sign in

## Expected

It signs in.

## Actual

<!-- what happened instead -->

## Notes
`

func TestSectionsSplitsOnHeadings(t *testing.T) {
	got := Sections(bugReport)
	if !strings.Contains(got["steps to reproduce"], "Click sign in") {
		t.Errorf("steps section = %q", got["steps to reproduce"])
	}
	if got["expected"] != "It signs in." {
		t.Errorf("expected section = %q", got["expected"])
	}
	if _, ok := got["notes"]; !ok {
		t.Error("a trailing heading with no content should still be recorded")
	}
}

// A heading inside a code fence is not a heading. Shell examples routinely contain
// `# comments`, and treating them as sections would let a template pass on a snippet.
func TestSectionsIgnoresFencedHeadings(t *testing.T) {
	body := "## Real\n\ncontent\n\n```sh\n## Fake\necho hi\n```\n"
	got := Sections(body)
	if _, ok := got["fake"]; ok {
		t.Error("a heading inside a code fence must not count as a section")
	}
	if !strings.Contains(got["real"], "content") {
		t.Errorf("the real section lost its content: %q", got["real"])
	}
}

func TestMissingSections(t *testing.T) {
	required := []string{"Steps to reproduce", "Expected", "Actual", "Impact"}
	missing := MissingSections(bugReport, required)

	// "Actual" holds only an HTML comment — the template's own prompt, left in place.
	// Accepting that would defeat the point of requiring the section at all.
	want := map[string]bool{"Actual": true, "Impact": true}
	if len(missing) != len(want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
	for _, m := range missing {
		if !want[m] {
			t.Errorf("did not expect %q to be missing", m)
		}
	}

	// Matching is case-insensitive: rejecting an issue over the capitalisation of a
	// heading is how a template gets abandoned.
	if got := MissingSections(bugReport, []string{"STEPS TO REPRODUCE"}); len(got) != 0 {
		t.Errorf("case should not matter, got %v", got)
	}
	if got := MissingSections(bugReport, nil); got != nil {
		t.Errorf("no requirements means nothing missing, got %v", got)
	}
}

// An unticked checklist is a prompt, not an answer.
func TestMissingSectionsUntickedChecklistIsNotAnAnswer(t *testing.T) {
	body := "## Definition of done\n\n- [ ] tests\n- [ ] docs\n"
	if got := MissingSections(body, []string{"Definition of done"}); len(got) != 1 {
		t.Errorf("an entirely unticked checklist should not satisfy the section, got %v", got)
	}
	ticked := "## Definition of done\n\n- [x] tests\n- [ ] docs\n"
	if got := MissingSections(ticked, []string{"Definition of done"}); len(got) != 0 {
		t.Errorf("a partly ticked checklist is an answer, got %v", got)
	}
}

func TestChecklist(t *testing.T) {
	body := "- [x] one\n- [ ] two\n* [X] three\n+ [ ] four\nnot an item\n"
	done, total := Checklist(body)
	if total != 4 || done != 2 {
		t.Errorf("got %d/%d, want 2/4 — all three bullet markers count", done, total)
	}

	// Markdown shown inside a fence is documentation, not somebody's progress.
	fenced := "- [x] real\n\n```md\n- [ ] example\n- [x] example\n```\n"
	done, total = Checklist(fenced)
	if total != 1 || done != 1 {
		t.Errorf("got %d/%d, want 1/1 — fenced items must not count", done, total)
	}

	if done, total := Checklist("no checklist here"); total != 0 || done != 0 {
		t.Errorf("got %d/%d, want 0/0", done, total)
	}
}

// A template's required sections are supposed to be empty in the template — they are
// the prompts. Checking answered-ness there would reject every template that works.
func TestMissingHeadingsIgnoresEmptiness(t *testing.T) {
	template := "## Steps to reproduce\n\n<!-- what you did -->\n\n## Expected\n\n## Actual\n"
	required := []string{"Steps to reproduce", "Expected", "Actual"}

	if got := MissingHeadings(template, required); len(got) != 0 {
		t.Errorf("an empty heading is present for MissingHeadings, got %v", got)
	}
	// The same body submitted as an issue answers none of them.
	if got := MissingSections(template, required); len(got) != 3 {
		t.Errorf("an unfilled template answers nothing, got %v", got)
	}
	if got := MissingHeadings(template, []string{"Impact"}); len(got) != 1 {
		t.Errorf("a heading that is not in the body is still missing, got %v", got)
	}
}
