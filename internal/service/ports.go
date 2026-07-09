// Package service holds the application/business logic. It depends only on the ports
// defined here (implemented by internal/repo/pg) and on domain types — never on the
// database driver or generated code directly. This keeps the logic unit-testable.
package service

import (
	"context"
	"encoding/json"
	"time"

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
	CreateAPIToken(ctx context.Context, in CreateTokenInput) (domain.APIToken, error)
	ListAPITokens(ctx context.Context, userID uuid.UUID) ([]domain.APIToken, error)
	RevokeAPIToken(ctx context.Context, userID, tokenID uuid.UUID) (bool, error)

	// Projects
	GetProjectByKey(ctx context.Context, key string) (domain.Project, error)
	ListProjects(ctx context.Context, limit, offset int32) ([]domain.Project, error)
	CreateProject(ctx context.Context, in CreateProjectInput) (domain.Project, error)
	UpdateProject(ctx context.Context, in UpdateProjectInput) (domain.Project, error)
	ListLabels(ctx context.Context, projectKey string) ([]domain.Label, error)

	// Issues (transactional writes take a PublishFn for the outbox)
	CreateIssue(ctx context.Context, in CreateIssueInput, publish PublishIssueFn) (domain.Issue, error)
	GetIssueByKey(ctx context.Context, projectKey string, number int32) (domain.Issue, error)
	ListIssues(ctx context.Context, f IssueFilter) ([]domain.Issue, int, error)
	TransitionIssue(ctx context.Context, id uuid.UUID, to domain.IssueStatus, actor uuid.UUID, publish PublishFn) (domain.Issue, error)
	UpdateIssue(ctx context.Context, id, actor uuid.UUID, in UpdateIssueInput, publish PublishFn) (domain.Issue, error)
	MoveIssue(ctx context.Context, id, actor uuid.UUID, targetProjectKey string, publish PublishFn) (domain.Issue, error)
	SoftDeleteIssue(ctx context.Context, id, actor uuid.UUID, publish PublishFn) error

	// Comments & timeline
	AddComment(ctx context.Context, issueID, author uuid.UUID, body string, publish PublishFn) (domain.Comment, error)
	ListComments(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Comment, error)
	ListActivity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, error)
	RecentActivity(ctx context.Context, limit int32) ([]domain.Activity, error)
	Dashboard(ctx context.Context) (domain.Dashboard, error)
	ListUsers(ctx context.Context, limit int32) ([]domain.User, error)
	UpdateUserRole(ctx context.Context, userID uuid.UUID, role domain.Role) (domain.User, error)

	// Git integration
	UpsertCommit(ctx context.Context, in CommitInput) (uuid.UUID, error)
	UpsertPullRequest(ctx context.Context, in PRInput) (uuid.UUID, error)
	ApplyGitLink(ctx context.Context, in GitLinkInput, publish PublishFn) error
	ListCommitsForIssue(ctx context.Context, issueID uuid.UUID) ([]domain.LinkedCommit, error)
}

type CommitInput struct {
	Repo, SHA, Author, Message, URL string
	CommittedAt                     time.Time
}

type PRInput struct {
	Repo       string
	Number     int
	URL, Title string
	State      string
	MergedAt   *time.Time
}

// GitLinkInput links a commit or PR to an issue, records a timeline entry, and optionally
// transitions the issue — all in one transaction, with publish enqueuing the event.
type GitLinkInput struct {
	IssueID      uuid.UUID
	CommitID     *uuid.UUID
	PRID         *uuid.UUID
	Verb         string // canonical ref_verb
	NewStatus    *domain.IssueStatus
	ActivityVerb string
	Detail       json.RawMessage
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

type CreateProjectInput struct {
	Key           string
	Name          string
	DescriptionMD string
}

// UpdateProjectInput is a partial edit; nil fields are left unchanged (COALESCE).
type UpdateProjectInput struct {
	Key           string
	Name          *string
	DescriptionMD *string
	IsArchived    *bool
}

// CreateTokenInput carries the pre-hashed token; the plaintext is generated and
// returned by the handler (shown once), never stored.
type CreateTokenInput struct {
	UserID    uuid.UUID
	Name      string
	Scopes    []string
	TokenHash []byte
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

// UpdateIssueInput is a partial update: nil fields are left unchanged.
type UpdateIssueInput struct {
	Title           *string
	DescriptionMD   *string
	Type            *domain.IssueType
	Severity        *domain.Severity
	Priority        *domain.Priority
	AssigneeID      *uuid.UUID
	VersionAffected *string
	VersionFixed    *string
	ReproStepsMD    *string
	ExpectedMD      *string
	ActualMD        *string
	EnvironmentMD   *string
	Labels          *[]string // nil = unchanged; non-nil replaces the label set
}

type IssueFilter struct {
	ProjectKey string
	Status     *domain.IssueStatus
	AssigneeID *uuid.UUID
	Type       *domain.IssueType
	Severity   *domain.Severity
	Label      string
	Query      string // full-text
	Sort       string
	Limit      int32
	Offset     int32
}
