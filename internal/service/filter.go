package service

import (
	"strings"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
)

// ParseFilter turns a GitHub-style filter string into an IssueFilter, returning
// per-term validation errors (empty when the whole filter parsed cleanly).
//
// Supported keys: is/status, assignee (@me or uuid), severity, type, label,
// component, milestone, release. Bare words accumulate into the full-text query.
// Values may be double-quoted to carry spaces: label:"needs triage".
//
// Enum values are validated here rather than passed through: they end up compared
// against Postgres enum columns, where an unknown value fails the whole query
// instead of matching nothing.
func ParseFilter(projectKey, raw, meUserID string) (IssueFilter, map[string]string) {
	f := IssueFilter{ProjectKey: projectKey}
	fields := map[string]string{}
	var freeText []string

	for _, tok := range splitFilterTerms(raw) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok {
			freeText = append(freeText, tok)
			continue
		}
		val = unquote(val)
		switch strings.ToLower(key) {
		case "is":
			// `is:` is the lifecycle predicate. `is:open` / `is:closed` are *sets* —
			// "open" means "not finished", the same definition the dashboard counts —
			// and `is:archived` is orthogonal to status entirely. To match one exact
			// status, use `status:` instead.
			switch {
			case strings.EqualFold(val, "archived"):
				f.ShowArchived = true
			case strings.EqualFold(val, "open"):
				f.Statuses = append(f.Statuses, domain.OpenStatuses...)
			case strings.EqualFold(val, "closed"):
				f.Statuses = append(f.Statuses, domain.ClosedStatuses...)
			default:
				s := domain.IssueStatus(strings.ToLower(val))
				if !domain.ValidStatus(s) {
					fields[key] = "unknown value " + quoted(val) +
						" — expected open, closed, archived, or a status name (" + joinStatuses() + ")"
					continue
				}
				f.Statuses = append(f.Statuses, s)
			}
		case "status":
			// Exact status match only. `status:open` must mean the literal "open"
			// status, not the whole unfinished set — otherwise there is no way to
			// ask for just-filed issues.
			s := domain.IssueStatus(strings.ToLower(val))
			if !domain.ValidStatus(s) {
				fields[key] = "unknown status " + quoted(val) + " — expected " + joinStatuses()
				continue
			}
			f.Statuses = append(f.Statuses, s)
		case "assignee":
			// Fail closed: an unresolvable assignee must not widen the result set
			// to every issue in the project.
			target := val
			if val == "@me" {
				target = meUserID
			}
			id, err := uuid.Parse(target)
			if err != nil {
				if val == "@me" {
					fields[key] = "@me is unavailable — could not resolve the current user"
				} else {
					fields[key] = "expected @me or a user uuid, got " + quoted(val)
				}
				continue
			}
			f.AssigneeID = &id
		case "severity":
			sev := domain.Severity(strings.ToLower(val))
			if !domain.ValidSeverity(sev) {
				fields[key] = "unknown severity " + quoted(val) + " — expected critical, high, medium, or low"
				continue
			}
			f.Severity = &sev
		case "type":
			t := domain.IssueType(strings.ToLower(val))
			if !domain.ValidType(t) {
				fields[key] = "unknown type " + quoted(val) + " — expected bug, task, feature, or improvement"
				continue
			}
			f.Type = &t
		case "label":
			f.Label = val
		case "component":
			f.Component = val
		case "milestone": // UI-generated deep links pass the milestone id
			id, err := uuid.Parse(val)
			if err != nil {
				fields[key] = "expected a milestone uuid, got " + quoted(val)
				continue
			}
			f.MilestoneID = &id
		case "release":
			id, err := uuid.Parse(val)
			if err != nil {
				fields[key] = "expected a release uuid, got " + quoted(val)
				continue
			}
			f.ReleaseID = &id
		default:
			freeText = append(freeText, tok)
		}
	}
	f.Query = strings.Join(freeText, " ")
	f.Statuses = dedupeStatuses(f.Statuses)
	return f, fields
}

// splitFilterTerms splits on whitespace like strings.Fields, but keeps
// double-quoted runs together so `label:"needs triage"` stays one term. An
// unterminated quote simply runs to the end of the input.
func splitFilterTerms(raw string) []string {
	var terms []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range raw {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			cur.WriteRune(r)
		case !inQuotes && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			if cur.Len() > 0 {
				terms = append(terms, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		terms = append(terms, cur.String())
	}
	return terms
}

// unquote strips a surrounding pair of double quotes (and any stray ones left by
// an unbalanced term) and trims the result.
func unquote(v string) string {
	return strings.TrimSpace(strings.Trim(v, `"`))
}

func quoted(v string) string { return `"` + v + `"` }

func joinStatuses() string {
	names := make([]string, 0, len(domain.AllStatuses))
	for _, s := range domain.AllStatuses {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

// dedupeStatuses keeps the set stable and duplicate-free — `is:open status:blocked`
// should not list blocked twice.
func dedupeStatuses(in []domain.IssueStatus) []domain.IssueStatus {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[domain.IssueStatus]bool, len(in))
	out := make([]domain.IssueStatus, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
