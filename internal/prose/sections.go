package prose

import (
	"regexp"
	"strings"
)

// headingRe matches an ATX markdown heading and captures its text.
var headingRe = regexp.MustCompile(`^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$`)

// checklistRe matches a GFM task-list item, capturing whether it is ticked.
var checklistRe = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s`)

// Sections splits a markdown body into heading → content, lower-cased and trimmed for
// matching. Content before the first heading is dropped: a required section is a
// question the template asked, and a preamble does not answer any of them.
//
// Code fences are honoured, so a `# comment` inside a shell block is not a heading —
// the same reason ParseKeys strips code before looking for issue references.
func Sections(body string) map[string]string {
	out := map[string]string{}
	var current string
	var buf []string
	inFence := false

	flush := func() {
		if current != "" {
			out[current] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			buf = append(buf, line)
			continue
		}
		if !inFence {
			if m := headingRe.FindStringSubmatch(line); m != nil {
				flush()
				current = strings.ToLower(strings.TrimSpace(m[2]))
				continue
			}
		}
		buf = append(buf, line)
	}
	flush()
	return out
}

// MissingHeadings returns the required headings a body does not *contain*, ignoring
// whether anything is written under them.
//
// This is the check for a template: a template's sections are supposed to be empty —
// they are the prompts. Using the answered-ness check here would reject every template
// that does its job.
func MissingHeadings(body string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	found := Sections(body)
	var missing []string
	for _, want := range required {
		key := strings.ToLower(strings.TrimSpace(want))
		if key == "" {
			continue
		}
		if _, ok := found[key]; !ok {
			missing = append(missing, want)
		}
	}
	return missing
}

// MissingSections returns the required headings a body does not answer, in the order
// they were required — so the error names them the way the template lists them.
//
// A heading with nothing under it counts as missing. Leaving the template's prompt in
// place and filing it is the most common way an issue arrives empty, and accepting it
// because the heading is technically present would defeat the whole feature.
func MissingSections(body string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	found := Sections(body)
	var missing []string
	for _, want := range required {
		key := strings.ToLower(strings.TrimSpace(want))
		if key == "" {
			continue
		}
		if content, ok := found[key]; !ok || !hasSubstance(content) {
			missing = append(missing, want)
		}
	}
	return missing
}

// hasSubstance rejects a section whose only content is the template's own scaffolding:
// blank lines, an HTML comment, or an unticked checklist with nothing written.
func hasSubstance(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	// Strip HTML comments, which is how templates carry instructions to the filer.
	stripped := htmlCommentRe.ReplaceAllString(content, "")
	for _, line := range strings.Split(stripped, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// A bare unticked checklist item is a prompt, not an answer; a ticked one is.
		if m := checklistRe.FindStringSubmatch(t); m != nil && m[1] == " " {
			continue
		}
		return true
	}
	return false
}

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// Checklist counts GFM task-list items in a body, and how many are ticked. Fenced code
// is skipped so a snippet showing markdown syntax does not become somebody's progress.
func Checklist(body string) (done, total int) {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := checklistRe.FindStringSubmatch(line); m != nil {
			total++
			if m[1] != " " {
				done++
			}
		}
	}
	return done, total
}
