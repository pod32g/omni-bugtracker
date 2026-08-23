package service

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// KnownEmoji is the closed reaction set, mirroring the CHECK constraint on the table.
// Closed on purpose: the value is rendered into the page, and an open text column any
// commenter can write to is not worth the expressiveness.
var KnownEmoji = []string{"+1", "-1", "tada", "confused", "heart", "rocket", "eyes"}

func knownEmoji(e string) bool {
	for _, k := range KnownEmoji {
		if k == e {
			return true
		}
	}
	return false
}

// listReactions returns every reaction on an issue and its comments.
func (h *httpHandlers) listReactions(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	viewer, _ := uuid.Parse(auth.FromContext(r.Context()).UserID)
	items, err := h.repo.ListReactions(r.Context(), issue.ID, viewer)
	if err != nil {
		h.serverError(w, r, "list failed", err)
		return
	}
	if items == nil {
		items = []domain.Reaction{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "emoji": KnownEmoji})
}

// toggleReaction adds or removes the caller's reaction. Body: {"emoji": "+1",
// "comment_id": "…"} — comment_id omitted means the issue body.
//
// Gated on comment:create rather than issue:update: reacting is participating in the
// conversation, and anyone who may comment may certainly agree with one. Reactions
// deliberately emit no event and write no activity — the entire point is that
// acknowledgement should be free, and a reaction that notifies is just a worse comment.
func (h *httpHandlers) toggleReaction(w http.ResponseWriter, r *http.Request) {
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
		Emoji     string `json:"emoji"`
		CommentID string `json:"comment_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if !knownEmoji(body.Emoji) {
		httpapi.WriteValidation(w, map[string]string{"emoji": "unknown reaction"})
		return
	}

	var commentID *uuid.UUID
	if body.CommentID != "" {
		id, err := uuid.Parse(body.CommentID)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"comment_id": "expected a uuid"})
			return
		}
		commentID = &id
	}

	userID, _ := uuid.Parse(p.UserID)
	added, err := h.repo.ToggleReaction(r.Context(), issue.ID, commentID, userID, body.Emoji)
	if err != nil {
		h.serverError(w, r, "reaction failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emoji": body.Emoji, "reacted": added})
}
