package integrations

import (
	"context"
	"errors"
	"log/slog"

	"github.com/omni/bugtracker/internal/config"
)

// ErrDisabled is returned by no-op adapters when an integration is turned off.
var ErrDisabled = errors.New("integration disabled")

// NotifyEvent is the payload sent to Omni-Notify.
type NotifyEvent struct {
	EventType  string   `json:"event_type"`
	IssueKey   string   `json:"issue_key,omitempty"`
	IssueID    string   `json:"issue_id,omitempty"`
	ActorID    string   `json:"actor_id,omitempty"`
	Title      string   `json:"title,omitempty"`
	Recipients []string `json:"recipients,omitempty"`
}

// SearchDoc is a document projected into Omni-Search.
type SearchDoc struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"` // issue|comment|commit|release
	Title  string            `json:"title"`
	Body   string            `json:"body"`
	Fields map[string]string `json:"fields,omitempty"`
}

// UploadRequest asks Omni-Upload for a presigned direct-upload target.
type UploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type UploadTarget struct {
	UploadURL string            `json:"upload_url"`
	ObjectKey string            `json:"object_key"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// Ports.
type Notifier interface {
	Notify(ctx context.Context, ev NotifyEvent) error
}
type Indexer interface {
	Index(ctx context.Context, doc SearchDoc) error
}
type Uploader interface {
	Presign(ctx context.Context, req UploadRequest) (UploadTarget, error)
}

// Registry is the set of external adapters, chosen by config (real vs no-op).
type Registry struct {
	Notify Notifier
	Search Indexer
	Upload Uploader
}

// NewRegistry builds adapters honoring the enabled flags. Disabled services get no-ops.
func NewRegistry(cfg config.Integrations, logger *slog.Logger) *Registry {
	reg := &Registry{
		Notify: noopNotifier{},
		Search: noopIndexer{},
		Upload: noopUploader{},
	}
	if cfg.Notify.Enabled {
		reg.Notify = newNotifyClient(cfg.Notify, logger)
	}
	if cfg.Search.Enabled {
		reg.Search = newSearchClient(cfg.Search, logger)
	}
	if cfg.Upload.Enabled {
		reg.Upload = newUploadClient(cfg.Upload, logger)
	}
	return reg
}

// ── no-op fallbacks ──

type noopNotifier struct{}

func (noopNotifier) Notify(context.Context, NotifyEvent) error { return ErrDisabled }

type noopIndexer struct{}

func (noopIndexer) Index(context.Context, SearchDoc) error { return ErrDisabled }

type noopUploader struct{}

func (noopUploader) Presign(context.Context, UploadRequest) (UploadTarget, error) {
	return UploadTarget{}, ErrDisabled
}
