package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/httpapi"
)

const maxWebhookBody = 5 << 20 // 5 MiB

// IntegrationHandlers serves inbound webhooks from other systems (git providers
// and observability alert sources). They authenticate via HMAC, not bearer tokens.
type IntegrationHandlers struct {
	pub    *events.Publisher
	cfg    config.Integrations
	logger *slog.Logger
}

func NewIntegrationHandlers(pub *events.Publisher, cfg config.Integrations, logger *slog.Logger) *IntegrationHandlers {
	return &IntegrationHandlers{pub: pub, cfg: cfg, logger: logger}
}

// GitEvents receives commit/PR webhooks (GitHub-compatible), verifies the HMAC signature,
// and enqueues a git-ingest job. Parsing/linking happens asynchronously in the worker.
func (h *IntegrationHandlers) GitEvents(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Git.Enabled {
		httpapi.WriteProblem(w, http.StatusNotImplemented, "git integration disabled", "")
		return
	}
	if !requireSecret(w, "git", h.cfg.Git.WebhookSecret) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "read body", err.Error())
		return
	}
	if !validSignature(r, body, h.cfg.Git.WebhookSecret) {
		httpapi.WriteProblem(w, http.StatusUnauthorized, "invalid signature", "")
		return
	}

	event := gitEventKind(r)
	if event == "ping" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if event != "push" && event != "pull_request" {
		w.WriteHeader(http.StatusAccepted) // ignored event kind
		return
	}

	if err := h.pub.Enqueue(r.Context(), events.GitIngestArgs{
		Provider: "github", Event: event, Payload: body,
	}); err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "enqueue failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ObsAlertsHandler returns the inbound handler for one alert source
// ("logging" or "metrics"). Alerts are validated, fingerprinted, and handed to
// the obs-ingest worker which creates or bumps the matching issue.
func (h *IntegrationHandlers) ObsAlertsHandler(source string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var inbound config.Inbound
		switch source {
		case "logging":
			inbound = h.cfg.Logging
		default:
			inbound = h.cfg.Metrics
		}
		if !inbound.Enabled {
			httpapi.WriteProblem(w, http.StatusNotImplemented, source+" ingestion disabled", "")
			return
		}
		if !requireSecret(w, source, inbound.WebhookSecret) {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			httpapi.WriteProblem(w, http.StatusBadRequest, "read body", err.Error())
			return
		}
		if !validSignature(r, body, inbound.WebhookSecret) {
			httpapi.WriteProblem(w, http.StatusUnauthorized, "invalid signature", "")
			return
		}
		var alert struct {
			Rule        string `json:"rule"`
			ProjectKey  string `json:"project_key"`
			Title       string `json:"title"`
			Fingerprint string `json:"fingerprint"`
		}
		if err := json.Unmarshal(body, &alert); err != nil {
			httpapi.WriteProblem(w, http.StatusBadRequest, "bad alert payload", err.Error())
			return
		}
		fields := map[string]string{}
		if strings.TrimSpace(alert.Title) == "" && strings.TrimSpace(alert.Rule) == "" {
			fields["title"] = "title or rule required"
		}
		if strings.TrimSpace(alert.ProjectKey) == "" {
			fields["project_key"] = "required"
		}
		if len(fields) > 0 {
			httpapi.WriteValidation(w, fields)
			return
		}
		// Fingerprint defaults to source+project+rule so repeated firings of the
		// same rule collapse into one issue.
		fingerprint := strings.TrimSpace(alert.Fingerprint)
		if fingerprint == "" {
			sum := sha256.Sum256([]byte(source + "|" + alert.ProjectKey + "|" + alert.Rule))
			fingerprint = hex.EncodeToString(sum[:16])
		}
		if err := h.pub.Enqueue(r.Context(), events.ObsIngestArgs{
			Source: source, ProjectKey: strings.ToUpper(alert.ProjectKey),
			Fingerprint: fingerprint, Payload: body,
		}); err != nil {
			httpapi.WriteProblem(w, http.StatusInternalServerError, "enqueue failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// validSignature verifies the GitHub-style HMAC-SHA256 signature.
//
// An unset secret fails closed. These endpoints sit outside bearer auth and can
// create issues and drive status transitions, so "no secret configured" has to
// mean "refuse everything", not "accept anything" — otherwise shipping the default
// config leaves them open to anonymous writes. Operators who genuinely want an
// unauthenticated endpoint on a trusted network disable the integration instead.
func validSignature(r *http.Request, body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		sig = r.Header.Get("X-Signature-256")
	}
	if sig == "" {
		sig = r.Header.Get("X-OBT-Signature")
	}
	sig = strings.TrimPrefix(strings.TrimSpace(sig), "sha256=")
	if sig == "" {
		return false
	}
	// Compare decoded bytes so hex casing doesn't matter and the comparison stays
	// constant-time over a fixed length.
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// requireSecret reports whether an inbound integration is usable, writing a clear
// operator-facing error when it is enabled but has no signing secret. Without this
// the misconfiguration surfaces as a bare 401 on every delivery.
func requireSecret(w http.ResponseWriter, kind, secret string) bool {
	if secret != "" {
		return true
	}
	httpapi.WriteProblem(w, http.StatusServiceUnavailable, "webhook secret not configured",
		"set integrations."+kind+".webhook_secret (OMNI_BT_INTEGRATIONS__"+
			strings.ToUpper(kind)+"__WEBHOOK_SECRET) before sending deliveries, or disable the integration")
	return false
}

func gitEventKind(r *http.Request) string {
	if e := r.Header.Get("X-GitHub-Event"); e != "" {
		return e
	}
	if e := r.Header.Get("X-Gitlab-Event"); e != "" {
		// GitLab: "Push Hook" / "Merge Request Hook"
		switch {
		case strings.Contains(e, "Push"):
			return "push"
		case strings.Contains(e, "Merge"):
			return "pull_request"
		}
	}
	return ""
}
