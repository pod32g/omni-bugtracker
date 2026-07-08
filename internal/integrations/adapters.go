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
	return &notifyClient{jsonClient: newJSONClient(cfg.BaseURL, cfg.Timeout, "omni-notify"), logger: logger}
}

func (c *notifyClient) Notify(ctx context.Context, ev NotifyEvent) error {
	return c.postJSON(ctx, "/api/v1/notifications", ev, nil)
}

// ── Omni-Search ──

type searchClient struct {
	jsonClient
	logger *slog.Logger
}

func newSearchClient(cfg config.ServiceAdapter, logger *slog.Logger) *searchClient {
	return &searchClient{jsonClient: newJSONClient(cfg.BaseURL, cfg.Timeout, "omni-search"), logger: logger}
}

func (c *searchClient) Index(ctx context.Context, doc SearchDoc) error {
	return c.postJSON(ctx, "/api/v1/index", doc, nil)
}

// ── Omni-Upload ──

type uploadClient struct {
	jsonClient
	logger *slog.Logger
}

func newUploadClient(cfg config.ServiceAdapter, logger *slog.Logger) *uploadClient {
	return &uploadClient{jsonClient: newJSONClient(cfg.BaseURL, cfg.Timeout, "omni-upload"), logger: logger}
}

func (c *uploadClient) Presign(ctx context.Context, req UploadRequest) (UploadTarget, error) {
	var target UploadTarget
	err := c.postJSON(ctx, "/api/v1/presign", req, &target)
	return target, err
}
