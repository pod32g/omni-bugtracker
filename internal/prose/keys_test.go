package prose

import "testing"

func keys(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	for _, k := range ParseKeys(text) {
		out = append(out, k.String())
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// Prose needs no verb — writing the key is the reference. This is the whole difference
// from RefParser, which requires one so a commit cannot close an issue by accident.
func TestParseKeysNeedsNoVerb(t *testing.T) {
	eq(t, keys(t, "same root cause as BUG-33, see also RECORDER-7"), []string{"BUG-33", "RECORDER-7"})
}

func TestParseKeysDedupesKeepingFirstOrder(t *testing.T) {
	eq(t, keys(t, "BUG-40 then BUG-33 then BUG-40 again"), []string{"BUG-40", "BUG-33"})
}

// A pasted log or snippet must not mint references.
func TestParseKeysIgnoresCode(t *testing.T) {
	for name, text := range map[string]string{
		"inline":   "the constant is `BUG-99` in the fixture",
		"fenced":   "```\npanic: BUG-99 not found\n```",
		"tilde":    "~~~\nBUG-99\n~~~",
		"indented": "    grep BUG-99 log.txt",
	} {
		if got := keys(t, text); len(got) != 0 {
			t.Errorf("%s: got %v, want no references", name, got)
		}
	}
}

func TestParseKeysSeesProseAroundCode(t *testing.T) {
	eq(t, keys(t, "BUG-1 says `BUG-99` but BUG-2 disagrees"), []string{"BUG-1", "BUG-2"})
}

// A markdown link to an issue is already a link; counting its target would record the
// same reference twice.
func TestParseKeysIgnoresLinkTargets(t *testing.T) {
	eq(t, keys(t, "see [the paging fix](/issues/BUG-32) and BUG-33"), []string{"BUG-33"})
}

func TestParseKeysRejectsNonKeys(t *testing.T) {
	for _, text := range []string{
		"lower-42 is not a key",
		"UPPERCASE-WORD is not a key",
		"a-1 is too short a project key",
		"TOOLONGAKEY12-1 exceeds the key length",
		"BUG-0 is not a valid issue number",
	} {
		if got := keys(t, text); len(got) != 0 {
			t.Errorf("%q: got %v, want no references", text, got)
		}
	}
}
