package service

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// NewHTTPHandlers builds the authenticated REST surface, wired to the service layer.
// This hand-written router delegates to the same services the generated strict-server
// will use post-`make generate`, so there is no business-logic duplication.
func NewHTTPHandlers(repo Repository, pub Publisher, logger *slog.Logger, cfg *config.Config) http.Handler {
	issues := NewIssues(repo, pub, logger)
	h := &httpHandlers{issues: issues, repo: repo}

	r := chi.NewRouter()
	r.Get("/me", h.me)
	r.Get("/me/tokens", h.listTokens)
	r.Post("/me/tokens", h.createToken)
	r.Delete("/me/tokens/{id}", h.revokeToken)
	r.Get("/users", h.users)
	r.Patch("/users/{id}/role", h.updateUserRole)
	r.Get("/dashboards/overview", h.dashboard)
	r.Get("/projects", h.listProjects)
	r.Post("/projects", h.createProject)
	r.Get("/projects/{key}", h.getProject)
	r.Patch("/projects/{key}", h.updateProject)
	r.Delete("/projects/{key}", h.archiveProject)
	r.Get("/projects/{key}/labels", h.listLabels)
	r.Get("/projects/{key}/issues", h.listIssues)
	r.Post("/projects/{key}/issues", h.createIssue)
	r.Get("/issues/{issueKey}", h.getIssue)
	r.Patch("/issues/{issueKey}", h.updateIssue)
	r.Delete("/issues/{issueKey}", h.deleteIssue)
	r.Post("/issues/{issueKey}/transition", h.transition)
	r.Get("/issues/{issueKey}/comments", h.listComments)
	r.Post("/issues/{issueKey}/comments", h.addComment)
	r.Get("/issues/{issueKey}/activity", h.activity)
	r.Get("/issues/{issueKey}/commits", h.commits)
	return r
}

type httpHandlers struct {
	issues *Issues
	repo   Repository
}

var projectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

func (h *httpHandlers) me(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"id": p.UserID, "email": p.Email, "display_name": p.DisplayName, "role": p.Role,
	})
}

// ── personal API tokens (self-service) ──

func (h *httpHandlers) listTokens(w http.ResponseWriter, r *http.Request) {
	uid, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	tokens, err := h.repo.ListAPITokens(r.Context(), uid)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if tokens == nil {
		tokens = []domain.APIToken{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tokens})
}

func (h *httpHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	uid, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		httpapi.WriteValidation(w, map[string]string{"name": "required"})
		return
	}
	plaintext, hash, err := auth.GenerateAPIToken()
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "token generation failed", err.Error())
		return
	}
	tok, err := h.repo.CreateAPIToken(r.Context(), CreateTokenInput{
		UserID: uid, Name: strings.TrimSpace(body.Name), Scopes: body.Scopes, TokenHash: hash,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "create failed", err.Error())
		return
	}
	// `token` is the only time the plaintext is ever returned — shown once.
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      plaintext,
		"id":         tok.ID,
		"name":       tok.Name,
		"scopes":     tok.Scopes,
		"created_at": tok.CreatedAt,
	})
}

func (h *httpHandlers) revokeToken(w http.ResponseWriter, r *http.Request) {
	uid, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	tid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad token id", "")
		return
	}
	ok, err := h.repo.RevokeAPIToken(r.Context(), uid, tid)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "revoke failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) users(w http.ResponseWriter, r *http.Request) {
	users, err := h.repo.ListUsers(r.Context(), 500)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (h *httpHandlers) updateUserRole(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermAdmin) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing admin:all")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad user id", "")
		return
	}
	if id.String() == p.UserID {
		httpapi.WriteProblem(w, http.StatusConflict, "forbidden", "you can't change your own role")
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if !validRole(body.Role) {
		httpapi.WriteValidation(w, map[string]string{"role": "must be owner, admin, maintainer, member, reporter, or bot"})
		return
	}
	user, err := h.repo.UpdateUserRole(r.Context(), id, domain.Role(body.Role))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func validRole(role string) bool {
	switch domain.Role(role) {
	case domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer, domain.RoleMember, domain.RoleReporter, domain.RoleBot:
		return true
	default:
		return false
	}
}

func (h *httpHandlers) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.repo.Dashboard(r.Context())
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "dashboard failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *httpHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.repo.ListProjects(r.Context(), 200, 0)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": projects})
}

func (h *httpHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	var body struct {
		Key           string `json:"key"`
		Name          string `json:"name"`
		DescriptionMD string `json:"description_md"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	fields := map[string]string{}
	if !projectKeyRe.MatchString(body.Key) {
		fields["key"] = "must be 2–10 uppercase letters/digits, starting with a letter"
	}
	if strings.TrimSpace(body.Name) == "" {
		fields["name"] = "required"
	}
	if len(fields) > 0 {
		httpapi.WriteValidation(w, fields)
		return
	}
	project, err := h.repo.CreateProject(r.Context(), CreateProjectInput{
		Key: body.Key, Name: body.Name, DescriptionMD: body.DescriptionMD,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a project with that key may already exist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (h *httpHandlers) getProject(w http.ResponseWriter, r *http.Request) {
	project, err := h.repo.GetProjectByKey(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (h *httpHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	key := chi.URLParam(r, "key")
	if _, err := h.repo.GetProjectByKey(r.Context(), key); err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	var body struct {
		Name          *string `json:"name"`
		DescriptionMD *string `json:"description_md"`
		IsArchived    *bool   `json:"is_archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		httpapi.WriteValidation(w, map[string]string{"name": "cannot be empty"})
		return
	}
	project, err := h.repo.UpdateProject(r.Context(), UpdateProjectInput{
		Key: key, Name: body.Name, DescriptionMD: body.DescriptionMD, IsArchived: body.IsArchived,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (h *httpHandlers) archiveProject(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	key := chi.URLParam(r, "key")
	if _, err := h.repo.GetProjectByKey(r.Context(), key); err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	archived := true
	if _, err := h.repo.UpdateProject(r.Context(), UpdateProjectInput{Key: key, IsArchived: &archived}); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "archive failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) listLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := h.repo.ListLabels(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": labels})
}

func (h *httpHandlers) listIssues(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	f := ParseFilter(key, r.URL.Query().Get("filter"), p.UserID)
	f.Sort = r.URL.Query().Get("sort")
	f.Limit = int32(atoiDefault(r.URL.Query().Get("limit"), 50))

	items, total, err := h.issues.List(r.Context(), f)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *httpHandlers) createIssue(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermIssueCreate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:create")
		return
	}
	var body struct {
		Type            domain.IssueType `json:"type"`
		Title           string           `json:"title"`
		DescriptionMD   string           `json:"description_md"`
		Severity        *domain.Severity `json:"severity"`
		Priority        domain.Priority  `json:"priority"`
		AssigneeID      *uuid.UUID       `json:"assignee_id"`
		Labels          []string         `json:"labels"`
		Components      []string         `json:"components"`
		VersionAffected string           `json:"version_affected"`
		ReproStepsMD    string           `json:"repro_steps_md"`
		ExpectedMD      string           `json:"expected_md"`
		ActualMD        string           `json:"actual_md"`
		EnvironmentMD   string           `json:"environment_md"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		httpapi.WriteValidation(w, map[string]string{"title": "required"})
		return
	}
	reporter, _ := uuid.Parse(p.UserID)
	issue, err := h.issues.Create(r.Context(), CreateIssueInput{
		ProjectKey: chi.URLParam(r, "key"), Type: body.Type, Title: body.Title,
		DescriptionMD: body.DescriptionMD, Severity: body.Severity, Priority: body.Priority,
		ReporterID: reporter, AssigneeID: body.AssigneeID, Labels: body.Labels,
		Components: body.Components, VersionAffected: body.VersionAffected,
		ReproStepsMD: body.ReproStepsMD, ExpectedMD: body.ExpectedMD,
		ActualMD: body.ActualMD, EnvironmentMD: body.EnvironmentMD,
		Source: domain.SourceHuman,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "create failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, issue)
}

func (h *httpHandlers) getIssue(w http.ResponseWriter, r *http.Request) {
	projectKey, number, ok := splitIssueKey(chi.URLParam(r, "issueKey"))
	if !ok {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad issue key", "expected e.g. BUG-421")
		return
	}
	issue, err := h.issues.Get(r.Context(), projectKey, number)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *httpHandlers) updateIssue(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	var body struct {
		Title           *string           `json:"title"`
		DescriptionMD   *string           `json:"description_md"`
		Type            *domain.IssueType `json:"type"`
		Severity        *domain.Severity  `json:"severity"`
		Priority        *domain.Priority  `json:"priority"`
		AssigneeID      *uuid.UUID        `json:"assignee_id"`
		VersionAffected *string           `json:"version_affected"`
		VersionFixed    *string           `json:"version_fixed"`
		ReproStepsMD    *string           `json:"repro_steps_md"`
		ExpectedMD      *string           `json:"expected_md"`
		ActualMD        *string           `json:"actual_md"`
		EnvironmentMD   *string           `json:"environment_md"`
		Labels          *[]string         `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Title != nil && strings.TrimSpace(*body.Title) == "" {
		httpapi.WriteValidation(w, map[string]string{"title": "cannot be empty"})
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	updated, err := h.issues.Update(r.Context(), issue.ID, actor, UpdateIssueInput{
		Title: body.Title, DescriptionMD: body.DescriptionMD, Type: body.Type,
		Severity: body.Severity, Priority: body.Priority, AssigneeID: body.AssigneeID,
		VersionAffected: body.VersionAffected, VersionFixed: body.VersionFixed,
		ReproStepsMD: body.ReproStepsMD, ExpectedMD: body.ExpectedMD,
		ActualMD: body.ActualMD, EnvironmentMD: body.EnvironmentMD, Labels: body.Labels,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *httpHandlers) deleteIssue(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermIssueDelete) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:delete")
		return
	}
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	if err := h.issues.Delete(r.Context(), issue.ID, actor); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) transition(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermIssueTransition) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:transition")
		return
	}
	projectKey, number, ok := splitIssueKey(chi.URLParam(r, "issueKey"))
	if !ok {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad issue key", "")
		return
	}
	var body struct {
		To domain.IssueStatus `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	issue, err := h.issues.Get(r.Context(), projectKey, number)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", err.Error())
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	updated, err := h.issues.Transition(r.Context(), issue.ID, issue.Status, body.To, actor)
	if err == ErrInvalidTransition {
		httpapi.WriteProblem(w, http.StatusConflict, "invalid transition",
			string(issue.Status)+" → "+string(body.To))
		return
	}
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "transition failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *httpHandlers) listComments(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	comments, err := h.repo.ListComments(r.Context(), issue.ID, 200, 0)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func (h *httpHandlers) addComment(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermCommentCreate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing comment:create")
		return
	}
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	var body struct {
		BodyMD string `json:"body_md"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.BodyMD) == "" {
		httpapi.WriteValidation(w, map[string]string{"body_md": "required"})
		return
	}
	author, _ := uuid.Parse(p.UserID)
	c, err := h.issues.Comment(r.Context(), issue.ID, author, body.BodyMD)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "comment failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *httpHandlers) activity(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	acts, err := h.issues.Activity(r.Context(), issue.ID, 100, 0)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "activity failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, acts)
}

func (h *httpHandlers) commits(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	commits, err := h.repo.ListCommitsForIssue(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "commits failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

func (h *httpHandlers) resolveIssue(w http.ResponseWriter, r *http.Request) (domain.Issue, bool) {
	projectKey, number, ok := splitIssueKey(chi.URLParam(r, "issueKey"))
	if !ok {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad issue key", "")
		return domain.Issue{}, false
	}
	issue, err := h.issues.Get(r.Context(), projectKey, number)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", err.Error())
		return domain.Issue{}, false
	}
	return issue, true
}

// ── helpers ──

func splitIssueKey(key string) (projectKey string, number int32, ok bool) {
	i := strings.LastIndex(key, "-")
	if i <= 0 || i == len(key)-1 {
		return "", 0, false
	}
	n, err := strconv.Atoi(key[i+1:])
	if err != nil {
		return "", 0, false
	}
	return key[:i], int32(n), true
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
