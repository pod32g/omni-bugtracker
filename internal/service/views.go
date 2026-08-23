package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// Shared views: project-scoped saved searches.
//
// The filter grammar is this tracker's best feature and, while views were personal, it
// only made individuals fast. A shared view turns an agreed definition — the triage
// queue, release blockers, regressions — into part of the project rather than something
// each person has to be told and retype identically.
//
// Reading them needs nothing beyond membership; creating and editing needs
// project:manage, because a shared view is a claim about how the team works.

func (h *httpHandlers) listProjectViews(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListProjectViews(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if items == nil {
		items = []domain.SavedSearch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// listAllViews is the install-wide list, for curating shared views in one place.
// Reviewing them one project at a time is why nobody reviews them.
func (h *httpHandlers) listAllViews(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListSharedViews(r.Context())
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if items == nil {
		items = []domain.SavedSearch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// updateSavedSearch edits one of the caller's personal views. Saving under a new name
// would leave the old one behind, so renaming needs its own path.
func (h *httpHandlers) updateSavedSearch(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	userID, _ := uuid.Parse(p.UserID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", "invalid view id")
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Query       *string `json:"query"`
		Description *string `json:"description"`
		Sort        *string `json:"sort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if trimmed == "" {
			httpapi.WriteValidation(w, map[string]string{"name": "cannot be empty"})
			return
		}
		body.Name = &trimmed
	}
	// Personal views span every project, so the filter is parsed unscoped.
	if body.Query != nil {
		if _, bad := ParseFilter("", *body.Query, p.UserID); len(bad) > 0 {
			httpapi.WriteValidation(w, bad)
			return
		}
	}
	view, err := h.repo.UpdateSavedSearch(r.Context(), userID, UpdateSavedSearchInput{
		ID: id, Name: body.Name, Query: body.Query, Description: body.Description, Sort: body.Sort,
	})
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "saved search", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *httpHandlers) createProjectView(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Query       string `json:"query"`
		Description string `json:"description"`
		Sort        string `json:"sort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		httpapi.WriteValidation(w, map[string]string{"name": "cannot be empty"})
		return
	}
	// Validate the filter now rather than letting every future reader discover it is
	// broken: a shared view with a typo is a trap the whole team walks into.
	if _, bad := ParseFilter(key, body.Query, p.UserID); len(bad) > 0 {
		httpapi.WriteValidation(w, bad)
		return
	}

	author, _ := uuid.Parse(p.UserID)
	view, err := h.repo.CreateProjectView(r.Context(), ProjectViewInput{
		ProjectKey: key, AuthorID: author, Name: body.Name, Query: body.Query,
		Description: body.Description, Sort: body.Sort,
	})
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "project", "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *httpHandlers) updateProjectView(w http.ResponseWriter, r *http.Request) {
	id, key, ok := h.authorizeViewManage(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Query       *string `json:"query"`
		Description *string `json:"description"`
		Sort        *string `json:"sort"`
		Position    *int    `json:"position"`
		IsDefault   *bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Query != nil {
		p := auth.FromContext(r.Context())
		if _, bad := ParseFilter(key, *body.Query, p.UserID); len(bad) > 0 {
			httpapi.WriteValidation(w, bad)
			return
		}
	}
	view, err := h.repo.UpdateProjectView(r.Context(), UpdateProjectViewInput{
		ID: id, Name: body.Name, Query: body.Query, Description: body.Description,
		Sort: body.Sort, Position: body.Position, IsDefault: body.IsDefault,
	})
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "view", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *httpHandlers) deleteProjectView(w http.ResponseWriter, r *http.Request) {
	id, _, ok := h.authorizeViewManage(w, r)
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteProjectView(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "delete failed", err)
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such view")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shareSavedSearch promotes one of the caller's personal views into a project's shared
// list. The row is kept, so the author and creation date survive the promotion.
func (h *httpHandlers) shareSavedSearch(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", "invalid view id")
		return
	}
	var body struct {
		ProjectKey string `json:"project_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if !h.canOnProject(r.Context(), p, body.ProjectKey, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	userID, _ := uuid.Parse(p.UserID)
	view, err := h.repo.ShareSavedSearch(r.Context(), userID, id, body.ProjectKey)
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "saved search", "share failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// authorizeViewManage resolves a shared view and checks project:manage on the project
// that owns it — ownership of a shared view is the project's, not the creator's.
func (h *httpHandlers) authorizeViewManage(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", "invalid view id")
		return uuid.Nil, "", false
	}
	key, err := h.repo.GetViewProjectKey(r.Context(), id)
	if err != nil {
		h.writeNotFoundOrError(w, r, err, "view", "lookup failed")
		return uuid.Nil, "", false
	}
	if key == "" {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such shared view")
		return uuid.Nil, "", false
	}
	if !h.canOnProject(r.Context(), auth.FromContext(r.Context()), key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return uuid.Nil, "", false
	}
	return id, key, true
}
