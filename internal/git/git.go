// Package git turns inbound version-control webhooks into issue links and transitions:
// it parses "Fixes BUG-421" style references from commit messages and pull requests,
// then the worker links commits/PRs to issues, updates the timeline, and auto-resolves
// or closes issues. The parsing here is pure and VCS-agnostic; github.go adapts payloads.
package git

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Commit is a normalized commit from any provider.
type Commit struct {
	SHA       string
	Message   string
	Author    string
	URL       string
	Timestamp time.Time
}

// PullRequest is a normalized pull/merge request.
type PullRequest struct {
	Number         int
	Title          string
	Body           string
	URL            string
	State          string // open|closed
	Merged         bool
	MergeCommitSHA string
}

// Event is a normalized push or pull_request webhook.
type Event struct {
	Provider string
	Repo     string
	Kind     string // "push" | "pull_request"
	Commits  []Commit
	PR       *PullRequest
}

// Ref is a parsed issue reference: which issue, the canonical verb, and whether the
// verb closes the issue (per configuration).
type Ref struct {
	Verb       string // canonical ref_verb enum: fixes|closes|resolves|refs|related
	ProjectKey string
	Number     int32
	Close      bool
}

// RefParser extracts issue references from free text using configurable verbs.
type RefParser struct {
	re         *regexp.Regexp
	closeVerbs map[string]bool
}

// NewRefParser compiles a matcher for the given close/link verbs. Verbs are matched
// case-insensitively; unknown-but-configured verbs still classify as close/link.
func NewRefParser(closeVerbs, linkVerbs []string) *RefParser {
	all := append(append([]string{}, closeVerbs...), linkVerbs...)
	quoted := make([]string, 0, len(all))
	for _, v := range all {
		if v = strings.TrimSpace(v); v != "" {
			quoted = append(quoted, regexp.QuoteMeta(v))
		}
	}
	// verb  KEY-123   (project key is 2-10 chars; unknown keys are filtered at resolution)
	pattern := `(?i)\b(` + strings.Join(quoted, "|") + `)\s+([A-Za-z][A-Za-z0-9]{1,9})-(\d+)\b`
	cm := make(map[string]bool, len(closeVerbs))
	for _, v := range closeVerbs {
		cm[strings.ToLower(strings.TrimSpace(v))] = true
	}
	return &RefParser{re: regexp.MustCompile(pattern), closeVerbs: cm}
}

// Parse returns the distinct references in text. A close reference wins over a link
// reference to the same issue.
func (p *RefParser) Parse(text string) []Ref {
	matches := p.re.FindAllStringSubmatch(text, -1)
	byKey := map[string]Ref{}
	order := []string{}
	for _, m := range matches {
		verb := strings.ToLower(m[1])
		key := strings.ToUpper(m[2])
		num, err := strconv.Atoi(m[3])
		if err != nil {
			continue
		}
		close := p.closeVerbs[verb]
		id := key + "-" + m[3]
		ref := Ref{Verb: canonicalVerb(verb, close), ProjectKey: key, Number: int32(num), Close: close}
		if existing, ok := byKey[id]; ok {
			if ref.Close && !existing.Close {
				byKey[id] = ref // upgrade link -> close
			}
			continue
		}
		byKey[id] = ref
		order = append(order, id)
	}
	out := make([]Ref, 0, len(order))
	for _, id := range order {
		out = append(out, byKey[id])
	}
	return out
}

// canonicalVerb maps a matched verb to a valid ref_verb enum value.
func canonicalVerb(verb string, close bool) string {
	switch verb {
	case "fix", "fixes", "fixed":
		return "fixes"
	case "close", "closes", "closed":
		return "closes"
	case "resolve", "resolves", "resolved":
		return "resolves"
	case "related", "relates":
		return "related"
	case "ref", "refs", "see":
		return "refs"
	}
	if close {
		return "closes"
	}
	return "refs"
}
