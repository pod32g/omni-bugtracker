// Package service holds the application/business logic. It depends only on the ports
// defined here (implemented by internal/repo/pg) and on domain types — never on the
// database driver or generated code directly. This keeps the logic unit-testable.
package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omni/bugtracker/internal/domain"
)

// PublishFn enqueues an event inside the repository's transaction (the outbox hook).
type PublishFn func(tx pgx.Tx) error

// PublishIssueFn is the create-time variant that receives the freshly-inserted issue,
// so the event payload can carry the generated ID/key.
type PublishIssueFn func(tx pgx.Tx, created domain.Issue) error

// Repository is the persistence port. The pg adapter runs the multi-statement writes
// (allocate number → insert → record activity → publish) inside a single transaction.
type Repository interface {
	// Auth / users
	UpsertUser(ctx context.Context, in UpsertUserParams) (domain.User, error)
	GetUserByToken(ctx context.Context, tokenHash []byte) (TokenPrincipal, error)
	TouchToken(ctx context.Context, tokenID uuid.UUID) error

	// Projects
	GetProjectByKey(ctx context.Context, key string) (domain.Project, error)

	// Issues (transactional writes take a PublishFn for the outbox)
	CreateIssue(ctx context.Context, in CreateIssueInput, publish PublishIssueFn) (domain.Issue, error)
	GetIssueByKey(ctx context.Context, projectKey string, number int32) (domain.Issue, error)
	ListIssues(ctx context.Context, f IssueFilter) ([]domain.Issue, int, error)
	TransitionIssue(ctx context.Context, id uuid.UUID, to domain.IssueStatus, actor uuid.UUID, publish PublishFn) (domain.Issue, error)

	// Comments & timeline
	AddComment(ctx context.Context, issueID, author uuid.UUID, body string, publish PublishFn) (domain.Comment, error)
	ListComments(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Comment, error)
	ListActivity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, error)
}

type UpsertUserParams struct {
	IdentitySub string
	Email       string
	DisplayName string
	AvatarURL   string
}

type TokenPrincipal struct {
	TokenID uuid.UUID
	User    domain.User
	Scopes  []string
}

type CreateIssueInput struct {
	ProjectKey      string
	Type            domain.IssueType
	Title           string
	DescriptionMD   string
	Severity        *domain.Severity
	Priority        domain.Priority
	ReporterID      uuid.UUID
	AssigneeID      *uuid.UUID
	Labels          []string
	Components      []string
	VersionAffected string
	ReproStepsMD    string
	ExpectedMD      string
	ActualMD        string
	EnvironmentMD   string
	Source          domain.IssueSource
	DedupeKey       *string
}

type IssueFilter struct {
	ProjectKey string
	Status     *domain.IssueStatus
	AssigneeID *uuid.UUID
	Type       *domain.IssueType
	Severity   *domain.Severity
	Query      string // full-text
	Sort       string
	Limit      int32
	Offset     int32
}
