package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/httpapi"
)

const maxWebhookBody = 5 << 20 // 5 MiB

// IntegrationHandlers serves inbound webhooks from other systems (git providers, and
// later Omni-Logging / Omni-Metrics). They authenticate via HMAC, not bearer tokens.
type IntegrationHandlers struct {
	pub    *events.Publisher
	cfg    config.Git
	logger *slog.Logger
}

func NewIntegrationHandlers(pub *events.Publisher, cfg config.Git, logger *slog.Logger) *IntegrationHandlers {
	return &IntegrationHandlers{pub: pub, cfg: cfg, logger: logger}
}

// GitEvents receives commit/PR webhooks (GitHub-compatible), verifies the HMAC signature,
// and enqueues a git-ingest job. Parsing/linking happens asynchronously in the worker.
func (h *IntegrationHandlers) GitEvents(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled {
		httpapi.WriteProblem(w, http.StatusNotImplemented, "git integration disabled", "")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "read body", err.Error())
		return
	}
	if !h.validSignature(r, body) {
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

// validSignature verifies the GitHub-style HMAC-SHA256 signature. If no secret is
// configured, verification is skipped (dev/trusted-network mode).
func (h *IntegrationHandlers) validSignature(r *http.Request, body []byte) bool {
	if h.cfg.WebhookSecret == "" {
		return true
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		sig = r.Header.Get("X-Signature-256")
	}
	sig = strings.TrimPrefix(sig, "sha256=")
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.WebhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
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
