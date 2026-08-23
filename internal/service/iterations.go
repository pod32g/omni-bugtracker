package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// velocityWindow is how many finished iterations the rolling average covers. Three is
// enough to survive one unusual fortnight and short enough to still describe the team
// as it is now rather than as it was last quarter.
const velocityWindow = 3

// maxIterationDays caps an iteration at a quarter. Longer than that and the burndown
// stops being a planning tool; it is also the shape of a mistyped year in a date.
const maxIterationDays = 92

func (h *httpHandlers) listIterations(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListIterations(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if items == nil {
		items = []domain.Iteration{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// iterationVelocity returns finished iterations plus the rolling average, and says how
// many iterations that average covers — "average of one" is not an average, and the
// planning page has to be able to say so rather than present a single fortnight as a
// trend.
func (h *httpHandlers) iterationVelocity(w http.ResponseWriter, r *http.Request) {
	history, err := h.repo.IterationVelocity(r.Context(), chi.URLParam(r, "key"), 12)
	if err != nil {
		h.serverError(w, r, "velocity failed", err)
		return
	}
	if history == nil {
		history = []domain.Velocity{}
	}
	issues, minutes, sampled := domain.AverageVelocity(history, velocityWindow)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":               history,
		"average_issues":      issues,
		"average_minutes":     minutes,
		"averaged_over":       sampled,
		"average_window_size": velocityWindow,
	})
}

func (h *httpHandlers) iterationBurndown(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", "expected a uuid")
		return
	}
	points, err := h.repo.IterationBurndown(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "burndown failed", err)
		return
	}
	if points == nil {
		points = []domain.BurndownPoint{}
	}
	it, err := h.repo.GetIteration(r.Context(), id)
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "iteration", "burndown failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"iteration": it, "points": points})
}

func (h *httpHandlers) createIteration(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	var body struct {
		Name     string `json:"name"`
		StartsOn string `json:"starts_on"`
		EndsOn   string `json:"ends_on"`
		Goal     string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	problems := map[string]string{}
	if strings.TrimSpace(body.Name) == "" {
		problems["name"] = "required"
	}
	start, end := parseIterationDates(body.StartsOn, body.EndsOn, problems)
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	it, err := h.repo.CreateIteration(r.Context(), IterationInput{
		ProjectKey: key, Name: strings.TrimSpace(body.Name),
		StartsOn: start, EndsOn: end, Goal: body.Goal,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"an iteration with that name may already exist: "+err.Error())
		return
	}
	h.audit(r, "iteration.create", "iteration", it.ID.String(), it.Name, map[string]any{
		"starts_on": it.StartsOn, "ends_on": it.EndsOn,
	})
	writeJSON(w, http.StatusCreated, it)
}

// parseIterationDates validates the window, writing into problems and returning the
// normalised dates.
func parseIterationDates(startsOn, endsOn string, problems map[string]string) (string, string) {
	start, errS := time.Parse("2006-01-02", strings.TrimSpace(startsOn))
	end, errE := time.Parse("2006-01-02", strings.TrimSpace(endsOn))
	if errS != nil {
		problems["starts_on"] = `expected a "YYYY-MM-DD" date`
	}
	if errE != nil {
		problems["ends_on"] = `expected a "YYYY-MM-DD" date`
	}
	if errS != nil || errE != nil {
		return "", ""
	}
	if end.Before(start) {
		problems["ends_on"] = "must not be before starts_on"
	}
	if days := int(end.Sub(start).Hours() / 24); days > maxIterationDays {
		problems["ends_on"] = "an iteration longer than a quarter is not a planning window — check the year"
	}
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

func (h *httpHandlers) updateIteration(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "iteration")
	if !ok {
		return
	}
	var body struct {
		Name     *string `json:"name"`
		StartsOn *string `json:"starts_on"`
		EndsOn   *string `json:"ends_on"`
		Goal     *string `json:"goal"`
		State    *string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	problems := map[string]string{}
	if body.State != nil && !domain.ValidIterationState(*body.State) {
		problems["state"] = "expected planned, active, or completed"
	}
	for field, v := range map[string]*string{"starts_on": body.StartsOn, "ends_on": body.EndsOn} {
		if v != nil {
			if _, err := time.Parse("2006-01-02", strings.TrimSpace(*v)); err != nil {
				problems[field] = `expected a "YYYY-MM-DD" date`
			}
		}
	}
	if len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	it, err := h.repo.UpdateIteration(r.Context(), id, UpdateIterationInput{
		Name: body.Name, StartsOn: body.StartsOn, EndsOn: body.EndsOn,
		Goal: body.Goal, State: body.State,
	})
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "iteration", "update failed")
		return
	}
	h.audit(r, "iteration.update", "iteration", it.ID.String(), it.Name, map[string]any{
		"state": it.State,
	})
	writeJSON(w, http.StatusOK, it)
}

func (h *httpHandlers) deleteIteration(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "iteration")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteIteration(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "delete failed", err)
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such iteration")
		return
	}
	h.audit(r, "iteration.delete", "iteration", id.String(), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

// carryOver moves an iteration's unfinished work somewhere else. Body:
// {"to": "<iteration uuid>"} — an empty or absent target returns it to the backlog.
//
// Deliberately a separate endpoint rather than something completing an iteration does
// on its own: a team that never sees the carry-over number never notices it growing.
func (h *httpHandlers) carryOverIteration(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "iteration")
	if !ok {
		return
	}
	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	var target *uuid.UUID
	if strings.TrimSpace(body.To) != "" {
		parsed, err := uuid.Parse(body.To)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"to": "expected an iteration uuid, or empty for the backlog"})
			return
		}
		if parsed == id {
			httpapi.WriteValidation(w, map[string]string{"to": "cannot carry an iteration over into itself"})
			return
		}
		// The destination needs its own authorization. Only the *source* was checked
		// above, so a maintainer of one project could push that project's unfinished
		// issues into an iteration belonging to a project they have no rights to: the
		// issues stayed where they were but pointed at a foreign iteration, corrupting
		// its burndown, velocity and effort rollups, and the owning project had no way
		// to see who did it — the audit row is on the source.
		//
		// Same shape as moving an issue between projects, which already requires
		// issue:create on the destination.
		if !h.authorizeTargetIteration(w, r, parsed) {
			return
		}
		target = &parsed
	}
	moved, err := h.repo.CarryOverIssues(r.Context(), id, target)
	if err != nil {
		h.serverError(w, r, "carry-over failed", err)
		return
	}
	h.audit(r, "iteration.carry_over", "iteration", id.String(), "", map[string]any{
		"moved": moved, "to": body.To,
	})
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved})
}

// authorizeTargetIteration resolves an iteration to its project and requires
// project:manage on it. Existence first, then permission — the same order (and the
// same answers) as authorizeEntityManage, so a caller cannot tell "no such iteration"
// apart from "not yours" by the shape of the refusal alone.
func (h *httpHandlers) authorizeTargetIteration(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	key, err := h.repo.ProjectKeyForEntity(r.Context(), "iteration", id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such iteration")
		return false
	}
	if !h.canOnProject(r.Context(), auth.FromContext(r.Context()), key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage on "+key)
		return false
	}
	return true
}

// setIssueIteration plans or unplans one issue. Body: {"iteration_id": "<uuid>"} —
// empty removes it from whatever iteration it is in.
func (h *httpHandlers) setIssueIteration(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	var body struct {
		IterationID string `json:"iteration_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	var target *uuid.UUID
	if strings.TrimSpace(body.IterationID) != "" {
		parsed, err := uuid.Parse(body.IterationID)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{
				"iteration_id": "expected an iteration uuid, or empty to unplan",
			})
			return
		}
		target = &parsed
	}
	actor, _ := uuid.Parse(p.UserID)
	if err := h.repo.SetIssueIteration(r.Context(), issue.ID, target, actor); err != nil {
		if strings.Contains(err.Error(), "does not belong") {
			httpapi.WriteValidation(w, map[string]string{
				"iteration_id": "that iteration belongs to a different project",
			})
			return
		}
		h.writeNotFoundOrError(w, r, err, "issue", "update failed")
		return
	}
	updated, err := h.repo.GetIssueByID(r.Context(), issue.ID)
	if err != nil {
		h.serverError(w, r, "reload failed", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// resolveIterationFilter turns the symbolic `iteration:` term into an id. Left to the
// handler because "current" and "next" need the database and ParseFilter is pure.
func (h *httpHandlers) resolveIterationFilter(r *http.Request, f *IssueFilter) {
	if f.IterationRef == "" {
		return
	}
	id, found, err := h.repo.ResolveIteration(r.Context(), f.ProjectKey, f.IterationRef)
	if err != nil {
		h.log.Warn("iteration filter resolve failed", "ref", f.IterationRef, "err", err)
	}
	if !found {
		// Nothing matched: narrow to empty. Dropping the term would list the whole
		// project, which is the opposite of what `iteration:current` asked for.
		f.MatchNothing = true
		return
	}
	f.IterationID = &id
}
