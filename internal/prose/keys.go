// Package prose parses the things people write into issue bodies and comments —
// issue keys and @handles — into the links and mentions the tracker acts on.
//
// Deliberately separate from internal/git, which parses the same key syntax out of
// commit messages under a different rule: there a verb is required, because a commit
// naming an issue must not silently close it. Here the mention itself is the intent.
package prose

import (
	"regexp"
	"strconv"
)

// Key is a bare issue reference — project key and number, no verb.
type Key struct {
	ProjectKey string
	Number     int32
}

// String renders the canonical "BUG-42" form.
func (k Key) String() string { return k.ProjectKey + "-" + strconv.FormatInt(int64(k.Number), 10) }

// bareKey matches an issue key written in prose. Uppercase only: project keys are
// uppercase everywhere in this system, and requiring it keeps ordinary hyphenated words
// out. Keys that do not exist are filtered when they are resolved against the database,
// so a stray "RFC-2119" costs one lookup and links nothing.
var bareKey = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9})-(\d+)\b`)

// ParseKeys returns the distinct issue keys mentioned in markdown prose, in order of
// first appearance.
func ParseKeys(text string) []Key {
	prose := stripCode(text)
	seen := make(map[string]bool)
	var out []Key

	for _, m := range bareKey.FindAllStringSubmatch(prose, -1) {
		id := m[1] + "-" + m[2]
		if seen[id] {
			continue
		}
		num, err := strconv.Atoi(m[2])
		if err != nil || num <= 0 {
			continue // overflow or a leading-zero oddity — not a key we can resolve
		}
		seen[id] = true
		out = append(out, Key{ProjectKey: m[1], Number: int32(num)})
	}
	return out
}
