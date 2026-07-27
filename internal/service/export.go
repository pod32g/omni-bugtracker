package service

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// exportColumns is the CSV header, and the order the row builder writes in. Anything
// added here must be added to exportRow, hence the two living side by side.
var exportColumns = []string{
	"key", "project", "number", "type", "title", "status", "severity", "priority",
	"assignee", "reporter", "labels", "components", "milestone", "release",
	"version_affected", "version_fixed", "open_blockers", "created_at", "updated_at",
	"archived_at", "due_at", "first_response_at", "resolved_at", "sla_state",
	"estimate_minutes", "spent_minutes", "description",
}

// slaStateString renders an absent SLA as empty rather than "ok": a project with no
// policy is not meeting its targets, it simply has none.
func slaStateString(s *domain.IssueSLA) string {
	if s == nil {
		return ""
	}
	return s.State
}

// minutesString renders an unestimated issue as empty rather than 0 — a spreadsheet
// that averages the estimate column must not count "not estimated" as "zero work".
func minutesString(m *int) string {
	if m == nil {
		return ""
	}
	return strconv.Itoa(*m)
}

func exportRow(i domain.Issue) []string {
	return []string{
		i.Key, i.ProjectKey, strconv.FormatInt(int64(i.Number), 10), string(i.Type), i.Title,
		string(i.Status), severityString(i.Severity), string(i.Priority),
		userEmail(i.Assignee), userEmail(i.Reporter),
		strings.Join(i.Labels, " "), strings.Join(i.Components, " "),
		i.Milestone, i.Release, i.VersionAffected, i.VersionFixed,
		strconv.Itoa(i.OpenBlockers),
		i.CreatedAt.UTC().Format(time.RFC3339), i.UpdatedAt.UTC().Format(time.RFC3339),
		timeString(i.ArchivedAt), timeString(i.DueAt), timeString(i.FirstResponseAt),
		timeString(i.ResolvedAt), slaStateString(i.SLA),
		minutesString(i.EstimateMinutes), strconv.Itoa(i.SpentMinutes), i.DescriptionMD,
	}
}

// exportIssues streams every issue matching the current filter — the whole result set,
// not the page on screen. The filter is the interchange format: whatever the list shows
// is what comes out.
func (h *httpHandlers) exportIssues(w http.ResponseWriter, r *http.Request) {
	// Reads are gated by authentication alone throughout this API — there is no
	// issue:read permission — and an export is the paged list without the paging, so
	// it exposes nothing the caller could not already fetch page by page.
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")

	f, badTerms := ParseFilter(key, r.URL.Query().Get("filter"), p.UserID)
	if len(badTerms) > 0 {
		httpapi.WriteValidation(w, badTerms)
		return
	}
	f.Sort = r.URL.Query().Get("sort")

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		httpapi.WriteValidation(w, map[string]string{"format": `expected "csv" or "json"`})
		return
	}

	// Content-Disposition before the first write: once the body starts, the status and
	// headers are already on the wire and an error can only truncate the download.
	stamp := time.Now().UTC().Format("20060102-150405")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-issues-%s.%s"`, strings.ToLower(key), stamp, format))

	if format == "json" {
		h.exportJSON(w, r, f)
		return
	}
	h.exportCSV(w, r, f)
}

func (h *httpHandlers) exportCSV(w http.ResponseWriter, r *http.Request, f IssueFilter) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	cw := csv.NewWriter(w)
	if err := cw.Write(exportColumns); err != nil {
		return
	}
	err := h.repo.EachIssue(r.Context(), f, func(i domain.Issue) error {
		return cw.Write(exportRow(i))
	})
	cw.Flush()
	if err == nil {
		err = cw.Error()
	}
	if err != nil {
		// The header and some rows are already sent, so a problem+json body is not an
		// option. Truncate with a marker the reader will notice, and log it.
		fmt.Fprintf(w, "\n# export failed after partial output: %v\n", err)
	}
}

func (h *httpHandlers) exportJSON(w http.ResponseWriter, r *http.Request, f IssueFilter) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)

	// Streamed as a JSON array written by hand rather than buffered into a slice: the
	// point of the export is that it does not have to fit in memory first.
	if _, err := w.Write([]byte(`{"items":[`)); err != nil {
		return
	}
	first := true
	err := h.repo.EachIssue(r.Context(), f, func(i domain.Issue) error {
		if !first {
			if _, err := w.Write([]byte(",")); err != nil {
				return err
			}
		}
		first = false
		return enc.Encode(i)
	})
	if err != nil {
		_, _ = fmt.Fprintf(w, `],"error":%q}`, err.Error())
		return
	}
	_, _ = w.Write([]byte(`]}`))
}

func severityString(s *domain.Severity) string {
	if s == nil {
		return ""
	}
	return string(*s)
}

func userEmail(u *domain.User) string {
	if u == nil {
		return ""
	}
	return u.Email
}

func timeString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
