package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/events"
)

var (
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrForbidden         = errors.New("forbidden")
)

// Publisher is the subset of events.Publisher the service needs.
type Publisher interface {
	PublishTx(ctx context.Context, tx pgx.Tx, ev events.DomainEventArgs) error
}

// Issues is the issue/bug application service.
type Issues struct {
	repo   Repository
	pub    Publisher
	logger *slog.Logger
}

func NewIssues(repo Repository, pub Publisher, logger *slog.Logger) *Issues {
	return &Issues{repo: repo, pub: pub, logger: logger}
}

// Create inserts a new issue and enqueues issue.created inside the same transaction.
func (s *Issues) Create(ctx context.Context, in CreateIssueInput) (domain.Issue, error) {
	if in.Type == "" {
		in.Type = domain.TypeBug
	}
	if in.Priority == "" {
		in.Priority = domain.P2
	}
	if in.Source == "" {
		in.Source = domain.SourceHuman
	}

	issue, err := s.repo.CreateIssue(ctx, in, func(tx pgx.Tx, created domain.Issue) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueCreated,
			IssueID:   created.ID.String(),
			ActorID:   in.ReporterID.String(),
		})
	})
	if err != nil {
		return domain.Issue{}, err
	}
	return issue, nil
}

// Get fetches an issue by its human key (project key + number).
func (s *Issues) Get(ctx context.Context, projectKey string, number int32) (domain.Issue, error) {
	return s.repo.GetIssueByKey(ctx, projectKey, number)
}

// List returns issues matching the parsed filter.
func (s *Issues) List(ctx context.Context, f IssueFilter) ([]domain.Issue, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	return s.repo.ListIssues(ctx, f)
}

// Transition validates the workflow edge, applies it, and emits the right event.
func (s *Issues) Transition(ctx context.Context, id uuid.UUID, from, to domain.IssueStatus, actor uuid.UUID) (domain.Issue, error) {
	if !domain.CanTransition(from, to) {
		return domain.Issue{}, ErrInvalidTransition
	}
	eventType := events.IssueStatusChanged
	switch to {
	case domain.StatusResolved:
		eventType = events.IssueResolved
	case domain.StatusClosed:
		eventType = events.IssueClosed
	case domain.StatusReopened:
		eventType = events.IssueReopened
	}

	changes, _ := json.Marshal(map[string]any{"status": map[string]string{"from": string(from), "to": string(to)}})
	return s.repo.TransitionIssue(ctx, id, to, actor, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: eventType,
			IssueID:   id.String(),
			ActorID:   actor.String(),
			Payload:   changes,
		})
	})
}

// Update applies a partial edit and emits issue.updated.
func (s *Issues) Update(ctx context.Context, id, actor uuid.UUID, in UpdateIssueInput) (domain.Issue, error) {
	return s.repo.UpdateIssue(ctx, id, actor, in, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueUpdated, IssueID: id.String(), ActorID: actor.String(),
		})
	})
}

// Move re-homes an issue into another project. The issue gets a fresh key in the target
// project (project-scoped number is reallocated), and project-scoped associations
// (milestone, release, components, labels) are reconciled to the destination. Emits
// issue.updated so downstream consumers refresh.
func (s *Issues) Move(ctx context.Context, id, actor uuid.UUID, targetProjectKey string) (domain.Issue, error) {
	return s.repo.MoveIssue(ctx, id, actor, targetProjectKey, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueUpdated, IssueID: id.String(), ActorID: actor.String(),
		})
	})
}

// Delete soft-deletes an issue and emits issue.deleted.
func (s *Issues) Delete(ctx context.Context, id, actor uuid.UUID) error {
	return s.repo.SoftDeleteIssue(ctx, id, actor, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueDeleted, IssueID: id.String(), ActorID: actor.String(),
		})
	})
}

// Comment adds a comment and emits comment.created.
func (s *Issues) Comment(ctx context.Context, issueID, author uuid.UUID, body string) (domain.Comment, error) {
	return s.repo.AddComment(ctx, issueID, author, body, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueCommented,
			IssueID:   issueID.String(),
			ActorID:   author.String(),
		})
	})
}

// Activity returns the issue timeline.
func (s *Issues) Activity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, error) {
	return s.repo.ListActivity(ctx, issueID, limit, offset)
}
