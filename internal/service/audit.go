package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// Audit actions. Named as verbs on a target so the log reads as a sentence and so a
// filter by action is a stable string rather than free text.
const (
	AuditUserRoleChanged   = "user.role_changed"
	AuditTokenCreated      = "token.created"
	AuditTokenRevoked      = "token.revoked"
	AuditProjectCreated    = "project.created"
	AuditProjectUpdated    = "project.updated"
	AuditProjectArchived   = "project.archived"
	AuditProjectKeyRenamed = "project.key_renamed"
	AuditMemberSet         = "project.member_set"
	AuditMemberRemoved     = "project.member_removed"
	AuditWebhookCreated    = "webhook.created"
	AuditWebhookUpdated    = "webhook.updated"
	AuditWebhookDeleted    = "webhook.deleted"
	AuditRuleCreated       = "automation.rule_created"
	AuditRuleUpdated       = "automation.rule_updated"
	AuditRuleDeleted       = "automation.rule_deleted"
	AuditSettingsUpdated   = "settings.updated"
)

// AuditEntry is one append-only record.
type AuditEntry struct {
	ActorID     *uuid.UUID
	ActorEmail  string
	Action      string
	TargetType  string
	TargetID    string
	TargetLabel string
	Details     json.RawMessage
	IP          string
	UserAgent   string
	ViaToken    bool
	TokenID     *uuid.UUID
}

// AuditFilter narrows the log. Zero values mean "no constraint".
type AuditFilter struct {
	Action     string
	TargetType string
	ActorID    *uuid.UUID
	Since      *time.Time
	Until      *time.Time
	Limit      int32
	Offset     int32
}

// audit records a privileged action, taking the actor and request metadata from the
// request itself so no call site has to remember to pass them.
//
// Failures are logged and swallowed. An audit insert that fails should not fail the
// role change it was describing — losing one log line is bad, refusing the operation
// because we could not describe it is worse, and the alternative (a transaction
// spanning both) would mean threading a tx through every privileged handler.
//
// Secrets never reach here: callers pass ids and names. Token plaintext and webhook
// secrets are not in the details of any call site, by construction.
func (h *httpHandlers) audit(r *http.Request, action, targetType, targetID, label string, details map[string]any) {
	p := auth.FromContext(r.Context())
	if p == nil {
		return
	}
	entry := AuditEntry{
		ActorEmail: p.Email, Action: action, TargetType: targetType,
		TargetID: targetID, TargetLabel: label,
		IP: clientIP(r), UserAgent: r.UserAgent(), ViaToken: p.ViaToken,
		Details: json.RawMessage("{}"),
	}
	if id, err := uuid.Parse(p.UserID); err == nil {
		entry.ActorID = &id
	}
	if id, err := uuid.Parse(p.TokenID); err == nil {
		entry.TokenID = &id
	}
	if len(details) > 0 {
		if raw, err := json.Marshal(details); err == nil {
			entry.Details = raw
		}
	}
	// WithoutCancel: the client disconnecting after a successful write must not lose
	// the record of it.
	if err := h.repo.RecordAudit(context.WithoutCancel(r.Context()), entry); err != nil && h.log != nil {
		h.log.Error("audit write failed", "err", err, "action", action, "target", targetID)
	}
}

// clientIP strips the port so one client is one address.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > strings.LastIndex(addr, "]") {
		addr = addr[:i]
	}
	return strings.Trim(addr, "[]")
}

// listAudit serves the log to admins only. Reading who changed what is itself sensitive:
// it reveals the shape of the team and when people are active.
func (h *httpHandlers) listAudit(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if !p.Can(auth.PermAdmin) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing admin:all")
		return
	}
	f := AuditFilter{
		Action:     r.URL.Query().Get("action"),
		TargetType: r.URL.Query().Get("target_type"),
		Limit:      int32(atoiDefault(r.URL.Query().Get("limit"), 50)),
		Offset:     int32(atoiDefault(r.URL.Query().Get("offset"), 0)),
	}
	if raw := r.URL.Query().Get("actor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{"actor": "expected a user uuid"})
			return
		}
		f.ActorID = &id
	}
	for _, spec := range []struct {
		param string
		dest  **time.Time
	}{{"since", &f.Since}, {"until", &f.Until}} {
		raw := r.URL.Query().Get(spec.param)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			httpapi.WriteValidation(w, map[string]string{spec.param: "expected an RFC 3339 timestamp"})
			return
		}
		*spec.dest = &t
	}

	items, total, err := h.repo.ListAudit(r.Context(), f)
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "list failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "actions": auditActions()})
}

func auditActions() []string {
	return []string{
		AuditUserRoleChanged, AuditTokenCreated, AuditTokenRevoked,
		AuditProjectCreated, AuditProjectUpdated, AuditProjectArchived, AuditProjectKeyRenamed,
		AuditMemberSet, AuditMemberRemoved,
		AuditWebhookCreated, AuditWebhookUpdated, AuditWebhookDeleted,
		AuditRuleCreated, AuditRuleUpdated, AuditRuleDeleted, AuditSettingsUpdated,
	}
}
