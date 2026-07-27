package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/httpapi"
	"github.com/omni/bugtracker/internal/prose"
)

// NewHTTPHandlers builds the authenticated REST surface, wired to the service layer.
// This hand-written router delegates to the same services the generated strict-server
// will use post-`make generate`, so there is no business-logic duplication.
func NewHTTPHandlers(repo Repository, pub Publisher, logger *slog.Logger, cfg *config.Config) http.Handler {
	issues := NewIssues(repo, pub, logger)

	attachDir := "./data/attachments"
	maxUploadMB := int64(25)
	if cfg != nil {
		if cfg.Storage.AttachmentsDir != "" {
			attachDir = cfg.Storage.AttachmentsDir
		}
		if cfg.Storage.MaxUploadMB > 0 {
			maxUploadMB = cfg.Storage.MaxUploadMB
		}
	}
	h := &httpHandlers{issues: issues, repo: repo, pub: pub, log: logger, cfg: cfg, attachDir: attachDir, maxUpload: maxUploadMB << 20}

	r := chi.NewRouter()
	r.Get("/me", h.me)
	r.Get("/settings/archive", h.getArchiveSettings)
	r.Put("/settings/archive", h.updateArchiveSettings)
	r.Get("/me/tokens", h.listTokens)
	r.Post("/me/tokens", h.createToken)
	r.Delete("/me/tokens/{id}", h.revokeToken)
	r.Get("/me/notifications", h.listNotifications)
	r.Post("/me/notifications/read", h.markNotificationsRead)
	r.Get("/me/notification-prefs", h.getNotificationPrefs)
	r.Put("/me/notification-prefs", h.putNotificationPrefs)
	r.Get("/me/saved-searches", h.listSavedSearches)
	r.Post("/me/saved-searches", h.saveSavedSearch)
	r.Delete("/me/saved-searches/{id}", h.deleteSavedSearch)
	r.Post("/me/saved-searches/{id}/share", h.shareSavedSearch)
	r.Get("/projects/{key}/views", h.listProjectViews)
	r.Post("/projects/{key}/views", h.createProjectView)
	r.Patch("/views/{id}", h.updateProjectView)
	r.Delete("/views/{id}", h.deleteProjectView)
	r.Get("/audit", h.listAudit)
	r.Get("/ops", h.ops)
	r.Get("/users", h.users)
	r.Patch("/users/{id}/role", h.updateUserRole)
	r.Get("/dashboards/overview", h.dashboard)
	r.Get("/reports", h.reports)
	r.Get("/search", h.search)
	r.Get("/projects/{key}/board", h.getProjectBoard)
	r.Patch("/boards/{id}", h.updateBoard)
	r.Post("/boards/{id}/columns", h.createBoardColumn)
	r.Patch("/board-columns/{id}", h.updateBoardColumn)
	r.Delete("/board-columns/{id}", h.deleteBoardColumn)
	r.Get("/automation/rules", h.listAutomationRules)
	r.Post("/automation/rules", h.createAutomationRule)
	r.Patch("/automation/rules/{id}", h.updateAutomationRule)
	r.Delete("/automation/rules/{id}", h.deleteAutomationRule)
	r.Get("/automation/runs", h.listAutomationRuns)
	r.Get("/webhooks", h.listWebhooks)
	r.Post("/webhooks", h.createWebhook)
	r.Patch("/webhooks/{id}", h.updateWebhook)
	r.Delete("/webhooks/{id}", h.deleteWebhook)
	r.Get("/webhooks/{id}/deliveries", h.listWebhookDeliveries)
	r.Post("/webhooks/{id}/deliveries/{deliveryId}/redeliver", h.redeliverWebhook)
	r.Get("/projects", h.listProjects)
	r.Post("/projects", h.createProject)
	r.Get("/projects/{key}", h.getProject)
	r.Patch("/projects/{key}", h.updateProject)
	r.Post("/projects/{key}/rename-key", h.renameProjectKey)
	r.Delete("/projects/{key}", h.archiveProject)
	r.Get("/projects/{key}/labels", h.listLabels)
	r.Get("/projects/{key}/components", h.listComponents)
	r.Post("/projects/{key}/components", h.createComponent)
	r.Patch("/components/{id}", h.updateComponent)
	r.Delete("/components/{id}", h.deleteComponent)
	r.Get("/projects/{key}/sla-policies", h.listSLAPolicies)
	r.Post("/projects/{key}/sla-policies", h.createSLAPolicy)
	r.Patch("/sla-policies/{id}", h.updateSLAPolicy)
	r.Delete("/sla-policies/{id}", h.deleteSLAPolicy)
	r.Get("/projects/{key}/templates", h.listIssueTemplates)
	r.Post("/projects/{key}/templates", h.createIssueTemplate)
	r.Patch("/templates/{id}", h.updateIssueTemplate)
	r.Delete("/templates/{id}", h.deleteIssueTemplate)
	r.Get("/projects/{key}/fields", h.listFieldDefinitions)
	r.Post("/projects/{key}/fields", h.createFieldDefinition)
	r.Patch("/fields/{id}", h.updateFieldDefinition)
	r.Delete("/fields/{id}", h.deleteFieldDefinition)
	r.Get("/issues/{issueKey}/fields", h.listIssueFields)
	r.Put("/issues/{issueKey}/fields", h.setIssueFields)
	r.Get("/projects/{key}/iterations", h.listIterations)
	r.Post("/projects/{key}/iterations", h.createIteration)
	r.Get("/projects/{key}/velocity", h.iterationVelocity)
	r.Patch("/iterations/{id}", h.updateIteration)
	r.Delete("/iterations/{id}", h.deleteIteration)
	r.Get("/iterations/{id}/burndown", h.iterationBurndown)
	r.Post("/iterations/{id}/carry-over", h.carryOverIteration)
	r.Put("/issues/{issueKey}/iteration", h.setIssueIteration)
	r.Get("/projects/{key}/milestones", h.listMilestones)
	r.Post("/projects/{key}/milestones", h.createMilestone)
	r.Patch("/milestones/{id}", h.updateMilestone)
	r.Delete("/milestones/{id}", h.deleteMilestone)
	r.Get("/projects/{key}/releases", h.listReleases)
	r.Post("/projects/{key}/releases", h.createRelease)
	r.Patch("/releases/{id}", h.updateRelease)
	r.Get("/releases/{id}/notes", h.releaseNotes)
	r.Post("/releases/{id}/notes", h.releaseNotes)
	r.Delete("/releases/{id}", h.deleteRelease)
	r.Get("/projects/{key}/members", h.listProjectMembers)
	r.Put("/projects/{key}/members/{id}", h.putProjectMember)
	r.Delete("/projects/{key}/members/{id}", h.deleteProjectMember)
	r.Get("/projects/{key}/issues", h.listIssues)
	r.Post("/projects/{key}/issues", h.createIssue)
	r.Get("/projects/{key}/issues/export", h.exportIssues)
	r.Get("/projects/{key}/issues/similar", h.similarIssues)
	r.Post("/issues/bulk", h.bulkUpdateIssues)
	// Project-less list: the same filter grammar across every project the caller can
	// see. Registered before the {issueKey} route for readability; chi matches the
	// static path either way.
	r.Get("/issues", h.listAllIssues)
	r.Get("/issues/{issueKey}", h.getIssue)
	r.Patch("/issues/{issueKey}", h.updateIssue)
	r.Delete("/issues/{issueKey}", h.deleteIssue)
	r.Post("/issues/{issueKey}/transition", h.transition)
	r.Post("/issues/{issueKey}/move", h.moveIssue)
	r.Post("/issues/{issueKey}/rank", h.rankIssue)
	r.Post("/issues/{issueKey}/archive", h.archiveIssue)
	r.Post("/issues/{issueKey}/unarchive", h.unarchiveIssue)
	r.Post("/issues/{issueKey}/read", h.markIssueRead)
	r.Post("/issues/{issueKey}/mute", h.muteIssue)
	r.Delete("/issues/{issueKey}/mute", h.unmuteIssue)
	r.Post("/issues/{issueKey}/snooze", h.snoozeIssue)
	r.Delete("/issues/{issueKey}/snooze", h.wakeIssue)
	r.Get("/issues/{issueKey}/time", h.listTimeEntries)
	r.Post("/issues/{issueKey}/time", h.logTime)
	r.Delete("/time-entries/{id}", h.deleteTimeEntry)
	r.Get("/issues/{issueKey}/comments", h.listComments)
	r.Post("/issues/{issueKey}/comments", h.addComment)
	r.Patch("/comments/{id}", h.updateComment)
	r.Delete("/comments/{id}", h.deleteComment)
	r.Get("/issues/{issueKey}/relations", h.listRelations)
	r.Post("/issues/{issueKey}/relations", h.addRelation)
	r.Delete("/relations/{id}", h.deleteRelation)
	r.Get("/issues/{issueKey}/references", h.listReferences)
	r.Get("/issues/{issueKey}/reactions", h.listReactions)
	r.Post("/issues/{issueKey}/reactions", h.toggleReaction)
	r.Get("/issues/{issueKey}/watchers", h.listWatchers)
	r.Put("/issues/{issueKey}/watchers/me", h.watchIssue)
	r.Delete("/issues/{issueKey}/watchers/me", h.unwatchIssue)
	r.Get("/issues/{issueKey}/attachments", h.listAttachments)
	r.Post("/issues/{issueKey}/attachments", h.uploadAttachment)
	r.Get("/attachments/{id}", h.downloadAttachment)
	r.Delete("/attachments/{id}", h.deleteAttachment)
	r.Get("/issues/{issueKey}/activity", h.activity)
	r.Get("/issues/{issueKey}/commits", h.commits)
	return r
}

type httpHandlers struct {
	issues    *Issues
	repo      Repository
	pub       Publisher
	log       *slog.Logger
	cfg       *config.Config // bootstrap defaults (e.g. archive fallback)
	attachDir string         // local-disk attachment storage root
	maxUpload int64          // bytes
}

// canOnProject is the elevation-aware permission check: the principal passes
// if their global role grants the permission OR their project_members role in
// this project does. Global owner/admin therefore always pass.
func (h *httpHandlers) canOnProject(ctx context.Context, p *auth.Principal, projectKey string, perm auth.Permission) bool {
	if p.Can(perm) {
		return true
	}
	uid, err := uuid.Parse(p.UserID)
	if err != nil {
		return false
	}
	role, ok, err := h.repo.GetProjectRole(ctx, projectKey, uid)
	if err != nil || !ok {
		return false
	}
	// Membership elevates the role but must not escape the token's scopes — otherwise
	// a narrowly-scoped token would regain full rights inside any project it belongs to.
	return auth.RoleCan(role, perm) && p.ScopeAllows(perm)
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
	// Scopes narrow the token below the owner's role; an unknown scope would
	// silently deny everything, so reject it up front. Empty = unrestricted.
	for _, s := range body.Scopes {
		if !auth.ValidScope(s) {
			httpapi.WriteValidation(w, map[string]string{
				"scopes": "unknown scope " + strconv.Quote(s) + " — expected one of " + strings.Join(scopeNames(), ", ") + `, or "*"`,
			})
			return
		}
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
	// Scopes and name only — the plaintext above never reaches the log.
	h.audit(r, AuditTokenCreated, "api_token", tok.ID.String(), tok.Name,
		map[string]any{"scopes": tok.Scopes})
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
	h.audit(r, AuditTokenRevoked, "api_token", chi.URLParam(r, "id"), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

// ── saved searches (personal) ──

func (h *httpHandlers) listSavedSearches(w http.ResponseWriter, r *http.Request) {
	uid, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	items, err := h.repo.ListSavedSearches(r.Context(), uid)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.SavedSearch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *httpHandlers) saveSavedSearch(w http.ResponseWriter, r *http.Request) {
	uid, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	var body struct {
		Name  string `json:"name"`
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	fields := map[string]string{}
	if strings.TrimSpace(body.Name) == "" {
		fields["name"] = "required"
	}
	if strings.TrimSpace(body.Query) == "" {
		fields["query"] = "required"
	}
	if len(fields) > 0 {
		httpapi.WriteValidation(w, fields)
		return
	}
	ss, err := h.repo.UpsertSavedSearch(r.Context(), uid, body.Name, body.Query)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "save failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ss)
}

func (h *httpHandlers) deleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	uid, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad id", "")
		return
	}
	ok, err := h.repo.DeleteSavedSearch(r.Context(), uid, id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such saved search")
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
	h.audit(r, AuditUserRoleChanged, "user", id.String(), user.Email,
		map[string]any{"role": body.Role})
	writeJSON(w, http.StatusOK, user)
}

func scopeNames() []string {
	out := make([]string, 0, len(auth.AllPermissions))
	for _, p := range auth.AllPermissions {
		out = append(out, string(p))
	}
	return out
}

func validRole(role string) bool {
	switch domain.Role(role) {
	case domain.RoleOwner, domain.RoleAdmin, domain.RoleMaintainer, domain.RoleMember, domain.RoleReporter, domain.RoleBot:
		return true
	default:
		return false
	}
}

// getArchiveSettings / updateArchiveSettings expose the auto-archive window as a
// runtime setting (admin only) so it can be toggled from the UI without a redeploy.
func (h *httpHandlers) getArchiveSettings(w http.ResponseWriter, r *http.Request) {
	if !auth.FromContext(r.Context()).Can(auth.PermAdmin) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing admin:all")
		return
	}
	days, err := EffectiveArchiveDays(r.Context(), h.repo, h.cfg)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "read failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_after_days": days})
}

func (h *httpHandlers) updateArchiveSettings(w http.ResponseWriter, r *http.Request) {
	if !auth.FromContext(r.Context()).Can(auth.PermAdmin) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing admin:all")
		return
	}
	var body struct {
		AutoAfterDays int `json:"auto_after_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.AutoAfterDays < 0 {
		httpapi.WriteValidation(w, map[string]string{"auto_after_days": "must be 0 (off) or a positive number of days"})
		return
	}
	if err := SetArchiveDays(r.Context(), h.repo, body.AutoAfterDays); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "save failed", err.Error())
		return
	}
	// Enabling it runs a sweep now so it doesn't wait for the daily tick; best-effort.
	if body.AutoAfterDays > 0 {
		_ = h.pub.EnqueueAutoArchive(r.Context())
	}
	h.audit(r, AuditSettingsUpdated, "settings", "archive", "auto-archive",
		map[string]any{"auto_after_days": body.AutoAfterDays})
	writeJSON(w, http.StatusOK, map[string]any{"auto_after_days": body.AutoAfterDays})
}

// dashboard aggregates health metrics. `?project=KEY` scopes every figure to one
// project — the UI labels this view per-project, so without a scope the numbers
// would silently span the whole install. No project = the cross-project rollup.
func (h *httpHandlers) dashboard(w http.ResponseWriter, r *http.Request) {
	projectKey := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("project")))
	if projectKey != "" {
		if _, err := h.repo.GetProjectByKey(r.Context(), projectKey); err != nil {
			httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project: "+projectKey)
			return
		}
	}
	d, err := h.repo.Dashboard(r.Context(), projectKey)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "dashboard failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// ── boards (configurable Kanban) ──

var validBoardStatuses = map[string]bool{
	"open": true, "in_progress": true, "blocked": true, "ready_for_review": true,
	"resolved": true, "closed": true, "reopened": true,
}
var validSwimlanes = map[string]bool{"none": true, "assignee": true, "priority": true}

func validStatusList(statuses []string) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, s := range statuses {
		if !validBoardStatuses[s] {
			return false
		}
	}
	return true
}

func (h *httpHandlers) getProjectBoard(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if _, err := h.repo.GetProjectByKey(r.Context(), key); err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	board, err := h.repo.GetOrCreateBoard(r.Context(), key)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "board failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// authorizeBoardManage resolves the owning project of a board/board_column and
// checks project:manage with membership elevation.
func (h *httpHandlers) authorizeBoardManage(w http.ResponseWriter, r *http.Request, entity string) (uuid.UUID, bool) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad id", "")
		return uuid.Nil, false
	}
	key, err := h.repo.ProjectKeyForEntity(r.Context(), entity, id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such "+entity)
		return uuid.Nil, false
	}
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return uuid.Nil, false
	}
	return id, true
}

func (h *httpHandlers) updateBoard(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeBoardManage(w, r, "board")
	if !ok {
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Swimlane *string `json:"swimlane"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Swimlane != nil && !validSwimlanes[*body.Swimlane] {
		httpapi.WriteValidation(w, map[string]string{"swimlane": "must be none, assignee, or priority"})
		return
	}
	board, err := h.repo.UpdateBoard(r.Context(), id, body.Name, body.Swimlane)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (h *httpHandlers) createBoardColumn(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeBoardManage(w, r, "board")
	if !ok {
		return
	}
	var body struct {
		Name     string   `json:"name"`
		Statuses []string `json:"statuses"`
		WipLimit *int     `json:"wip_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	fields := map[string]string{}
	if strings.TrimSpace(body.Name) == "" {
		fields["name"] = "required"
	}
	if !validStatusList(body.Statuses) {
		fields["statuses"] = "non-empty list of workflow statuses"
	}
	if len(fields) > 0 {
		httpapi.WriteValidation(w, fields)
		return
	}
	board, err := h.repo.CreateBoardColumn(r.Context(), id, BoardColumnInput{
		Name: body.Name, Statuses: body.Statuses, WipLimit: body.WipLimit,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "create failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, board)
}

func (h *httpHandlers) updateBoardColumn(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeBoardManage(w, r, "board_column")
	if !ok {
		return
	}
	var body struct {
		Name     *string   `json:"name"`
		Statuses *[]string `json:"statuses"`
		WipLimit *int      `json:"wip_limit"` // -1 clears the limit
		Position *int      `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Statuses != nil && !validStatusList(*body.Statuses) {
		httpapi.WriteValidation(w, map[string]string{"statuses": "non-empty list of workflow statuses"})
		return
	}
	in := UpdateBoardColumnInput{Name: body.Name, Statuses: body.Statuses, Position: body.Position}
	if body.WipLimit != nil {
		if *body.WipLimit < 0 {
			in.ClearWip = true
		} else {
			in.WipLimit = body.WipLimit
		}
	}
	board, err := h.repo.UpdateBoardColumn(r.Context(), id, in)
	if err != nil {
		writeNotFoundOrError(w, err, "column", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (h *httpHandlers) deleteBoardColumn(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeBoardManage(w, r, "board_column")
	if !ok {
		return
	}
	board, found, err := h.repo.DeleteBoardColumn(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !found {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such column")
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// ── automation rules ──

var validActionKinds = map[string]bool{
	"set_priority": true, "set_severity": true, "set_assignee": true,
	"add_label": true, "set_status": true, "add_comment": true,
}

func (h *httpHandlers) requireAutomationEdit(w http.ResponseWriter, r *http.Request) bool {
	if !auth.FromContext(r.Context()).Can(auth.PermAutomationEdit) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing automation:edit")
		return false
	}
	return true
}

// validateRulePayload checks trigger/actions shape shared by create and update.
func validateRulePayload(trigger, actions json.RawMessage) map[string]string {
	fields := map[string]string{}
	if trigger != nil {
		var t struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal(trigger, &t); err != nil || strings.TrimSpace(t.Event) == "" {
			fields["trigger"] = `must be {"event": "...", "conditions": {...}} (event required, "*" = any)`
		}
	}
	if actions != nil {
		var acts []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(actions, &acts); err != nil || len(acts) == 0 {
			fields["actions"] = "must be a non-empty array of {kind, value}"
		} else {
			for _, a := range acts {
				if !validActionKinds[a.Kind] {
					fields["actions"] = "unknown kind " + a.Kind
				} else if strings.TrimSpace(a.Value) == "" {
					fields["actions"] = a.Kind + " needs a value"
				}
			}
		}
	}
	return fields
}

func (h *httpHandlers) listAutomationRules(w http.ResponseWriter, r *http.Request) {
	if !h.requireAutomationEdit(w, r) {
		return
	}
	items, err := h.repo.ListAutomationRules(r.Context())
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.AutomationRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *httpHandlers) createAutomationRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireAutomationEdit(w, r) {
		return
	}
	var body struct {
		Name       string          `json:"name"`
		ProjectKey string          `json:"project_key"`
		Priority   int             `json:"priority"`
		Trigger    json.RawMessage `json:"trigger"`
		Actions    json.RawMessage `json:"actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	fields := validateRulePayload(body.Trigger, body.Actions)
	if strings.TrimSpace(body.Name) == "" {
		fields["name"] = "required"
	}
	if body.Trigger == nil {
		fields["trigger"] = "required"
	}
	if body.Actions == nil {
		fields["actions"] = "required"
	}
	if len(fields) > 0 {
		httpapi.WriteValidation(w, fields)
		return
	}
	if body.ProjectKey != "" {
		if _, err := h.repo.GetProjectByKey(r.Context(), body.ProjectKey); err != nil {
			httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
			return
		}
	}
	if body.Priority == 0 {
		body.Priority = 100
	}
	creator, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	rule, err := h.repo.CreateAutomationRule(r.Context(), CreateAutomationRuleInput{
		ProjectKey: body.ProjectKey, Name: body.Name, Priority: body.Priority,
		Trigger: body.Trigger, Actions: body.Actions, CreatedBy: creator,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "create failed", err.Error())
		return
	}
	h.audit(r, AuditRuleCreated, "automation_rule", rule.ID.String(), rule.Name,
		map[string]any{"project": rule.ProjectKey, "active": rule.IsActive})
	writeJSON(w, http.StatusCreated, rule)
}

func (h *httpHandlers) updateAutomationRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireAutomationEdit(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad rule id", "")
		return
	}
	var body struct {
		Name     *string         `json:"name"`
		Priority *int            `json:"priority"`
		IsActive *bool           `json:"is_active"`
		Trigger  json.RawMessage `json:"trigger"`
		Actions  json.RawMessage `json:"actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if fields := validateRulePayload(body.Trigger, body.Actions); len(fields) > 0 {
		httpapi.WriteValidation(w, fields)
		return
	}
	rule, err := h.repo.UpdateAutomationRule(r.Context(), UpdateAutomationRuleInput{
		ID: id, Name: body.Name, Priority: body.Priority, IsActive: body.IsActive,
		Trigger: body.Trigger, Actions: body.Actions,
	})
	if err != nil {
		writeNotFoundOrError(w, err, "rule", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *httpHandlers) deleteAutomationRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireAutomationEdit(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad rule id", "")
		return
	}
	ok, err := h.repo.DeleteAutomationRule(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such rule")
		return
	}
	h.audit(r, AuditRuleDeleted, "automation_rule", chi.URLParam(r, "id"), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) listAutomationRuns(w http.ResponseWriter, r *http.Request) {
	if !h.requireAutomationEdit(w, r) {
		return
	}
	items, err := h.repo.ListAutomationRuns(r.Context(), 25)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.AutomationRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ── webhooks (outbound event subscriptions) ──

func (h *httpHandlers) requireWebhookEdit(w http.ResponseWriter, r *http.Request) bool {
	if !auth.FromContext(r.Context()).Can(auth.PermWebhookEdit) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing webhook:edit")
		return false
	}
	return true
}

func (h *httpHandlers) listWebhooks(w http.ResponseWriter, r *http.Request) {
	if !h.requireWebhookEdit(w, r) {
		return
	}
	items, err := h.repo.ListWebhooks(r.Context())
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Webhook{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *httpHandlers) createWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.requireWebhookEdit(w, r) {
		return
	}
	var body struct {
		URL        string   `json:"url"`
		Secret     string   `json:"secret"`
		Events     []string `json:"events"`
		ProjectKey string   `json:"project_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if !strings.HasPrefix(body.URL, "http://") && !strings.HasPrefix(body.URL, "https://") {
		httpapi.WriteValidation(w, map[string]string{"url": "must be an http(s) URL"})
		return
	}
	if body.ProjectKey != "" {
		if _, err := h.repo.GetProjectByKey(r.Context(), body.ProjectKey); err != nil {
			httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
			return
		}
	}
	creator, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	wh, err := h.repo.CreateWebhook(r.Context(), CreateWebhookInput{
		ProjectKey: body.ProjectKey, URL: body.URL, Secret: body.Secret,
		Events: body.Events, CreatedBy: creator,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "create failed", err.Error())
		return
	}
	// URL and event filter, never the signing secret.
	h.audit(r, AuditWebhookCreated, "webhook", wh.ID.String(), wh.URL,
		map[string]any{"events": wh.Events, "project": wh.ProjectKey, "has_secret": wh.HasSecret})
	writeJSON(w, http.StatusCreated, wh)
}

func (h *httpHandlers) updateWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.requireWebhookEdit(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad webhook id", "")
		return
	}
	var body struct {
		URL      *string   `json:"url"`
		Secret   *string   `json:"secret"`
		Events   *[]string `json:"events"`
		IsActive *bool     `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.URL != nil && !strings.HasPrefix(*body.URL, "http://") && !strings.HasPrefix(*body.URL, "https://") {
		httpapi.WriteValidation(w, map[string]string{"url": "must be an http(s) URL"})
		return
	}
	wh, err := h.repo.UpdateWebhook(r.Context(), UpdateWebhookInput{
		ID: id, URL: body.URL, Secret: body.Secret, Events: body.Events, IsActive: body.IsActive,
	})
	if err != nil {
		writeNotFoundOrError(w, err, "webhook", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, wh)
}

func (h *httpHandlers) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.requireWebhookEdit(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad webhook id", "")
		return
	}
	ok, err := h.repo.DeleteWebhook(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such webhook")
		return
	}
	h.audit(r, AuditWebhookDeleted, "webhook", chi.URLParam(r, "id"), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if !h.requireWebhookEdit(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad webhook id", "")
		return
	}
	items, err := h.repo.ListWebhookDeliveries(r.Context(), id, 25)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.WebhookDelivery{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// redeliverWebhook re-enqueues a past delivery with its original payload.
func (h *httpHandlers) redeliverWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.requireWebhookEdit(w, r) {
		return
	}
	hookID, err1 := uuid.Parse(chi.URLParam(r, "id"))
	deliveryID, err2 := uuid.Parse(chi.URLParam(r, "deliveryId"))
	if err1 != nil || err2 != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad id", "")
		return
	}
	d, err := h.repo.GetWebhookDelivery(r.Context(), deliveryID)
	if err != nil || d.WebhookID != hookID {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such delivery")
		return
	}
	if err := h.repo.ResetWebhookDelivery(r.Context(), deliveryID); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "redeliver failed", err.Error())
		return
	}
	if err := h.pub.EnqueueWebhook(r.Context(), events.WebhookJobArgs{
		WebhookID: hookID.String(), DeliveryID: deliveryID.String(),
		EventType: d.EventType, Payload: d.Payload,
	}); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "redeliver failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// search is global full-text search across projects (issues + comments).
func (h *httpHandlers) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		httpapi.WriteValidation(w, map[string]string{"q": "at least 2 characters"})
		return
	}
	limit := int32(atoiDefault(r.URL.Query().Get("limit"), 20))
	if limit > 50 {
		limit = 50
	}
	hits, err := h.repo.Search(r.Context(), q, limit)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "search failed", err.Error())
		return
	}
	if hits == nil {
		hits = []domain.SearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits, "total": len(hits), "source": "postgres-fts"})
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
	key := chi.URLParam(r, "key")
	project, err := h.repo.GetProjectByKey(r.Context(), key)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	// Surface the caller's effective role so the UI can gate management
	// affordances: project membership elevates non-admin global roles.
	p := auth.FromContext(r.Context())
	project.MyRole = p.Role
	if !p.Can(auth.PermAdmin) {
		if uid, err := uuid.Parse(p.UserID); err == nil {
			if role, ok, _ := h.repo.GetProjectRole(r.Context(), key, uid); ok {
				project.MyRole = role
			}
		}
	}
	writeJSON(w, http.StatusOK, project)
}

// ── project members ──

func (h *httpHandlers) listProjectMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.repo.ListProjectMembers(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if members == nil {
		members = []domain.ProjectMember{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": members})
}

func (h *httpHandlers) putProjectMember(w http.ResponseWriter, r *http.Request) {
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
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad user id", "")
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
	m, err := h.repo.UpsertProjectMember(r.Context(), key, uid, domain.Role(body.Role))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "add member failed", err.Error())
		return
	}
	h.audit(r, AuditMemberSet, "project_member", uid.String(), m.User.Email,
		map[string]any{"project": key, "role": body.Role})
	writeJSON(w, http.StatusOK, m)
}

func (h *httpHandlers) deleteProjectMember(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad user id", "")
		return
	}
	ok, err := h.repo.RemoveProjectMember(r.Context(), key, uid)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "remove failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "not a member")
		return
	}
	h.audit(r, AuditMemberRemoved, "project_member", chi.URLParam(r, "id"), "",
		map[string]any{"project": chi.URLParam(r, "key")})
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
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
		Name              *string    `json:"name"`
		DescriptionMD     *string    `json:"description_md"`
		IsArchived        *bool      `json:"is_archived"`
		DefaultAssigneeID *uuid.UUID `json:"default_assignee_id"`
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
		DefaultAssigneeID: body.DefaultAssigneeID,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	h.audit(r, AuditProjectUpdated, "project", project.Key, project.Name, nil)
	writeJSON(w, http.StatusOK, project)
}

// renameProjectKey changes a project's key (e.g. BUG → TRACK). Issue keys are derived
// from the project key, so every issue re-labels automatically (BUG-42 → TRACK-42) and
// no issue rows are rewritten. External references to the old key (git commits,
// bookmarks) won't resolve afterwards — that's inherent to renaming a key.
func (h *httpHandlers) renameProjectKey(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	key := chi.URLParam(r, "key")
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return
	}
	var body struct {
		NewKey string `json:"new_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	newKey := strings.ToUpper(strings.TrimSpace(body.NewKey))
	if !projectKeyRe.MatchString(newKey) {
		httpapi.WriteValidation(w, map[string]string{"new_key": "must be 2–10 uppercase letters/digits, starting with a letter"})
		return
	}
	if newKey == key {
		httpapi.WriteProblem(w, http.StatusConflict, "no change", "project already has key "+key)
		return
	}
	if _, err := h.repo.GetProjectByKey(r.Context(), key); err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project")
		return
	}
	project, err := h.repo.RenameProjectKey(r.Context(), key, newKey)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "rename failed",
			"a project with key "+newKey+" may already exist: "+err.Error())
		return
	}
	// Every issue key in the project changed with it — worth recording both sides.
	h.audit(r, AuditProjectKeyRenamed, "project", newKey, newKey,
		map[string]any{"from": key, "to": newKey})
	writeJSON(w, http.StatusOK, project)
}

func (h *httpHandlers) archiveProject(w http.ResponseWriter, r *http.Request) {
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
	archived := true
	if _, err := h.repo.UpdateProject(r.Context(), UpdateProjectInput{Key: key, IsArchived: &archived}); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "archive failed", err.Error())
		return
	}
	h.audit(r, AuditProjectArchived, "project", key, key, nil)
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

// authorizeEntityManage parses {id}, resolves the owning project of an
// id-addressed component/milestone/release, and checks project:manage with
// membership elevation. Writes the error response itself on failure.
func (h *httpHandlers) authorizeEntityManage(w http.ResponseWriter, r *http.Request, entity string) (uuid.UUID, bool) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad "+entity+" id", "")
		return uuid.Nil, false
	}
	key, err := h.repo.ProjectKeyForEntity(r.Context(), entity, id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such "+entity)
		return uuid.Nil, false
	}
	if !h.canOnProject(r.Context(), p, key, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing project:manage")
		return uuid.Nil, false
	}
	return id, true
}

// ── components ──

func (h *httpHandlers) listComponents(w http.ResponseWriter, r *http.Request) {
	components, err := h.repo.ListComponents(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if components == nil {
		components = []domain.Component{}
	}
	if effort, err := h.repo.ComponentEffort(r.Context(), chi.URLParam(r, "key")); err == nil {
		for i := range components {
			components[i].Effort = effort[components[i].ID]
		}
	} else {
		h.log.Warn("component effort rollup failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": components})
}

func (h *httpHandlers) createComponent(w http.ResponseWriter, r *http.Request) {
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
		Name          string     `json:"name"`
		DescriptionMD string     `json:"description_md"`
		LeadID        *uuid.UUID `json:"lead_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		httpapi.WriteValidation(w, map[string]string{"name": "required"})
		return
	}
	c, err := h.repo.CreateComponent(r.Context(), CreateComponentInput{
		ProjectKey: key, Name: body.Name, DescriptionMD: body.DescriptionMD, LeadID: body.LeadID,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a component with that name may already exist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *httpHandlers) updateComponent(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "component")
	if !ok {
		return
	}
	var body struct {
		Name          *string    `json:"name"`
		DescriptionMD *string    `json:"description_md"`
		LeadID        *uuid.UUID `json:"lead_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		httpapi.WriteValidation(w, map[string]string{"name": "cannot be empty"})
		return
	}
	c, err := h.repo.UpdateComponent(r.Context(), UpdateComponentInput{
		ID: id, Name: body.Name, DescriptionMD: body.DescriptionMD, LeadID: body.LeadID,
	})
	if err != nil {
		writeNotFoundOrError(w, err, "component", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *httpHandlers) deleteComponent(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "component")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteComponent(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such component")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── milestones ──

// parseDueOn accepts "YYYY-MM-DD" and returns nil for empty input.
func parseDueOn(s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (h *httpHandlers) listMilestones(w http.ResponseWriter, r *http.Request) {
	milestones, err := h.repo.ListMilestones(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if milestones == nil {
		milestones = []domain.Milestone{}
	}
	// Rollups are attached here rather than joined into the list query: one grouped
	// query for the whole page, and the milestone SQL stays about milestones.
	// A rollup failure degrades to zeroes rather than failing the list — planning
	// numbers are useful, but not at the price of the page not loading.
	if effort, err := h.repo.MilestoneEffort(r.Context(), chi.URLParam(r, "key")); err == nil {
		for i := range milestones {
			milestones[i].Effort = effort[milestones[i].ID]
		}
	} else {
		h.log.Warn("milestone effort rollup failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": milestones})
}

func (h *httpHandlers) createMilestone(w http.ResponseWriter, r *http.Request) {
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
		Title         string `json:"title"`
		DescriptionMD string `json:"description_md"`
		DueOn         string `json:"due_on"` // YYYY-MM-DD, optional
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		httpapi.WriteValidation(w, map[string]string{"title": "required"})
		return
	}
	dueOn, err := parseDueOn(body.DueOn)
	if err != nil {
		httpapi.WriteValidation(w, map[string]string{"due_on": "expected YYYY-MM-DD"})
		return
	}
	m, err := h.repo.CreateMilestone(r.Context(), CreateMilestoneInput{
		ProjectKey: key, Title: body.Title, DescriptionMD: body.DescriptionMD, DueOn: dueOn,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a milestone with that title may already exist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (h *httpHandlers) updateMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "milestone")
	if !ok {
		return
	}
	var body struct {
		Title         *string `json:"title"`
		DescriptionMD *string `json:"description_md"`
		DueOn         *string `json:"due_on"` // nil = unchanged, "" = clear, else YYYY-MM-DD
		State         *string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.State != nil && *body.State != "open" && *body.State != "closed" {
		httpapi.WriteValidation(w, map[string]string{"state": "must be open or closed"})
		return
	}
	in := UpdateMilestoneInput{ID: id, Title: body.Title, DescriptionMD: body.DescriptionMD, State: body.State}
	if body.DueOn != nil {
		if strings.TrimSpace(*body.DueOn) == "" {
			in.ClearDueOn = true
		} else {
			dueOn, err := parseDueOn(*body.DueOn)
			if err != nil {
				httpapi.WriteValidation(w, map[string]string{"due_on": "expected YYYY-MM-DD"})
				return
			}
			in.DueOn = dueOn
		}
	}
	m, err := h.repo.UpdateMilestone(r.Context(), in)
	if err != nil {
		writeNotFoundOrError(w, err, "milestone", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *httpHandlers) deleteMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "milestone")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteMilestone(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such milestone")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── releases ──

func (h *httpHandlers) listReleases(w http.ResponseWriter, r *http.Request) {
	releases, err := h.repo.ListReleases(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if releases == nil {
		releases = []domain.Release{}
	}
	if effort, err := h.repo.ReleaseEffort(r.Context(), chi.URLParam(r, "key")); err == nil {
		for i := range releases {
			releases[i].Effort = effort[releases[i].ID]
		}
	} else {
		h.log.Warn("release effort rollup failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": releases})
}

func (h *httpHandlers) createRelease(w http.ResponseWriter, r *http.Request) {
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
		Version string `json:"version"`
		Name    string `json:"name"`
		NotesMD string `json:"notes_md"`
		GitTag  string `json:"git_tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Version) == "" {
		httpapi.WriteValidation(w, map[string]string{"version": "required"})
		return
	}
	creator, _ := uuid.Parse(p.UserID)
	rel, err := h.repo.CreateRelease(r.Context(), CreateReleaseInput{
		ProjectKey: key, Version: body.Version, Name: body.Name, NotesMD: body.NotesMD,
		GitTag: body.GitTag, CreatedBy: creator,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "create failed",
			"a release with that version may already exist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rel)
}

func (h *httpHandlers) updateRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "release")
	if !ok {
		return
	}
	var body struct {
		Version *string `json:"version"`
		Name    *string `json:"name"`
		NotesMD *string `json:"notes_md"`
		GitTag  *string `json:"git_tag"`
		State   *string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.State != nil && *body.State != "draft" && *body.State != "published" {
		httpapi.WriteValidation(w, map[string]string{"state": "must be draft or published"})
		return
	}
	if body.Version != nil && strings.TrimSpace(*body.Version) == "" {
		httpapi.WriteValidation(w, map[string]string{"version": "cannot be empty"})
		return
	}
	rel, err := h.repo.UpdateRelease(r.Context(), UpdateReleaseInput{
		ID: id, Version: body.Version, Name: body.Name, NotesMD: body.NotesMD,
		GitTag: body.GitTag, State: body.State,
	})
	if err != nil {
		writeNotFoundOrError(w, err, "release", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

func (h *httpHandlers) deleteRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := h.authorizeEntityManage(w, r, "release")
	if !ok {
		return
	}
	deleted, err := h.repo.DeleteRelease(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !deleted {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such release")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listAllIssues is listIssues without a project scope, for the cross-project queue.
// People have one attention span across many projects; the tracker having one page that
// matches is the difference between a system of record and somewhere you start the day.
func (h *httpHandlers) listAllIssues(w http.ResponseWriter, r *http.Request) {
	h.issueList(w, r, "")
}

func (h *httpHandlers) listIssues(w http.ResponseWriter, r *http.Request) {
	h.issueList(w, r, chi.URLParam(r, "key"))
}

func (h *httpHandlers) issueList(w http.ResponseWriter, r *http.Request, key string) {
	p := auth.FromContext(r.Context())
	f, badTerms := ParseFilter(key, r.URL.Query().Get("filter"), p.UserID)
	// A typo in the filter box is the caller's mistake, not a server fault: unknown
	// enum values would otherwise reach Postgres and fail the whole query as a 500.
	if len(badTerms) > 0 {
		httpapi.WriteValidation(w, badTerms)
		return
	}
	h.resolveIterationFilter(r, &f)
	if problems := h.resolveFieldFilters(r, &f); len(problems) > 0 {
		httpapi.WriteValidation(w, problems)
		return
	}
	f.Sort = r.URL.Query().Get("sort")
	f.Limit = int32(atoiDefault(r.URL.Query().Get("limit"), 50))
	// Paging: `total` in the response is the unpaged count, so clients page with
	// offset until they've collected `total` items.
	f.Offset = int32(atoiDefault(r.URL.Query().Get("offset"), 0))

	items, total, err := h.issues.List(r.Context(), f)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Issue{} // an empty page is [], not null, like every other list
	}
	// Effort covers the whole filtered set, not the page — "3 days estimated" that
	// silently meant "on this screen" would be worse than no number at all. Degrades
	// to zeroes rather than failing the list.
	effort, err := h.repo.EffortForIssues(r.Context(), f)
	if err != nil {
		h.log.Warn("issue effort rollup failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "effort": effort})
}

func (h *httpHandlers) createIssue(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !h.canOnProject(r.Context(), p, chi.URLParam(r, "key"), auth.PermIssueCreate) {
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
		DueAt           string           `json:"due_at"`
		Estimate        string           `json:"estimate"`
		Fields          map[string]any   `json:"fields"`
		TemplateID      string           `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		httpapi.WriteValidation(w, map[string]string{"title": "required"})
		return
	}
	dueAt, _, err := parseDueAt(body.DueAt)
	if err != nil {
		httpapi.WriteValidation(w, map[string]string{"due_at": err.Error()})
		return
	}
	estimate, err := parseEstimate(body.Estimate)
	if err != nil {
		httpapi.WriteValidation(w, map[string]string{"estimate": err.Error()})
		return
	}
	// Template defaults and required sections. Applied before validation so a default
	// the filer never touched still counts as their answer.
	projectKey := chi.URLParam(r, "key")
	if body.TemplateID != "" {
		tmplID, err := uuid.Parse(body.TemplateID)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"template_id": "expected a template uuid"})
			return
		}
		tmpl, err := h.repo.GetIssueTemplate(r.Context(), tmplID)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"template_id": "no such template"})
			return
		}
		if tmpl.ProjectKey != projectKey {
			httpapi.WriteValidation(w, map[string]string{
				"template_id": "that template belongs to a different project",
			})
			return
		}
		if missing := prose.MissingSections(body.DescriptionMD, tmpl.RequiredSections); len(missing) > 0 {
			// 422 naming the headings, the same shape the filter parser returns —
			// "validation failed" with no clue which section is the reason people
			// paste the whole template back in and try again.
			httpapi.WriteValidation(w, map[string]string{
				"description_md": "the " + tmpl.Name + " template requires these sections, filled in: " +
					strings.Join(missing, ", "),
			})
			return
		}
		applyTemplateDefaults(&body.Labels, &body.Components, &body.Priority, &body.Severity, tmpl)
	}
	defs, err := h.repo.ListFieldDefinitions(r.Context(), projectKey)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "load fields failed", err.Error())
		return
	}
	if len(defs) > 0 {
		if problems := ValidateFieldValues(defs, body.Type, body.Fields, true); len(problems) > 0 {
			httpapi.WriteValidation(w, problems)
			return
		}
	}
	reporter, _ := uuid.Parse(p.UserID)
	issue, err := h.issues.Create(r.Context(), CreateIssueInput{
		ProjectKey: projectKey, Type: body.Type, Title: body.Title,
		DescriptionMD: body.DescriptionMD, Severity: body.Severity, Priority: body.Priority,
		ReporterID: reporter, AssigneeID: body.AssigneeID, Labels: body.Labels,
		Components: body.Components, VersionAffected: body.VersionAffected,
		ReproStepsMD: body.ReproStepsMD, ExpectedMD: body.ExpectedMD,
		ActualMD: body.ActualMD, EnvironmentMD: body.EnvironmentMD,
		Source: domain.SourceHuman, DueAt: dueAt, EstimateMinutes: estimate,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "create failed", err.Error())
		return
	}
	// Field values are written after the issue exists — they are keyed by issue id, so
	// there is nothing to attach them to before the insert. A failure here is reported
	// rather than swallowed, but the issue itself has already been filed and is not
	// rolled back: losing somebody's bug report over a custom field would be worse.
	if len(body.Fields) > 0 {
		if err := h.repo.SetIssueFieldValues(r.Context(), issue.ID, body.Fields); err != nil {
			h.log.Error("set field values on create", "issue", issue.Key, "err", err)
		}
	}
	writeJSON(w, http.StatusCreated, issue)
}

// bulkUpdateIssues applies a patch, a status transition, and/or a project move
// to a set of issues. Each issue is processed independently with the same
// permission checks and activity/event semantics as the single-issue
// endpoints; failures don't abort the batch and are reported per issue.
func (h *httpHandlers) bulkUpdateIssues(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	var body struct {
		IDs   []uuid.UUID `json:"ids"`
		Patch *struct {
			Priority    *domain.Priority `json:"priority"`
			Severity    *domain.Severity `json:"severity"`
			AssigneeID  *uuid.UUID       `json:"assignee_id"`
			Labels      *[]string        `json:"labels"`
			Components  *[]string        `json:"components"`
			MilestoneID *uuid.UUID       `json:"milestone_id"`
			ReleaseID   *uuid.UUID       `json:"release_id"`
		} `json:"patch"`
		Status           *domain.IssueStatus `json:"status"`
		TargetProjectKey *string             `json:"target_project_key"`
		Archived         *bool               `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if len(body.IDs) == 0 || len(body.IDs) > 100 {
		httpapi.WriteValidation(w, map[string]string{"ids": "between 1 and 100 issue ids"})
		return
	}
	if body.Patch == nil && body.Status == nil && body.TargetProjectKey == nil && body.Archived == nil {
		httpapi.WriteValidation(w, map[string]string{"patch": "nothing to apply"})
		return
	}
	var target string
	if body.TargetProjectKey != nil {
		target = strings.ToUpper(strings.TrimSpace(*body.TargetProjectKey))
		if _, err := h.repo.GetProjectByKey(r.Context(), target); err != nil {
			httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project: "+target)
			return
		}
	}

	actor, _ := uuid.Parse(p.UserID)
	type failure struct {
		Key   string `json:"key"`
		Error string `json:"error"`
	}
	updated, skipped := 0, 0
	failed := []failure{}
	fail := func(key, msg string) { failed = append(failed, failure{Key: key, Error: msg}) }

	for _, id := range body.IDs {
		// Tracks whether this issue was actually written to — asking for a status
		// it already has, or a move to the project it's already in, is a no-op and
		// must not be counted as an update.
		changed := false
		issue, err := h.repo.GetIssueByID(r.Context(), id)
		if err != nil {
			fail(id.String(), "not found")
			continue
		}
		if body.Patch != nil || body.Status != nil {
			perm := auth.PermIssueUpdate
			if body.Patch == nil {
				perm = auth.PermIssueTransition
			}
			if !h.canOnProject(r.Context(), p, issue.ProjectKey, perm) {
				fail(issue.Key, "forbidden")
				continue
			}
		}
		if body.Patch != nil {
			if _, err := h.issues.Update(r.Context(), issue.ID, actor, UpdateIssueInput{
				Priority: body.Patch.Priority, Severity: body.Patch.Severity,
				AssigneeID: body.Patch.AssigneeID, Labels: body.Patch.Labels,
				Components: body.Patch.Components, MilestoneID: body.Patch.MilestoneID,
				ReleaseID: body.Patch.ReleaseID,
			}); err != nil {
				fail(issue.Key, "update: "+err.Error())
				continue
			}
			changed = true
		}
		if body.Status != nil && issue.Status != *body.Status {
			if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueTransition) {
				fail(issue.Key, "forbidden (transition)")
				continue
			}
			if _, err := h.issues.Transition(r.Context(), issue.ID, issue.Status, *body.Status, actor); err != nil {
				fail(issue.Key, "transition: "+err.Error())
				continue
			}
			changed = true
		}
		if target != "" && target != issue.ProjectKey {
			if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) ||
				!h.canOnProject(r.Context(), p, target, auth.PermIssueCreate) {
				fail(issue.Key, "forbidden (move)")
				continue
			}
			if _, err := h.issues.Move(r.Context(), issue.ID, actor, target); err != nil {
				fail(issue.Key, "move: "+err.Error())
				continue
			}
			changed = true
		}
		if body.Archived != nil {
			if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
				fail(issue.Key, "forbidden (archive)")
				continue
			}
			if _, err := h.issues.SetArchived(r.Context(), issue.ID, actor, *body.Archived); err != nil {
				fail(issue.Key, "archive: "+err.Error())
				continue
			}
			changed = true
		}
		if changed {
			updated++
		} else {
			skipped++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated, "skipped": skipped, "failed": failed})
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
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
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
		Components      *[]string         `json:"components"`
		MilestoneID     *uuid.UUID        `json:"milestone_id"`
		ReleaseID       *uuid.UUID        `json:"release_id"`
		// Pointer-to-string so the three cases stay distinguishable: absent leaves the
		// due date alone, "" clears it, a timestamp sets it.
		DueAt *string `json:"due_at"`
		// Same three cases for the estimate: absent, "" clears, "2d" sets.
		Estimate *string `json:"estimate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.Title != nil && strings.TrimSpace(*body.Title) == "" {
		httpapi.WriteValidation(w, map[string]string{"title": "cannot be empty"})
		return
	}
	var dueAt *time.Time
	if body.DueAt != nil {
		parsed, clear, err := parseDueAt(*body.DueAt)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"due_at": err.Error()})
			return
		}
		if clear {
			// The store reads Go's zero time as "clear it" — see UpdateIssue.
			dueAt = &time.Time{}
		} else {
			dueAt = parsed
		}
	}
	var estimate *int
	if body.Estimate != nil {
		parsed, err := parseEstimate(*body.Estimate)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"estimate": err.Error()})
			return
		}
		if parsed == nil {
			// The store reads zero as "clear it" — see UpdateIssue.
			zero := 0
			parsed = &zero
		}
		estimate = parsed
	}
	actor, _ := uuid.Parse(p.UserID)
	updated, err := h.issues.Update(r.Context(), issue.ID, actor, UpdateIssueInput{
		Title: body.Title, DescriptionMD: body.DescriptionMD, Type: body.Type,
		Severity: body.Severity, Priority: body.Priority, AssigneeID: body.AssigneeID,
		VersionAffected: body.VersionAffected, VersionFixed: body.VersionFixed,
		ReproStepsMD: body.ReproStepsMD, ExpectedMD: body.ExpectedMD,
		ActualMD: body.ActualMD, EnvironmentMD: body.EnvironmentMD, Labels: body.Labels,
		Components: body.Components, MilestoneID: body.MilestoneID, ReleaseID: body.ReleaseID,
		DueAt: dueAt, EstimateMinutes: estimate,
	})
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// moveIssue re-homes an issue into another project. The issue's key changes (it's
// reallocated a number in the target project) and project-scoped associations are
// reconciled — see Store.MoveIssue. Returns the moved issue with its new key.
func (h *httpHandlers) moveIssue(w http.ResponseWriter, r *http.Request) {
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
		TargetProjectKey string `json:"target_project_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	target := strings.ToUpper(strings.TrimSpace(body.TargetProjectKey))
	if !projectKeyRe.MatchString(target) {
		httpapi.WriteValidation(w, map[string]string{"target_project_key": "required"})
		return
	}
	if target == issue.ProjectKey {
		httpapi.WriteProblem(w, http.StatusConflict, "already there", "issue is already in "+target)
		return
	}
	if !h.canOnProject(r.Context(), p, target, auth.PermIssueCreate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:create on "+target)
		return
	}
	if _, err := h.repo.GetProjectByKey(r.Context(), target); err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such project: "+target)
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	moved, err := h.issues.Move(r.Context(), issue.ID, actor, target)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "move failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, moved)
}

// archiveIssue / unarchiveIssue hide or restore an issue. Archived issues drop out of
// default lists and search but keep their status and stay reachable by key.
func (h *httpHandlers) archiveIssue(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, true)
}
func (h *httpHandlers) unarchiveIssue(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, false)
}

// snoozeIssue hides an issue until a date. Body: {"until": RFC3339, "note": "..."}.
func (h *httpHandlers) snoozeIssue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Until string `json:"until"`
		Note  string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	until, err := time.Parse(time.RFC3339, body.Until)
	if err != nil {
		httpapi.WriteValidation(w, map[string]string{"until": "expected an RFC 3339 timestamp"})
		return
	}
	// A snooze into the past is almost certainly a timezone mistake, and it would
	// silently do nothing — the waking job would clear it on the next run.
	if !until.After(time.Now()) {
		httpapi.WriteValidation(w, map[string]string{"until": "must be in the future"})
		return
	}
	h.setSnooze(w, r, &until, body.Note)
}

// wakeIssue clears a snooze early.
func (h *httpHandlers) wakeIssue(w http.ResponseWriter, r *http.Request) {
	h.setSnooze(w, r, nil, "")
}

func (h *httpHandlers) setSnooze(w http.ResponseWriter, r *http.Request, until *time.Time, note string) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	updated, err := h.issues.SetSnooze(r.Context(), issue.ID, actor, until, note)
	if err != nil {
		writeNotFoundOrError(w, err, "issue", "snooze failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// rankIssue positions a card within a board column, between the two cards the client
// saw either side of the drop. Body: {"after": "BUG-4", "before": "BUG-7"} — either may
// be empty for the ends of the column.
//
// The client names neighbours rather than sending a computed rank: the ordering scheme
// then lives in one place, and a stale client cannot write a rank that contradicts what
// is actually in the column.
func (h *httpHandlers) rankIssue(w http.ResponseWriter, r *http.Request) {
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
		After  string `json:"after"`  // the card above the drop
		Before string `json:"before"` // the card below the drop
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}

	prev, next, err := h.repo.NeighbourRanks(r.Context(), issue.ProjectKey, body.After, body.Before)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "rank failed", err.Error())
		return
	}
	rank := RankBetween(prev, next)
	if err := h.repo.SetIssueRank(r.Context(), issue.ID, rank); err != nil {
		writeNotFoundOrError(w, err, "issue", "rank failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": issue.Key, "rank": rank})
}

func (h *httpHandlers) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	updated, err := h.issues.SetArchived(r.Context(), issue.ID, actor, archived)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "archive failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *httpHandlers) deleteIssue(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermIssueDelete) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:delete")
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
	projectKey, number, ok := splitIssueKey(chi.URLParam(r, "issueKey"))
	if !ok {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad issue key", "")
		return
	}
	if !h.canOnProject(r.Context(), p, projectKey, auth.PermIssueTransition) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:transition")
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
	limit := int32(atoiDefault(r.URL.Query().Get("limit"), 100))
	offset := int32(atoiDefault(r.URL.Query().Get("offset"), 0))
	comments, total, err := h.repo.ListComments(r.Context(), issue.ID, limit, offset)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if comments == nil {
		comments = []domain.Comment{}
	}
	// `total` is the unpaged count so a long conversation pages instead of being
	// silently cut off at the page size.
	writeJSON(w, http.StatusOK, map[string]any{"items": comments, "total": total})
}

func (h *httpHandlers) addComment(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermCommentCreate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing comment:create")
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
	// `/spend 90m yesterday chasing the retry loop` logs time from the comment it was
	// written in. Applied after the comment lands, and never fatal: a rejected entry
	// must not swallow what somebody wrote, and the comment is still the record of it.
	for _, cmd := range ParseSpendCommands(body.BodyMD, time.Now()) {
		if _, err := h.repo.LogTime(r.Context(), TimeEntryInput{
			IssueID: issue.ID, UserID: author, Minutes: cmd.Minutes,
			SpentOn: cmd.SpentOn, Note: cmd.Note,
		}); err != nil {
			h.log.Warn("spend command failed", "issue", issue.Key, "err", err)
		}
	}
	writeJSON(w, http.StatusCreated, c)
}

// ── watchers ──

func (h *httpHandlers) listWatchers(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	items, err := h.repo.ListWatchers(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.User{}
	}
	watching := false
	for _, u := range items {
		if u.ID.String() == p.UserID {
			watching = true
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "watching": watching})
}

func (h *httpHandlers) setWatchState(w http.ResponseWriter, r *http.Request, watching bool) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	uid, err := uuid.Parse(p.UserID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad principal", "")
		return
	}
	if err := h.repo.SetWatcher(r.Context(), issue.ID, uid, watching); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "watch update failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) watchIssue(w http.ResponseWriter, r *http.Request) {
	h.setWatchState(w, r, true)
}
func (h *httpHandlers) unwatchIssue(w http.ResponseWriter, r *http.Request) {
	h.setWatchState(w, r, false)
}

// ── issue relations ──

var validRelationKinds = map[string]bool{
	"blocks": true, "blocked_by": true, "duplicates": true, "relates": true, "caused_by": true,
}

func (h *httpHandlers) listRelations(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	items, err := h.repo.ListRelations(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.IssueRelation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// listReferences returns the issues that mention this one in their prose.
func (h *httpHandlers) listReferences(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	items, err := h.repo.ListReferencedBy(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.IssueReference{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *httpHandlers) addRelation(w http.ResponseWriter, r *http.Request) {
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
		Kind     string `json:"kind"`
		IssueKey string `json:"issue_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if !validRelationKinds[body.Kind] {
		httpapi.WriteValidation(w, map[string]string{"kind": "must be blocks, blocked_by, duplicates, relates, or caused_by"})
		return
	}
	targetKey, targetNumber, ok := splitIssueKey(strings.ToUpper(strings.TrimSpace(body.IssueKey)))
	if !ok {
		httpapi.WriteValidation(w, map[string]string{"issue_key": "expected e.g. BUG-421"})
		return
	}
	target, err := h.issues.Get(r.Context(), targetKey, targetNumber)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such issue: "+body.IssueKey)
		return
	}
	if target.ID == issue.ID {
		httpapi.WriteProblem(w, http.StatusConflict, "invalid relation", "an issue cannot relate to itself")
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	rel, err := h.repo.CreateRelation(r.Context(), issue.ID, target.ID, body.Kind, actor)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusConflict, "link failed",
			"this relation may already exist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rel)
}

func (h *httpHandlers) deleteRelation(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad relation id", "")
		return
	}
	fromKey, toKey, err := h.repo.GetRelationProjectKeys(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such relation")
		return
	}
	if !h.canOnProject(r.Context(), p, fromKey, auth.PermIssueUpdate) &&
		!h.canOnProject(r.Context(), p, toKey, auth.PermIssueUpdate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing issue:update")
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	ok, err := h.repo.DeleteRelation(r.Context(), id, actor)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "unlink failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such relation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateComment lets the author revise their own comment (stamps edited_at).
func (h *httpHandlers) updateComment(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad comment id", "")
		return
	}
	c, err := h.repo.GetComment(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such comment")
		return
	}
	if c.Author == nil || c.Author.ID.String() != p.UserID {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "only the author can edit a comment")
		return
	}
	var body struct {
		BodyMD string `json:"body_md"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.BodyMD) == "" {
		httpapi.WriteValidation(w, map[string]string{"body_md": "required"})
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	updated, err := h.repo.UpdateComment(r.Context(), id, actor, body.BodyMD)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "update failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// deleteComment soft-deletes; allowed for the author or project managers.
func (h *httpHandlers) deleteComment(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad comment id", "")
		return
	}
	c, err := h.repo.GetComment(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such comment")
		return
	}
	isAuthor := c.Author != nil && c.Author.ID.String() == p.UserID
	if !isAuthor && !h.canOnProject(r.Context(), p, c.ProjectKey, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "not the author and missing project:manage")
		return
	}
	actor, _ := uuid.Parse(p.UserID)
	ok, err := h.repo.SoftDeleteComment(r.Context(), id, actor)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !ok {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such comment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpHandlers) activity(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	limit := int32(atoiDefault(r.URL.Query().Get("limit"), 100))
	offset := int32(atoiDefault(r.URL.Query().Get("offset"), 0))
	acts, total, err := h.issues.Activity(r.Context(), issue.ID, limit, offset)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "activity failed", err.Error())
		return
	}
	if acts == nil {
		acts = []domain.Activity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": acts, "total": total})
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

// ── attachments (bytes on local disk, metadata in Postgres) ──

func (h *httpHandlers) listAttachments(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	items, err := h.repo.ListAttachmentsForIssue(r.Context(), issue.ID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Attachment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *httpHandlers) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	// Attaching evidence is part of collaborating on an issue — same bar as commenting.
	if !h.canOnProject(r.Context(), p, issue.ProjectKey, auth.PermCommentCreate) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing comment:create")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload)
	if err := r.ParseMultipartForm(4 << 20); err != nil { // 4MB in memory, rest to temp files
		httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "upload too large",
			fmt.Sprintf("multipart parse failed (limit %d MB): %v", h.maxUpload>>20, err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpapi.WriteValidation(w, map[string]string{"file": "multipart field 'file' required"})
		return
	}
	defer file.Close() //nolint:errcheck

	// Keep only the base name; never trust client paths.
	filename := filepath.Base(strings.TrimSpace(header.Filename))
	if filename == "" || filename == "." || filename == "/" {
		filename = "attachment"
	}

	// Object key is server-generated; the extension is kept only as a hint.
	objectKey := uuid.NewString() + strings.ToLower(filepath.Ext(filename))
	if err := os.MkdirAll(h.attachDir, 0o755); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "storage unavailable", err.Error())
		return
	}
	dst, err := os.Create(filepath.Join(h.attachDir, objectKey))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "storage unavailable", err.Error())
		return
	}
	defer dst.Close() //nolint:errcheck

	hasher := sha256.New()
	size, err := io.Copy(dst, io.TeeReader(file, hasher))
	if err != nil {
		_ = os.Remove(filepath.Join(h.attachDir, objectKey))
		httpapi.WriteProblem(w, http.StatusInternalServerError, "write failed", err.Error())
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	uploader, _ := uuid.Parse(p.UserID)
	att, err := h.repo.CreateAttachment(r.Context(), CreateAttachmentInput{
		IssueID: issue.ID, UploaderID: uploader, Filename: filename, ContentType: contentType,
		SizeBytes: size, ObjectKey: objectKey, Checksum: hex.EncodeToString(hasher.Sum(nil)),
	})
	if err != nil {
		_ = os.Remove(filepath.Join(h.attachDir, objectKey))
		httpapi.WriteProblem(w, http.StatusInternalServerError, "save failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, att)
}

func (h *httpHandlers) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad attachment id", "")
		return
	}
	att, err := h.repo.GetAttachment(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such attachment")
		return
	}
	f, err := os.Open(filepath.Join(h.attachDir, filepath.Base(att.ObjectKey)))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "attachment bytes missing from storage")
		return
	}
	defer f.Close() //nolint:errcheck

	// Content-Length must describe the bytes actually on disk, not what the metadata
	// row claims: a short file would otherwise leave the client waiting on a read
	// that never completes.
	size := att.SizeBytes
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}

	// Serve user content defensively: images/PDF render inline, everything else
	// downloads; HTML-ish types are neutralized to text/plain.
	ct := att.ContentType
	disposition := "attachment"
	switch {
	case strings.HasPrefix(ct, "image/"), ct == "application/pdf":
		disposition = "inline"
	case strings.Contains(ct, "html"), strings.Contains(ct, "xml"), strings.Contains(ct, "svg"):
		ct = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, att.Filename))
	_, _ = io.Copy(w, f)
}

func (h *httpHandlers) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad attachment id", "")
		return
	}
	att, err := h.repo.GetAttachment(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such attachment")
		return
	}
	// The uploader may remove their own file; otherwise project management rights.
	isUploader := att.Uploader != nil && att.Uploader.ID.String() == p.UserID
	if !isUploader && !h.canOnProject(r.Context(), p, att.ProjectKey, auth.PermProjectManage) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "not the uploader and missing project:manage")
		return
	}
	objectKey, found, err := h.repo.DeleteAttachment(r.Context(), id)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}
	if !found {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such attachment")
		return
	}
	_ = os.Remove(filepath.Join(h.attachDir, filepath.Base(objectKey))) // best effort
	w.WriteHeader(http.StatusNoContent)
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

// writeNotFoundOrError distinguishes "the row isn't there" from "the write failed".
// Collapsing both into a 404 (or a 409) makes a real database fault indistinguishable
// from a missing id, which is the difference between a client bug and an outage.
func writeNotFoundOrError(w http.ResponseWriter, err error, entity, title string) {
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.WriteProblem(w, http.StatusNotFound, "not found", "no such "+entity)
		return
	}
	httpapi.WriteProblem(w, http.StatusInternalServerError, title, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
