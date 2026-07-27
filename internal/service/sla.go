package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// maxSLAMinutes caps a budget at one year. Not a business rule — a guard against the
// fat-finger that types minutes where it meant to type days and produces deadlines
// Postgres cannot represent.
const maxSLAMinutes = 365 * 24 * 60

// parseDueAt accepts an RFC 3339 timestamp or a bare "YYYY-MM-DD" date, returning
// (time, clear, err). An empty string means "clear it", which is why clear is its own
// return value rather than a nil time — nil already means "not supplied".
//
// A bare date is taken as the end of that day in the server's zone: somebody typing
// "due 5 March" means by the end of the 5th, and midnight would make it late all day.
func parseDueAt(v string) (*time.Time, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, true, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t, false, nil
	}
	if d, err := time.Parse("2006-01-02", v); err == nil {
		end := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, time.Local)
		return &end, false, nil
	}
	return nil, false, errBadDueAt
}

var errBadDueAt = errors.New(`expected an RFC 3339 timestamp or a "YYYY-MM-DD" date`)

func (h *httpHandlers) listSLAPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.repo.ListSLAPolicies(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if policies == nil {
		policies = []domain.SLAPolicy{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": policies})
}

func (h *httpHandlers) createSLAPolicy(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	if _, err := h.repo.GetProjectByKey(r.Context(), key); err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	var body struct {
		Severity          string `json:"severity"`
		Type              string `json:"type"`
		ResponseMinutes   int    `json:"response_minutes"`
		ResolutionMinutes int    `json:"resolution_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}

	problems := map[string]string{}
	in := SLAPolicyInput{
		ProjectKey:        key,
		ResponseMinutes:   body.ResponseMinutes,
		ResolutionMinutes: body.ResolutionMinutes,
	}
	// Empty string means "any severity" rather than an invalid one — a catch-all row is
	// the common case, and making callers send null for it would be a trap.
	if v := strings.TrimSpace(strings.ToLower(body.Severity)); v != "" && v != "*" {
		sev := domain.Severity(v)
		if !domain.ValidSeverity(sev) {
			problems["severity"] = "unknown severity " + quoted(v) +
				" — expected critical, high, medium, low, or empty for any"
		} else {
			in.Severity = &sev
		}
	}
	if v := strings.TrimSpace(strings.ToLower(body.Type)); v != "" && v != "*" {
		t := domain.IssueType(v)
		if !domain.ValidType(t) {
			problems["type"] = "unknown type " + quoted(v) +
				" — expected bug, task, feature, improvement, or empty for any"
		} else {
			in.Type = &t
		}
	}
	for field, mins := range map[string]int{
		"response_minutes": in.ResponseMinutes, "resolution_minutes": in.ResolutionMinutes,
	} {
		if mins <= 0 || mins > maxSLAMinutes {
			problems[field] = "must be between 1 and " + strconv.Itoa(maxSLAMinutes) + " minutes"
		}
	}
	// A response target longer than the resolution target is not a policy anyone can
	// meet in the order it describes; it is almost always the two fields swapped.
	if len(problems) == 0 && in.ResponseMinutes > in.ResolutionMinutes {
		problems["response_minutes"] = "must not exceed resolution_minutes — a first response cannot be due after the fix"
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}

	pol, err := h.repo.CreateSLAPolicy(r.Context(), in)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a policy for that severity/type may already exist: "+err.Error())
		return
	}
	h.audit(r, "sla_policy.create", "sla_policy", pol.ID.String(), key, map[string]any{
		"severity": body.Severity, "type": body.Type,
		"response_minutes": pol.ResponseMinutes, "resolution_minutes": pol.ResolutionMinutes,
	})
	writeJSON(w, http.StatusCreated, pol)
}

func (h *httpHandlers) updateSLAPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "sla_policy")
	if !ok {
		return
	}
	var body struct {
		ResponseMinutes   *int  `json:"response_minutes"`
		ResolutionMinutes *int  `json:"resolution_minutes"`
		IsActive          *bool `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	problems := map[string]string{}
	if body.ResponseMinutes != nil && (*body.ResponseMinutes <= 0 || *body.ResponseMinutes > maxSLAMinutes) {
		problems["response_minutes"] = "must be between 1 and " + strconv.Itoa(maxSLAMinutes) + " minutes"
	}
	if body.ResolutionMinutes != nil && (*body.ResolutionMinutes <= 0 || *body.ResolutionMinutes > maxSLAMinutes) {
		problems["resolution_minutes"] = "must be between 1 and " + strconv.Itoa(maxSLAMinutes) + " minutes"
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	pol, err := h.repo.UpdateSLAPolicy(r.Context(), id, UpdateSLAPolicyInput{
		ResponseMinutes: body.ResponseMinutes, ResolutionMinutes: body.ResolutionMinutes,
		IsActive: body.IsActive,
	})
	if err != nil {
		writeNotFoundOrError(w, err, "sla policy", "update failed")
		return
	}
	// Checked after the write rather than before: the stored values are what the two
	// fields end up as, and validating the request alone would let a one-field edit
	// leave the pair in an order nobody can satisfy.
	if pol.ResponseMinutes > pol.ResolutionMinutes {
		httpapi.WriteValidation(w, map[string]string{
			"response_minutes": "must not exceed resolution_minutes — a first response cannot be due after the fix",
		})
		return
	}
	h.audit(r, "sla_policy.update", "sla_policy", pol.ID.String(), pol.ProjectKey, map[string]any{
		"response_minutes": pol.ResponseMinutes, "resolution_minutes": pol.ResolutionMinutes,
		"is_active": pol.IsActive,
	})
	writeJSON(w, http.StatusOK, pol)
}

func (h *httpHandlers) deleteSLAPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "sla_policy")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteSLAPolicy(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such policy")
		return
	}
	h.audit(r, "sla_policy.delete", "sla_policy", id.String(), "", nil)
	w.WriteHeader(http.StatusNoContent)
}
