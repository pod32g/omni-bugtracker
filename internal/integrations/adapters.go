package integrations

import (
	"context"
	"log/slog"

	"github.com/omni/bugtracker/internal/config"
)

// ── Omni-Notify ──

type notifyClient struct {
	jsonClient
	logger *slog.Logger
}

func newNotifyClient(cfg config.ServiceAdapter, logger *slog.Logger) *notifyClient {
	return &notifyClient{
		jsonClient: newJSONClient(cfg.BaseURL, cfg.Timeout, "omni-notify").withToken(cfg.APIToken),
		logger:     logger,
	}
}

// Notify posts an event to Omni-Notify's ingest endpoint. Routing to concrete
// providers (Discord/Telegram/email/…) is configured inside Omni-Notify.
func (c *notifyClient) Notify(ctx context.Context, ev NotifyEvent) error {
	return c.postJSON(ctx, "/api/v1/events", ev, nil)
}
