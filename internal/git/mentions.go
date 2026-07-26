package git

import (
	"regexp"
	"strconv"
	"strings"
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

// Code spans and fenced blocks, stripped before matching. A pasted log or a snippet
// containing something key-shaped must not mint a cross-reference — that is noise
// nobody asked for and cannot easily remove.
var (
	fencedCode = regexp.MustCompile("(?s)```.*?```|(?s)~~~.*?~~~")
	inlineCode = regexp.MustCompile("`[^`\n]*`")
	// Indented code blocks: four spaces or a tab at the start of a line.
	indentedCode = regexp.MustCompile(`(?m)^(?: {4}|\t).*$`)
	// Markdown links and images: the URL half often carries a key ("/issues/BUG-42")
	// which is already a link, so counting it again would double up.
	linkTarget = regexp.MustCompile(`\]\([^)]*\)`)
)

// ParseKeys returns the distinct issue keys mentioned in markdown prose, in order of
// first appearance.
//
// This is deliberately not RefParser: that one requires a verb ("fixes BUG-42") because
// a commit message saying a key should not silently close an issue. Prose is the other
// way round — writing the key at all is the reference, and demanding a verb would miss
// almost every real mention.
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

// stripCode blanks out code spans, blocks and link targets, preserving offsets nowhere
// in particular — only the surviving prose matters.
func stripCode(text string) string {
	for _, re := range []*regexp.Regexp{fencedCode, inlineCode, indentedCode, linkTarget} {
		text = re.ReplaceAllStringFunc(text, blank)
	}
	return text
}

// blank keeps newlines so line-anchored patterns applied afterwards still see the
// original line structure.
func blank(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		return ' '
	}, s)
}
