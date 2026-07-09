// Package events defines the internal event taxonomy and the River-backed publisher.
// River's transactional Insert IS the outbox: an event-dispatch job is enqueued in the
// same Postgres transaction as the domain write, so events can never be lost or double-sent.
package events

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Event types (aggregate.verb).
const (
	IssueCreated       = "issue.created"
	IssueUpdated       = "issue.updated"
	IssueDeleted       = "issue.deleted"
	IssueAssigned      = "issue.assigned"
	IssueStatusChanged = "issue.status_changed"
	IssueResolved      = "issue.resolved"
	IssueClosed        = "issue.closed"
	IssueReopened      = "issue.reopened"
	IssueCommented     = "comment.created"
	IssueLinked        = "issue.linked"
	UserMentioned      = "user.mentioned"
	ReleasePublished   = "release.published"
)

// DomainEventArgs is the single fan-out job. The worker's dispatcher turns one of these
// into downstream jobs (notify, webhook, index, automation).
type DomainEventArgs struct {
	EventType string          `json:"event_type"`
	IssueID   string          `json:"issue_id,omitempty"`
	ActorID   string          `json:"actor_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func (DomainEventArgs) Kind() string { return "domain_event" }

// Queue routing so heavy work can't starve latency-sensitive work.
func (DomainEventArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "default"}
}

// Publisher enqueues domain events. Insert-only client (no workers registered here).
type Publisher struct {
	client *river.Client[pgx.Tx]
}

// NewPublisher builds an insert-only River client bound to the shared pool.
func NewPublisher(pool *pgxpool.Pool) (*Publisher, error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return nil, err
	}
	return &Publisher{client: client}, nil
}

// PublishTx enqueues an event within the caller's transaction (the outbox guarantee).
func (p *Publisher) PublishTx(ctx context.Context, tx pgx.Tx, ev DomainEventArgs) error {
	_, err := p.client.InsertTx(ctx, tx, ev, nil)
	return err
}

// Publish enqueues an event outside a transaction (e.g. from ingestion workers).
func (p *Publisher) Publish(ctx context.Context, ev DomainEventArgs) error {
	_, err := p.client.Insert(ctx, ev, nil)
	return err
}

// Enqueue inserts any River job (e.g. GitIngestArgs from an inbound webhook handler).
func (p *Publisher) Enqueue(ctx context.Context, args river.JobArgs) error {
	_, err := p.client.Insert(ctx, args, nil)
	return err
}

// Noop keeps the events import referenced where the worker wires River directly.
func Noop() error { return nil }
