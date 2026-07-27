package integrations

import (
	"context"
	"errors"
	"log/slog"

	"github.com/omni/bugtracker/internal/config"
)

// ErrDisabled is returned by no-op adapters when an integration is turned off.
var ErrDisabled = errors.New("integration disabled")

// NotifyEvent is the payload sent to Omni-Notify's POST /api/v1/events —
// its native event schema (event_id/type/source/title/timestamp required).
type NotifyEvent struct {
	EventID   string            `json:"event_id"`
	Type      string            `json:"type"`
	Source    string            `json:"source"`
	Status    string            `json:"status,omitempty"`   // firing | resolved
	Severity  string            `json:"severity,omitempty"` // critical|error|warning|info|debug
	Title     string            `json:"title"`
	Summary   string            `json:"summary,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp string            `json:"timestamp"` // RFC3339
}

// Ports.
type Notifier interface {
	Notify(ctx context.Context, ev NotifyEvent) error
}

// Registry is the set of external adapters, chosen by config (real vs no-op).
type Registry struct {
	Notify Notifier
}

// NewRegistry builds adapters honoring the enabled flags. Disabled services get no-ops.
func NewRegistry(cfg config.Integrations, logger *slog.Logger) *Registry {
	reg := &Registry{
		Notify: noopNotifier{},
	}
	if cfg.Notify.Enabled {
		reg.Notify = newNotifyClient(cfg.Notify, logger)
	}
	return reg
}

// ── no-op fallbacks ──

type noopNotifier struct{}

func (noopNotifier) Notify(context.Context, NotifyEvent) error { return ErrDisabled }
