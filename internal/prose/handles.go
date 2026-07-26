package prose

import (
	"regexp"
	"strings"
)

// handle matches an @mention. The character class covers both things a person can be
// addressed by — a display name like @pod32g and an email local-part like @jane.doe —
// so resolution can try each without the parser needing to know which it got.
//
// A leading boundary is required so an email address in prose ("mail jane@example.com")
// does not read as a mention of @example.
var handle = regexp.MustCompile(`(^|[^\w@.-])@([A-Za-z0-9][A-Za-z0-9._-]{0,63})`)

// ParseHandles returns the distinct @handles in markdown prose, lowercased, in order of
// first appearance. Code spans and blocks are ignored, so a pasted shell line does not
// mention anybody.
//
// Handles are returned unresolved: whether @jane names a user is a question for the
// database, and a handle that names nobody simply produces no mention.
func ParseHandles(text string) []string {
	var out []string
	seen := map[string]bool{}

	for _, m := range handle.FindAllStringSubmatch(stripCode(text), -1) {
		// Trailing punctuation is part of the sentence, not the name: "@claude,"
		// and "@claude." both mean @claude.
		h := strings.ToLower(strings.TrimRight(m[2], "._-"))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}
