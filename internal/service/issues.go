package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/events"
)

var (
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrForbidden         = errors.New("forbidden")
	// ErrStaleStatus means the issue moved between the caller reading its status and
	// the write landing — the compare-and-set in TransitionIssue found a different
	// `from`. The caller saw a valid edge; it just is not the edge that is available
	// any more. Answered as 409, because retrying from the current state may well
	// succeed and the client needs to re-read to decide.
	ErrStaleStatus = errors.New("issue status changed while you were working on it")
)

// Publisher is the subset of events.Publisher the service needs.
type Publisher interface {
	PublishTx(ctx context.Context, tx pgx.Tx, ev events.DomainEventArgs) error
	// EnqueueWebhook schedules an outbound delivery (webhook redelivery).
	EnqueueWebhook(ctx context.Context, args events.WebhookJobArgs) error
	// EnqueueAutoArchive runs the auto-archive sweep once, immediately.
	EnqueueAutoArchive(ctx context.Context) error
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
	issue, err := s.repo.GetIssueByKey(ctx, projectKey, number)
	if err != nil {
		return issue, err
	}
	issue.Checklist = checklistFor(issue.DescriptionMD)
	return issue, nil
}

// List returns issues matching the parsed filter.
func (s *Issues) List(ctx context.Context, f IssueFilter) ([]domain.Issue, int, error) {
	// Page bounds are enforced in one place — the repository (clampLimit/clampOffset).
	// A second copy here silently re-capped an over-max limit to the default, which is
	// how `?limit=500` came back as 50 even after the repo learned to clamp down.
	issues, total, err := s.repo.ListIssues(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	// Checklist progress is derived from the description the row already carries, so
	// it costs nothing extra — and a definition-of-done nobody can see the state of
	// without opening the issue is a definition-of-done nobody uses.
	for i := range issues {
		issues[i].Checklist = checklistFor(issues[i].DescriptionMD)
	}
	return issues, total, nil
}

// Transition validates the workflow edge, applies it, and emits the right event.
// A non-empty comment is recorded against the issue in the same transaction and
// emits comment.created too, so watchers hear about the explanation and not just
// the state change.
func (s *Issues) Transition(ctx context.Context, id uuid.UUID, from, to domain.IssueStatus, actor uuid.UUID, comment string) (domain.Issue, error) {
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
	commented := strings.TrimSpace(comment) != ""
	return s.repo.TransitionIssue(ctx, id, from, to, actor, comment, changes, func(tx pgx.Tx) error {
		if err := s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: eventType,
			IssueID:   id.String(),
			ActorID:   actor.String(),
			Payload:   changes,
		}); err != nil {
			return err
		}
		if !commented {
			return nil
		}
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.IssueCommented,
			IssueID:   id.String(),
			ActorID:   actor.String(),
		})
	})
}

// SetArchived archives (archived=true) or restores an issue — hidden from default
// lists and search but recoverable, and its status is untouched. Emits
// issue.archived / issue.unarchived.
func (s *Issues) SetArchived(ctx context.Context, id, actor uuid.UUID, archived bool) (domain.Issue, error) {
	eventType := events.IssueUnarchived
	if archived {
		eventType = events.IssueArchived
	}
	return s.repo.SetIssueArchived(ctx, id, actor, archived, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: eventType,
			IssueID:   id.String(),
			ActorID:   actor.String(),
		})
	})
}

// SetSnooze hides an issue until `until`, or wakes it when until is nil.
func (s *Issues) SetSnooze(
	ctx context.Context, id, actor uuid.UUID, until *time.Time, note string,
) (domain.Issue, error) {
	eventType := events.IssueWoke
	if until != nil {
		eventType = events.IssueSnoozed
	}
	return s.repo.SetIssueSnooze(ctx, id, actor, until, note, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: eventType,
			IssueID:   id.String(),
			ActorID:   actor.String(),
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

// EditComment rewrites a comment and emits comment.edited.
//
// The event is what makes a newly-added @mention reach anybody: syncMentions records
// the row inside the write, and the dispatcher's mention fan-out only runs off an
// enqueued event. Editing used to call the store directly with no hook at all.
// issueID is passed rather than looked up: the mention fan-out keys entirely off
// ev.IssueID and silently does nothing when it is empty, so an event without it would
// reintroduce the exact bug this method exists to fix. Every caller has already loaded
// the comment to authorise the edit, so it costs nothing.
func (s *Issues) EditComment(
	ctx context.Context, issueID, commentID, actor uuid.UUID, body string,
) (domain.Comment, error) {
	return s.repo.UpdateComment(ctx, commentID, actor, body, func(tx pgx.Tx) error {
		return s.pub.PublishTx(ctx, tx, events.DomainEventArgs{
			EventType: events.CommentEdited,
			IssueID:   issueID.String(),
			ActorID:   actor.String(),
		})
	})
}

// Activity returns one page of the issue timeline plus the unpaged total.
func (s *Issues) Activity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, int, error) {
	return s.repo.ListActivity(ctx, issueID, limit, offset)
}
