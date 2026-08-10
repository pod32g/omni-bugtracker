package service

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// listNotifications returns the caller's inbox page and their unread total.
func (h *httpHandlers) listNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerID(w, r)
	if !ok {
		return
	}
	items, unread, err := h.repo.ListNotifications(r.Context(), userID,
		r.URL.Query().Get("unread") == "true",
		int32(atoiDefault(r.URL.Query().Get("limit"), 30)),
		int32(atoiDefault(r.URL.Query().Get("offset"), 0)))
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if items == nil {
		items = []domain.Notification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

// markNotificationsRead marks the given ids read, or the whole inbox when none are given.
func (h *httpHandlers) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerID(w, r)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	// An empty body means "all", so a decode failure on no content is not an error.
	_ = json.NewDecoder(r.Body).Decode(&body)

	ids := make([]uuid.UUID, 0, len(body.IDs))
	for _, raw := range body.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"ids": "expected notification uuids"})
			return
		}
		ids = append(ids, id)
	}
	n, err := h.repo.MarkNotificationsRead(r.Context(), userID, ids)
	if err != nil {
		h.serverError(w, r, "update failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked": n})
}

// callerID resolves the authenticated principal to a user id.
func (h *httpHandlers) callerID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	p := auth.FromContext(r.Context())
	id, err := uuid.Parse(p.UserID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusUnauthorized, "unauthorized", "no user in context")
		return uuid.Nil, false
	}
	return id, true
}

// markIssueRead clears the caller's unread notifications for one issue. Called when the
// issue is opened: reading the thing the badge pointed at is what "read" means, and a
// count that survives it is a count people learn to ignore.
func (h *httpHandlers) markIssueRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerID(w, r)
	if !ok {
		return
	}
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if err := h.repo.MarkIssueNotificationsRead(r.Context(), userID, issue.ID); err != nil {
		h.serverError(w, r, "update failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
