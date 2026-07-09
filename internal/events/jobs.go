package events

import (
	"encoding/json"

	"github.com/riverqueue/river"
)

// Downstream job contracts. The dispatcher turns a DomainEventArgs into these; the worker
// package registers a processor per Kind. Keeping the args here gives producer and consumer
// a single shared definition.

// NotifyJobArgs → Omni-Notify.
type NotifyJobArgs struct {
	EventType  string   `json:"event_type"`
	IssueID    string   `json:"issue_id"`
	ActorID    string   `json:"actor_id,omitempty"`
	Recipients []string `json:"recipients,omitempty"`
}

func (NotifyJobArgs) Kind() string                 { return "notify" }
func (NotifyJobArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: "integrations"} }

// WebhookJobArgs → one outbound HTTP delivery (retried by River).
type WebhookJobArgs struct {
	WebhookID string          `json:"webhook_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

func (WebhookJobArgs) Kind() string { return "webhook_delivery" }
func (WebhookJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "webhooks", MaxAttempts: 8}
}

// IndexJobArgs → project a document into Omni-Search.
type IndexJobArgs struct {
	DocType string `json:"doc_type"`
	DocID   string `json:"doc_id"`
}

func (IndexJobArgs) Kind() string                 { return "search_index" }
func (IndexJobArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: "integrations"} }

// AutomationJobArgs → evaluate automation rules against an event.
type AutomationJobArgs struct {
	EventType string `json:"event_type"`
	IssueID   string `json:"issue_id"`
}

func (AutomationJobArgs) Kind() string                 { return "automation" }
func (AutomationJobArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: "default"} }

// GitIngestArgs → parse a commit/PR webhook payload for issue references.
type GitIngestArgs struct {
	Provider string          `json:"provider"`
	Event    string          `json:"event"` // push | pull_request
	Payload  json.RawMessage `json:"payload"`
}

func (GitIngestArgs) Kind() string                 { return "git_ingest" }
func (GitIngestArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: "default"} }

// ObsIngestArgs → create/dedupe an issue from a logging/metrics alert.
type ObsIngestArgs struct {
	Source      string          `json:"source"` // logging|metrics
	ProjectKey  string          `json:"project_key"`
	Fingerprint string          `json:"fingerprint"`
	Payload     json.RawMessage `json:"payload"`
}

func (ObsIngestArgs) Kind() string                 { return "obs_ingest" }
func (ObsIngestArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: "default"} }
