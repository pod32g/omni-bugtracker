package prose

import (
	"regexp"
	"strings"
)

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
