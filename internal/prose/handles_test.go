package prose

import (
	"strings"
	"testing"
)

func handles(t *testing.T, text string) string {
	t.Helper()
	return strings.Join(ParseHandles(text), ",")
}

func TestParseHandles(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"@pod32g can you look at this?", "pod32g"},
		{"cc @claude and @admin", "claude,admin"},
		{"@Claude and @claude are the same person", "claude"},
		{"jane.doe@example.com is an address, not a mention", ""},
		{"mid@word does not mention word", ""},
		{"@claude, please", "claude"},
		{"ask @jane.doe about it", "jane.doe"},
		{"(@claude) in parentheses", "claude"},
		{"an email like foo@bar.com and a real @claude", "claude"},
	} {
		if got := handles(t, tc.text); got != tc.want {
			t.Errorf("ParseHandles(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

// A shell line or a snippet containing an @ must not notify anybody.
func TestParseHandlesIgnoresCode(t *testing.T) {
	for name, text := range map[string]string{
		"inline": "run `ssh @claude` to reproduce",
		"fenced": "```\nscp file @admin:/tmp\n```",
	} {
		if got := handles(t, text); got != "" {
			t.Errorf("%s: got %q, want no mentions", name, got)
		}
	}
}
