package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// noteGroups is the order sections appear in, and the heading each issue type lands
// under. Types absent from a release simply produce no heading.
var noteGroups = []struct {
	heading string
	types   []domain.IssueType
}{
	{"Features", []domain.IssueType{domain.TypeFeature}},
	{"Improvements", []domain.IssueType{domain.TypeImprovement, domain.TypeTask}},
	{"Fixes", []domain.IssueType{domain.TypeBug}},
}

// excludeLabel keeps internal churn out of a changelog other people read.
const excludeLabel = "changelog:exclude"

// ComposeReleaseNotes renders the markdown for a release from the issues targeting it.
//
// Grouped by type and ordered by severity within a group, so the thing most worth
// knowing is at the top of its section. Every line links back, because the note is a
// summary and the issue is the detail.
func ComposeReleaseNotes(rel domain.Release, issues []domain.Issue) string {
	var b strings.Builder
	title := rel.Version
	if rel.Name != "" {
		title += " — " + rel.Name
	}
	fmt.Fprintf(&b, "## %s\n", title)
	if rel.GitTag != "" {
		fmt.Fprintf(&b, "\nTag `%s`.\n", rel.GitTag)
	}

	kept := make([]domain.Issue, 0, len(issues))
	for _, i := range issues {
		if !hasLabel(i.Labels, excludeLabel) {
			kept = append(kept, i)
		}
	}
	if len(kept) == 0 {
		b.WriteString("\n_No issues are targeted at this release yet._\n")
		return b.String()
	}

	for _, g := range noteGroups {
		section := filterByType(kept, g.types)
		if len(section) == 0 {
			continue
		}
		sortBySeverity(section)
		fmt.Fprintf(&b, "\n### %s\n\n", g.heading)
		for _, i := range section {
			fmt.Fprintf(&b, "- %s ([%s](/issues/%s))\n", i.Title, i.Key, i.Key)
		}
	}
	return b.String()
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, want) {
			return true
		}
	}
	return false
}

func filterByType(issues []domain.Issue, types []domain.IssueType) []domain.Issue {
	var out []domain.Issue
	for _, i := range issues {
		for _, t := range types {
			if i.Type == t {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

var severityRank = map[domain.Severity]int{
	domain.SeverityCritical: 0, domain.SeverityHigh: 1, domain.SeverityMedium: 2, domain.SeverityLow: 3,
}

// sortBySeverity is stable on the incoming order, so issues without a severity keep
// their relative position rather than being shuffled.
func sortBySeverity(issues []domain.Issue) {
	rank := func(i domain.Issue) int {
		if i.Severity == nil {
			return 4
		}
		return severityRank[*i.Severity]
	}
	sort.SliceStable(issues, func(a, b int) bool { return rank(issues[a]) < rank(issues[b]) })
}

// releaseNotes returns the composed notes for a release without saving them.
// `?apply=true` writes them into notes_md as a draft to edit.
//
// Never silently overwrites: applying to a release that already has notes requires
// `force=true`, so a generated draft cannot eat something somebody wrote by hand.
func (h *httpHandlers) releaseNotes(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", "invalid release id")
		return
	}

	rel, issues, err := h.releaseWithIssues(r.Context(), id)
	if err != nil {
		writeNotFoundOrError(w, err, "release", "compose failed")
		return
	}
	notes := ComposeReleaseNotes(rel, issues)

	if r.URL.Query().Get("apply") != "true" {
		if strings.HasSuffix(r.URL.Path, ".md") || r.URL.Query().Get("format") == "md" {
			// For CI cutting a tag: the markdown itself, not a JSON envelope.
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte(notes))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"notes_md": notes, "issue_count": len(issues), "current_notes_md": rel.NotesMD,
		})
		return
	}

	// Applying is a write; the read above is not.
	if _, ok := h.authorizeEntityManage(w, r, "release"); !ok {
		return
	}
	if strings.TrimSpace(rel.NotesMD) != "" && r.URL.Query().Get("force") != "true" {
		httpapi.WriteProblem(w, http.StatusConflict, "notes already written",
			"this release has notes; pass force=true to replace them")
		return
	}
	updated, err := h.repo.UpdateRelease(r.Context(), UpdateReleaseInput{ID: id, NotesMD: &notes})
	if err != nil {
		writeNotFoundOrError(w, err, "release", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// releaseWithIssues loads a release and everything targeting it, in one place so the
// preview and the apply path can never compose from different inputs.
func (h *httpHandlers) releaseWithIssues(ctx context.Context, id uuid.UUID) (domain.Release, []domain.Issue, error) {
	rel, err := h.repo.GetRelease(ctx, id)
	if err != nil {
		return domain.Release{}, nil, err
	}
	var issues []domain.Issue
	err = h.repo.EachIssue(ctx, IssueFilter{ProjectKey: rel.ProjectKey, ReleaseID: &id, Sort: "oldest"},
		func(i domain.Issue) error {
			issues = append(issues, i)
			return nil
		})
	return rel, issues, err
}
