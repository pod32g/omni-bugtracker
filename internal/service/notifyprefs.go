package service

import (
	"encoding/json"
	"net/http"

	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/httpapi"
)

// Notification channels.
const (
	ChannelOff   = "off"
	ChannelInbox = "inbox"
	ChannelPush  = "push"
	ChannelBoth  = "both"
)

// DefaultChannels is what an event does for somebody who has never opened settings.
//
// The shape of it is the argument: being named or handed an issue always reaches you,
// lifecycle changes reach the people following it, and the bookkeeping — labels,
// fields, archival — does not interrupt anyone. Notifications become worthless the
// moment they are noisy, so the defaults start quiet and let people opt into more.
var DefaultChannels = map[string]string{
	events.UserMentioned:      ChannelBoth,
	events.IssueAssigned:      ChannelBoth,
	events.IssueCommented:     ChannelBoth,
	events.IssueStatusChanged: ChannelInbox,
	events.IssueResolved:      ChannelInbox,
	events.IssueClosed:        ChannelInbox,
	events.IssueReopened:      ChannelInbox,
	events.IssueCreated:       ChannelInbox,
	events.IssueWoke:          ChannelInbox,
	events.IssueUpdated:       ChannelOff,
	events.IssueArchived:      ChannelOff,
	events.IssueUnarchived:    ChannelOff,
	events.IssueLinked:        ChannelOff,
}

// ChannelFor resolves an event to a channel, falling back to the default and then to
// inbox-only for anything not listed — a new event type should show up somewhere rather
// than vanish, but it should not page anybody either.
func ChannelFor(prefs map[string]string, eventType string) string {
	if c, ok := prefs[eventType]; ok {
		return c
	}
	if c, ok := DefaultChannels[eventType]; ok {
		return c
	}
	return ChannelInbox
}

// WantsInbox / WantsPush read a channel.
func WantsInbox(c string) bool { return c == ChannelInbox || c == ChannelBoth }
func WantsPush(c string) bool  { return c == ChannelPush || c == ChannelBoth }

// getNotificationPrefs returns the caller's explicit choices plus the defaults, so the
// settings page can render the effective value without duplicating the table above.
func (h *httpHandlers) getNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerID(w, r)
	if !ok {
		return
	}
	prefs, err := h.repo.GetNotificationPrefs(r.Context(), userID)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "load failed", err.Error())
		return
	}
	effective := make(map[string]string, len(DefaultChannels))
	for event := range DefaultChannels {
		effective[event] = ChannelFor(prefs, event)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channels": effective, "defaults": DefaultChannels, "explicit": prefs,
	})
}

// putNotificationPrefs replaces the caller's explicit choices. Setting an event back to
// its default deletes the row rather than storing a copy of it, so a later change to the
// defaults reaches everyone who never disagreed with them.
func (h *httpHandlers) putNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.callerID(w, r)
	if !ok {
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	fields := map[string]string{}
	for event, channel := range body {
		if _, known := DefaultChannels[event]; !known {
			fields[event] = "unknown event type"
			continue
		}
		switch channel {
		case ChannelOff, ChannelInbox, ChannelPush, ChannelBoth:
		default:
			fields[event] = "expected off, inbox, push or both"
		}
	}
	if len(fields) > 0 {
		httpapi.WriteValidation(w, fields)
		return
	}
	if err := h.repo.SetNotificationPrefs(r.Context(), userID, body, DefaultChannels); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "save failed", err.Error())
		return
	}
	h.getNotificationPrefs(w, r)
}

// muteIssue silences one issue without unwatching it, and DELETE undoes that.
func (h *httpHandlers) setIssueMute(w http.ResponseWriter, r *http.Request, muted bool) {
	userID, ok := h.callerID(w, r)
	if !ok {
		return
	}
	issue, ok := h.resolveIssue(w, r)
	if !ok {
		return
	}
	if err := h.repo.SetIssueMute(r.Context(), issue.ID, userID, muted); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "mute failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"muted": muted})
}

func (h *httpHandlers) muteIssue(w http.ResponseWriter, r *http.Request) { h.setIssueMute(w, r, true) }
func (h *httpHandlers) unmuteIssue(w http.ResponseWriter, r *http.Request) {
	h.setIssueMute(w, r, false)
}
