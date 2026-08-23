package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// maxEntryMinutes caps one entry at four weeks. A single log longer than that is a
// typo — "3w" typed where "3h" was meant — and it would poison every rollup it lands in.
const maxEntryMinutes = 4 * 7 * 24 * 60

// durationRe parses "90m", "1.5h", "2d", "3w", or a bare number (minutes).
var durationRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([mhdw]?)$`)

// ParseDuration turns a human duration into minutes. Zero and negative are rejected by
// the caller; this only reports whether the shape was understood at all.
//
// Days are 8 hours and weeks are 5 days: somebody logging "2d" against an issue means
// two working days, and counting 48 hours would silently double every estimate that
// was written down in days.
func ParseDuration(s string) (int, bool) {
	m := durationRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	per := 1.0
	switch m[2] {
	case "h":
		per = 60
	case "d":
		per = 60 * 8
	case "w":
		per = 60 * 8 * 5
	}
	return int(n * per), true
}

// FormatDuration is ParseDuration's inverse for display: the largest whole unit that
// does not lose information, so "480" comes back as "1d" rather than "8h".
func FormatDuration(minutes int) string {
	switch {
	case minutes <= 0:
		return "0m"
	case minutes%(60*8*5) == 0:
		return strconv.Itoa(minutes/(60*8*5)) + "w"
	case minutes%(60*8) == 0:
		return strconv.Itoa(minutes/(60*8)) + "d"
	case minutes%60 == 0:
		return strconv.Itoa(minutes/60) + "h"
	default:
		return strconv.Itoa(minutes) + "m"
	}
}

// spendCommandRe matches a `/spend` line: the duration, an optional date, and a note.
// Anchored to the start of a line so a duration mentioned mid-sentence is not a command.
var spendCommandRe = regexp.MustCompile(`(?m)^/spend\s+(\S+)(?:\s+(\d{4}-\d{2}-\d{2}|yesterday|today))?[ \t]*(.*)$`)

// SpendCommand is one parsed `/spend` line from a comment body.
type SpendCommand struct {
	Minutes int
	SpentOn string // YYYY-MM-DD, empty = today
	Note    string
}

// ParseSpendCommands extracts every `/spend` line from a comment.
//
// The comment itself is still posted verbatim. Stripping the command out would leave
// the timeline saying somebody logged 90 minutes with no trace of what they wrote, and
// the sentence after the duration is usually the only explanation anyone gets.
func ParseSpendCommands(body string, now time.Time) []SpendCommand {
	var out []SpendCommand
	for _, m := range spendCommandRe.FindAllStringSubmatch(body, -1) {
		minutes, ok := ParseDuration(m[1])
		if !ok || minutes <= 0 || minutes > maxEntryMinutes {
			continue
		}
		spentOn := ""
		switch m[2] {
		case "yesterday":
			spentOn = now.AddDate(0, 0, -1).Format("2006-01-02")
		case "today", "":
		default:
			spentOn = m[2]
		}
		out = append(out, SpendCommand{
			Minutes: minutes, SpentOn: spentOn, Note: strings.TrimSpace(m[3]),
		})
	}
	return out
}

func (h *httpHandlers) listTimeEntries(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	entries, err := h.repo.ListTimeEntries(r.Context(), issue.ID)
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if entries == nil {
		entries = []domain.TimeEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":            entries,
		"spent_minutes":    issue.SpentMinutes,
		"estimate_minutes": issue.EstimateMinutes,
	})
}

func (h *httpHandlers) logTime(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	// Logging time is a claim about work on the issue, so it needs the same permission
	// as any other write to it.
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	var body struct {
		Duration string `json:"duration"`
		Minutes  int    `json:"minutes"`
		SpentOn  string `json:"spent_on"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	minutes := body.Minutes
	if strings.TrimSpace(body.Duration) != "" {
		parsed, ok := ParseDuration(body.Duration)
		if !ok {
			httpapi.WriteValidation(w, map[string]string{
				"duration": `expected a duration like "90m", "1.5h", "2d" or "1w"`,
			})
			return
		}
		minutes = parsed
	}
	if problems := validateEntry(minutes, body.SpentOn); len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	entry, err := h.repo.LogTime(r.Context(), TimeEntryInput{
		IssueID: issue.ID, UserID: actor, Minutes: minutes,
		SpentOn: strings.TrimSpace(body.SpentOn), Note: body.Note,
	})
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "issue", "log failed")
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func validateEntry(minutes int, spentOn string) map[string]string {
	problems := map[string]string{}
	if minutes <= 0 {
		problems["minutes"] = "must be greater than zero"
	} else if minutes > maxEntryMinutes {
		problems["minutes"] = "a single entry cannot exceed " + FormatDuration(maxEntryMinutes) +
			" — split it, or check the unit"
	}
	if s := strings.TrimSpace(spentOn); s != "" {
		day, err := time.Parse("2006-01-02", s)
		if err != nil {
			problems["spent_on"] = `expected a "YYYY-MM-DD" date`
		} else if day.After(time.Now().AddDate(0, 0, 1)) {
			// Tomorrow's date is allowed for timezone slack; next week is a typo.
			problems["spent_on"] = "cannot log time against a future date"
		}
	}
	return problems
}

func (h *httpHandlers) deleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", "expected a uuid")
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	// Anyone may delete their own entry; deleting somebody else's needs project:manage,
	// because a time sheet other people can quietly rewrite is not evidence of anything.
	force := p.Can(auth.PermProjectManage)
	deleted, err := h.repo.DeleteTimeEntry(r.Context(), id, actor, force)
	if err != nil {
		h.serverError(w, r, "delete failed", err)
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found",
			"no such entry, or it belongs to somebody else")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseEstimate reads an estimate from the wire. An empty string means "no estimate",
// returned as nil so callers can tell it apart from a parse failure.
func parseEstimate(v string) (*int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	minutes, ok := ParseDuration(v)
	if !ok || minutes <= 0 {
		return nil, errBadEstimate
	}
	if minutes > maxEntryMinutes {
		return nil, errEstimateTooBig
	}
	return &minutes, nil
}

var (
	errBadEstimate    = errors.New(`expected a duration like "90m", "1.5h", "2d" or "1w"`)
	errEstimateTooBig = errors.New("that is larger than " + FormatDuration(maxEntryMinutes) +
		" — an issue that big wants splitting, not estimating")
)
