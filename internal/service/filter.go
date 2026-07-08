package service

import (
	"strings"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
)

// ParseFilter turns a GitHub-style filter string into an IssueFilter.
// Supported keys: is/status, assignee (@me or uuid), severity, type. Bare words
// accumulate into the full-text query.
func ParseFilter(projectKey, raw, meUserID string) IssueFilter {
	f := IssueFilter{ProjectKey: projectKey}
	var freeText []string

	for _, tok := range strings.Fields(raw) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok {
			freeText = append(freeText, tok)
			continue
		}
		switch strings.ToLower(key) {
		case "is", "status":
			s := domain.IssueStatus(val)
			f.Status = &s
		case "assignee":
			if val == "@me" {
				if id, err := uuid.Parse(meUserID); err == nil {
					f.AssigneeID = &id
				}
			} else if id, err := uuid.Parse(val); err == nil {
				f.AssigneeID = &id
			}
		case "severity":
			sev := domain.Severity(val)
			f.Severity = &sev
		case "type":
			t := domain.IssueType(val)
			f.Type = &t
		default:
			freeText = append(freeText, tok)
		}
	}
	f.Query = strings.Join(freeText, " ")
	return f
}
