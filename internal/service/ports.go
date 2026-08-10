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

// ObsPublishFn lets the obs-ingest transaction enqueue the resulting domain
// event (issue.created / issue.updated / issue.reopened) atomically.
type ObsPublishFn func(tx pgx.Tx, issue domain.Issue, eventType string) error

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
	// RenameProjectKey changes a project's key. Issue keys are derived (project.key
	// || '-' || number), so every issue re-labels automatically; a collision with an
	// existing key returns an error (enforced by the UNIQUE constraint).
	RenameProjectKey(ctx context.Context, oldKey, newKey string) (domain.Project, error)
	// Labels. ListLabels returns a project's own labels plus the global ones.
	// LabelScope resolves the owning project of a label; a global label reports an
	// empty key with found=true, so the two cases stay distinguishable.
	ListLabels(ctx context.Context, projectKey string) ([]domain.Label, error)
	LabelScope(ctx context.Context, id uuid.UUID) (projectKey string, found bool, err error)
	CreateLabel(ctx context.Context, in CreateLabelInput) (domain.Label, error)
	UpdateLabel(ctx context.Context, in UpdateLabelInput) (domain.Label, error)
	DeleteLabel(ctx context.Context, id uuid.UUID) (bool, error)
	// MergeLabels moves every issue off source onto target and deletes source.
	MergeLabels(ctx context.Context, sourceID, targetID uuid.UUID) (domain.Label, error)

	// Components (project-scoped areas of ownership)
	ListComponents(ctx context.Context, projectKey string) ([]domain.Component, error)
	CreateComponent(ctx context.Context, in CreateComponentInput) (domain.Component, error)
	UpdateComponent(ctx context.Context, in UpdateComponentInput) (domain.Component, error)
	DeleteComponent(ctx context.Context, id uuid.UUID) (bool, error)

	// Milestones
	ListMilestones(ctx context.Context, projectKey string) ([]domain.Milestone, error)
	CreateMilestone(ctx context.Context, in CreateMilestoneInput) (domain.Milestone, error)
	UpdateMilestone(ctx context.Context, in UpdateMilestoneInput) (domain.Milestone, error)
	DeleteMilestone(ctx context.Context, id uuid.UUID) (bool, error)

	// Releases
	GetRelease(ctx context.Context, id uuid.UUID) (domain.Release, error)
	ListReleases(ctx context.Context, projectKey string) ([]domain.Release, error)
	CreateRelease(ctx context.Context, in CreateReleaseInput) (domain.Release, error)
	UpdateRelease(ctx context.Context, in UpdateReleaseInput) (domain.Release, error)
	DeleteRelease(ctx context.Context, id uuid.UUID) (bool, error)

	// Issues (transactional writes take a PublishFn for the outbox)
	CreateIssue(ctx context.Context, in CreateIssueInput, publish PublishIssueFn) (domain.Issue, error)
	GetIssueByKey(ctx context.Context, projectKey string, number int32) (domain.Issue, error)
	GetIssueByID(ctx context.Context, id uuid.UUID) (domain.Issue, error)
	ListIssues(ctx context.Context, f IssueFilter) ([]domain.Issue, int, error)
	// EachIssue streams every issue matching the filter, unpaged. Export needs the
	// whole result set, which is exactly what the paged list is built to avoid.
	EachIssue(ctx context.Context, f IssueFilter, fn func(domain.Issue) error) error
	// FindSimilarIssues ranks a project's live issues against a query built from a
	// draft title, for duplicate detection at filing time.
	FindSimilarIssues(ctx context.Context, projectKey, query, excludeKey string, limit int32) ([]domain.SimilarIssue, error)
	// TransitionIssue applies from → to as a compare-and-set: the write only lands if
	// the row is still in `from`, and returns ErrStaleStatus if somebody moved it first.
	TransitionIssue(ctx context.Context, id uuid.UUID, from, to domain.IssueStatus, actor uuid.UUID, comment string, changes []byte, publish PublishFn) (domain.Issue, error)
	UpdateIssue(ctx context.Context, id, actor uuid.UUID, in UpdateIssueInput, publish PublishFn) (domain.Issue, error)
	MoveIssue(ctx context.Context, id, actor uuid.UUID, targetProjectKey string, publish PublishFn) (domain.Issue, error)
	SoftDeleteIssue(ctx context.Context, id, actor uuid.UUID, publish PublishFn) error
	// SetIssueArchived archives (archived=true) or restores an issue, records an
	// activity entry, and runs publish in the same tx.
	SetIssueArchived(ctx context.Context, id, actor uuid.UUID, archived bool, publish PublishFn) (domain.Issue, error)
	// SetIssueRank writes a card's manual board position.
	SetIssueRank(ctx context.Context, id uuid.UUID, rank string) error
	// NeighbourRanks resolves the ranks either side of a drop position.
	NeighbourRanks(ctx context.Context, projectKey, beforeKey, afterKey string) (string, string, error)
	// Issue templates (per project and type)
	ListIssueTemplates(ctx context.Context, projectKey string) ([]domain.IssueTemplate, error)
	GetIssueTemplate(ctx context.Context, id uuid.UUID) (domain.IssueTemplate, error)
	CreateIssueTemplate(ctx context.Context, in IssueTemplateInput) (domain.IssueTemplate, error)
	UpdateIssueTemplate(ctx context.Context, id uuid.UUID, in UpdateIssueTemplateInput) (domain.IssueTemplate, error)
	DeleteIssueTemplate(ctx context.Context, id uuid.UUID) (bool, error)

	// Custom fields (project-scoped extra structure)
	ListFieldDefinitions(ctx context.Context, projectKey string) ([]domain.FieldDefinition, error)
	CreateFieldDefinition(ctx context.Context, in FieldDefinitionInput) (domain.FieldDefinition, error)
	UpdateFieldDefinition(ctx context.Context, id uuid.UUID, in UpdateFieldDefinitionInput) (domain.FieldDefinition, error)
	DeleteFieldDefinition(ctx context.Context, id uuid.UUID) (bool, error)
	// IssueFieldValues returns every field that applies to the issue, set or not.
	IssueFieldValues(ctx context.Context, issueID uuid.UUID) ([]domain.FieldValue, error)
	// SetIssueFieldValues writes a batch; an empty value clears the field.
	SetIssueFieldValues(ctx context.Context, issueID uuid.UUID, values map[string]any) error
	// ResolveFieldFilter turns a `field:<key>:<value>` term into a SQL predicate, or
	// a validation message when the key or the value does not fit the definition.
	ResolveFieldFilter(ctx context.Context, projectKey, key, value string) (FieldPredicate, error)

	// Iterations (time-boxed planning)
	ListIterations(ctx context.Context, projectKey string) ([]domain.Iteration, error)
	GetIteration(ctx context.Context, id uuid.UUID) (domain.Iteration, error)
	CreateIteration(ctx context.Context, in IterationInput) (domain.Iteration, error)
	UpdateIteration(ctx context.Context, id uuid.UUID, in UpdateIterationInput) (domain.Iteration, error)
	DeleteIteration(ctx context.Context, id uuid.UUID) (bool, error)
	// SetIssueIteration moves one issue in or out; nil removes it from any iteration.
	SetIssueIteration(ctx context.Context, issueID uuid.UUID, iteration *uuid.UUID, actor uuid.UUID) error
	// CarryOverIssues moves unfinished work between iterations, explicitly.
	CarryOverIssues(ctx context.Context, from uuid.UUID, to *uuid.UUID) (int, error)
	IterationBurndown(ctx context.Context, id uuid.UUID) ([]domain.BurndownPoint, error)
	// SnapshotIterations records today's remaining work for every active iteration.
	SnapshotIterations(ctx context.Context) (int, error)
	IterationVelocity(ctx context.Context, projectKey string, limit int) ([]domain.Velocity, error)
	// ResolveIteration turns an `iteration:` filter value (current/next/name/uuid)
	// into an id; found=false means the filter must match nothing.
	ResolveIteration(ctx context.Context, projectKey, ref string) (uuid.UUID, bool, error)

	// Time tracking
	ListTimeEntries(ctx context.Context, issueID uuid.UUID) ([]domain.TimeEntry, error)
	LogTime(ctx context.Context, in TimeEntryInput) (domain.TimeEntry, error)
	// DeleteTimeEntry removes an entry; force lets a project maintainer delete
	// somebody else's, which is checked at the HTTP layer.
	DeleteTimeEntry(ctx context.Context, id, userID uuid.UUID, force bool) (bool, error)
	// EffortForIssues rolls up estimate-vs-spent over any filter the list can express.
	EffortForIssues(ctx context.Context, f IssueFilter) (domain.EffortRollup, error)
	MilestoneEffort(ctx context.Context, projectKey string) (map[uuid.UUID]domain.EffortRollup, error)
	ReleaseEffort(ctx context.Context, projectKey string) (map[uuid.UUID]domain.EffortRollup, error)
	ComponentEffort(ctx context.Context, projectKey string) (map[uuid.UUID]domain.EffortRollup, error)
	// RemainingByAssignee is open estimated work per person, in minutes.
	RemainingByAssignee(ctx context.Context, projectKey string) (map[string]int, error)

	// SLA policies (project-scoped response/resolution budgets)
	ListSLAPolicies(ctx context.Context, projectKey string) ([]domain.SLAPolicy, error)
	CreateSLAPolicy(ctx context.Context, in SLAPolicyInput) (domain.SLAPolicy, error)
	UpdateSLAPolicy(ctx context.Context, id uuid.UUID, in UpdateSLAPolicyInput) (domain.SLAPolicy, error)
	DeleteSLAPolicy(ctx context.Context, id uuid.UUID) (bool, error)
	// ClaimSLAEscalations atomically claims each (issue, threshold) crossing exactly
	// once, so a warning cannot re-fire on every worker run.
	ClaimSLAEscalations(ctx context.Context) ([]SLAEscalation, error)

	// SetIssueSnooze hides an issue until a time, or wakes it when until is nil.
	SetIssueSnooze(ctx context.Context, id, actor uuid.UUID, until *time.Time, note string, publish PublishFn) (domain.Issue, error)
	// WakeSnoozedIssues clears every snooze that has come due, returning the ids.
	WakeSnoozedIssues(ctx context.Context) ([]uuid.UUID, error)
	// ArchiveStaleClosed archives every non-archived issue closed more than `days`
	// ago (attributed to actor). Returns how many were archived. Used by auto-archive.
	ArchiveStaleClosed(ctx context.Context, days int, actor uuid.UUID) (int, error)
	// GetSetting returns a runtime setting's JSONB value, or (nil, nil) if unset.
	GetSetting(ctx context.Context, key string) ([]byte, error)
	// SetSetting upserts a runtime setting's JSONB value.
	SetSetting(ctx context.Context, key string, value []byte) error

	// ProjectKeyForEntity resolves which project a component/milestone/release
	// belongs to (for permission checks on id-addressed endpoints).
	ProjectKeyForEntity(ctx context.Context, entity string, id uuid.UUID) (string, error)

	// Project membership (per-project role elevation)
	ListProjectMembers(ctx context.Context, projectKey string) ([]domain.ProjectMember, error)
	GetProjectRole(ctx context.Context, projectKey string, userID uuid.UUID) (domain.Role, bool, error)
	UpsertProjectMember(ctx context.Context, projectKey string, userID uuid.UUID, role domain.Role) (domain.ProjectMember, error)
	RemoveProjectMember(ctx context.Context, projectKey string, userID uuid.UUID) (bool, error)

	// Observability ingest: create or bump an issue for an alert fingerprint.
	// Returns the issue and whether it was newly created.
	IngestObsAlert(ctx context.Context, in ObsAlertInput, publish ObsPublishFn) (domain.Issue, bool, error)

	// Boards (configurable Kanban)
	GetOrCreateBoard(ctx context.Context, projectKey string) (domain.Board, error)
	UpdateBoard(ctx context.Context, id uuid.UUID, name, swimlane *string) (domain.Board, error)
	CreateBoardColumn(ctx context.Context, boardID uuid.UUID, in BoardColumnInput) (domain.Board, error)
	UpdateBoardColumn(ctx context.Context, columnID uuid.UUID, in UpdateBoardColumnInput) (domain.Board, error)
	DeleteBoardColumn(ctx context.Context, columnID uuid.UUID) (domain.Board, bool, error)

	// Automation rules (trigger → actions on issue events)
	ListAutomationRules(ctx context.Context) ([]domain.AutomationRule, error)
	MatchingAutomationRules(ctx context.Context, projectKey, event string) ([]domain.AutomationRule, error)
	CreateAutomationRule(ctx context.Context, in CreateAutomationRuleInput) (domain.AutomationRule, error)
	UpdateAutomationRule(ctx context.Context, in UpdateAutomationRuleInput) (domain.AutomationRule, error)
	DeleteAutomationRule(ctx context.Context, id uuid.UUID) (bool, error)
	ListAutomationRuns(ctx context.Context, limit int32) ([]domain.AutomationRun, error)
	RecordAutomationRun(ctx context.Context, ruleID, issueID uuid.UUID, status string, log []byte) error
	// EnsureBotUser upserts a system user (e.g. the automation actor) and returns its id.
	EnsureBotUser(ctx context.Context, identitySub, displayName, email string) (uuid.UUID, error)

	// Webhooks (outbound event subscriptions)
	ListWebhooks(ctx context.Context) ([]domain.Webhook, error)
	CreateWebhook(ctx context.Context, in CreateWebhookInput) (domain.Webhook, error)
	UpdateWebhook(ctx context.Context, in UpdateWebhookInput) (domain.Webhook, error)
	DeleteWebhook(ctx context.Context, id uuid.UUID) (bool, error)
	ListWebhookDeliveries(ctx context.Context, webhookID uuid.UUID, limit int32) ([]domain.WebhookDelivery, error)
	GetWebhookDelivery(ctx context.Context, id uuid.UUID) (domain.WebhookDelivery, error)
	ResetWebhookDelivery(ctx context.Context, id uuid.UUID) error

	// Saved searches (personal named filters)
	// The audit log is append-only: there is no update or delete in this interface,
	// deliberately.
	RecordAudit(ctx context.Context, in AuditEntry) error
	ListAudit(ctx context.Context, f AuditFilter) ([]domain.AuditEntry, int, error)

	ListSavedSearches(ctx context.Context, userID uuid.UUID) ([]domain.SavedSearch, error)
	// Shared views are project-scoped saved searches: the same rows, owned by a
	// project rather than a person.
	ListProjectViews(ctx context.Context, projectKey string) ([]domain.SavedSearch, error)
	// ListSharedViews spans every project, for curating them in one place.
	ListSharedViews(ctx context.Context) ([]domain.SavedSearch, error)
	CreateProjectView(ctx context.Context, in ProjectViewInput) (domain.SavedSearch, error)
	UpdateProjectView(ctx context.Context, in UpdateProjectViewInput) (domain.SavedSearch, error)
	DeleteProjectView(ctx context.Context, id uuid.UUID) (bool, error)
	GetViewProjectKey(ctx context.Context, id uuid.UUID) (string, error)
	ShareSavedSearch(ctx context.Context, userID, id uuid.UUID, projectKey string) (domain.SavedSearch, error)
	UpsertSavedSearch(ctx context.Context, userID uuid.UUID, name, query string) (domain.SavedSearch, error)
	// UpdateSavedSearch edits a personal view in place — the only way to rename one,
	// since upsert-by-name would create a second view instead.
	UpdateSavedSearch(ctx context.Context, userID uuid.UUID, in UpdateSavedSearchInput) (domain.SavedSearch, error)
	DeleteSavedSearch(ctx context.Context, userID, id uuid.UUID) (bool, error)

	// Watchers (issue subscriptions; auto-watch on report/comment/assign)
	GetNotificationPrefs(ctx context.Context, userID uuid.UUID) (map[string]string, error)
	SetNotificationPrefs(ctx context.Context, userID uuid.UUID, prefs, defaults map[string]string) error
	SetIssueMute(ctx context.Context, issueID, userID uuid.UUID, muted bool) error
	// RouteRecipients applies per-user preferences and per-issue mutes, splitting
	// candidates into inbox and push audiences.
	RouteRecipients(ctx context.Context, issueID uuid.UUID, eventType string, emails []string, defaults map[string]string) (inbox, push []string, err error)

	// RecordNotifications writes the in-app inbox rows for a notify job's recipients.
	RecordNotifications(ctx context.Context, issueID uuid.UUID, eventType string, actorID *uuid.UUID, emails []string) error
	ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit, offset int32) ([]domain.Notification, int, error)
	MarkNotificationsRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (int, error)
	MarkIssueNotificationsRead(ctx context.Context, userID, issueID uuid.UUID) error

	ListWatchers(ctx context.Context, issueID uuid.UUID) ([]domain.User, error)
	IsWatcher(ctx context.Context, issueID, userID uuid.UUID) (bool, error)
	SetWatcher(ctx context.Context, issueID, userID uuid.UUID, watching bool) error

	// Issue relations (blocks / duplicates / relates / caused_by)
	// Reactions are aggregated per target and emoji; viewerID resolves "mine".
	ToggleReaction(ctx context.Context, issueID uuid.UUID, commentID *uuid.UUID, userID uuid.UUID, emoji string) (bool, error)
	ListReactions(ctx context.Context, issueID, viewerID uuid.UUID) ([]domain.Reaction, error)
	ListRelations(ctx context.Context, issueID uuid.UUID) ([]domain.IssueRelation, error)
	// ClaimPendingMentions stamps and returns the email addresses of everyone newly
	// @mentioned on an issue. The claim is the stamp, so a mention notifies exactly
	// once however many events fire for the issue.
	ClaimPendingMentions(ctx context.Context, issueID uuid.UUID) ([]string, error)
	// ListReferencedBy returns the issues whose prose mentions this one. Derived from
	// the text on every write, unlike relations, which are created deliberately.
	ListReferencedBy(ctx context.Context, issueID uuid.UUID) ([]domain.IssueReference, error)
	CreateRelation(ctx context.Context, fromIssue, toIssue uuid.UUID, kind string, actor uuid.UUID) (domain.IssueRelation, error)
	GetRelationProjectKeys(ctx context.Context, id uuid.UUID) (fromKey, toKey string, err error)
	DeleteRelation(ctx context.Context, id, actor uuid.UUID) (bool, error)

	// Global search (Postgres FTS over issues + comments)
	Search(ctx context.Context, query string, limit int32) ([]domain.SearchHit, error)

	// Attachments (metadata; bytes live on local disk under ObjectKey)
	CreateAttachment(ctx context.Context, in CreateAttachmentInput) (domain.Attachment, error)
	ListAttachmentsForIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Attachment, error)
	GetAttachment(ctx context.Context, id uuid.UUID) (domain.Attachment, error)
	DeleteAttachment(ctx context.Context, id uuid.UUID) (objectKey string, found bool, err error)

	// Comments & timeline
	AddComment(ctx context.Context, issueID, author uuid.UUID, body string, publish PublishFn) (domain.Comment, error)
	// ListComments / ListActivity return one page plus the unpaged total.
	ListComments(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Comment, int, error)
	GetComment(ctx context.Context, id uuid.UUID) (domain.Comment, error)
	// UpdateComment rewrites a body and re-syncs its references and mentions. publish
	// runs in the same tx — without it a newly-added @mention is recorded and never
	// notified.
	UpdateComment(ctx context.Context, id, actor uuid.UUID, bodyMD string, publish PublishFn) (domain.Comment, error)
	SoftDeleteComment(ctx context.Context, id, actor uuid.UUID) (bool, error)
	ListActivity(ctx context.Context, issueID uuid.UUID, limit, offset int32) ([]domain.Activity, int, error)
	RecentActivity(ctx context.Context, projectKey string, limit int32) ([]domain.Activity, error)
	// Dashboard aggregates health metrics; an empty projectKey spans every project.
	Dashboard(ctx context.Context, projectKey string) (domain.Dashboard, error)
	// OpsSnapshot is the queue and delivery health view for admins.
	OpsSnapshot(ctx context.Context) (domain.OpsSnapshot, error)
	// Report is the trend view over a date range; see ReportFilter.
	Report(ctx context.Context, f ReportFilter) (domain.Report, error)
	// ListUsers returns active users; includeInactive adds deactivated accounts, which
	// only the admin screen wants — an assignee picker must never offer a leaver.
	ListUsers(ctx context.Context, limit int32, includeInactive bool) ([]domain.User, error)
	UpdateUserRole(ctx context.Context, userID uuid.UUID, role domain.Role) (domain.User, error)
	// SetUserActive gates authentication for a user, revoking their API tokens when
	// deactivating.
	SetUserActive(ctx context.Context, userID uuid.UUID, active bool) (domain.User, error)
	// UpdateUserProfile writes the fields a user owns about themselves and marks the
	// profile overridden so the next OIDC login stops mirroring over them.
	UpdateUserProfile(ctx context.Context, userID uuid.UUID, in UpdateProfileInput) (domain.User, error)

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

// UpdateProfileInput is a self-service profile edit. A nil field is left unchanged;
// email and role are deliberately absent — the IdP owns one and admins own the other.
type UpdateProfileInput struct {
	DisplayName *string
	AvatarURL   *string
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

// CreateLabelInput creates a label ahead of use. An empty ProjectKey makes it global.
type CreateLabelInput struct {
	ProjectKey  string
	Name        string
	Color       string
	Description string
}

// UpdateLabelInput edits a label in place; nil fields are left alone. Renaming is the
// point of it — every issue carrying the label follows the rename.
type UpdateLabelInput struct {
	ID          uuid.UUID
	Name        *string
	Color       *string
	Description *string
}

type CreateComponentInput struct {
	ProjectKey    string
	Name          string
	DescriptionMD string
	LeadID        *uuid.UUID
}

// UpdateComponentInput is a partial edit; nil = unchanged. LeadID follows the
// zero-UUID-clears convention.
type UpdateComponentInput struct {
	ID            uuid.UUID
	Name          *string
	DescriptionMD *string
	LeadID        *uuid.UUID
}

type CreateMilestoneInput struct {
	ProjectKey    string
	Title         string
	DescriptionMD string
	DueOn         *time.Time
}

// UpdateMilestoneInput is a partial edit; nil = unchanged. ClearDueOn removes
// the due date (DueOn nil alone means "leave as is").
type UpdateMilestoneInput struct {
	ID            uuid.UUID
	Title         *string
	DescriptionMD *string
	DueOn         *time.Time
	ClearDueOn    bool
	State         *string // open | closed
}

type CreateReleaseInput struct {
	ProjectKey string
	Version    string
	Name       string
	NotesMD    string
	GitTag     string
	CreatedBy  uuid.UUID
}

// UpdateReleaseInput is a partial edit; nil = unchanged. Setting State to
// "published" stamps released_at on first publish.
type UpdateReleaseInput struct {
	ID      uuid.UUID
	Version *string
	Name    *string
	NotesMD *string
	GitTag  *string
	State   *string // draft | published
}

// UpdateProjectInput is a partial edit; nil fields are left unchanged (COALESCE).
// DefaultAssigneeID follows the issue-assignee convention: nil = unchanged,
// zero UUID = clear, otherwise set.
type UpdateProjectInput struct {
	Key               string
	Name              *string
	DescriptionMD     *string
	IsArchived        *bool
	DefaultAssigneeID *uuid.UUID
}

// CreateTokenInput carries the pre-hashed token; the plaintext is generated and
// returned by the handler (shown once), never stored.
type CreateTokenInput struct {
	UserID    uuid.UUID
	Name      string
	Scopes    []string
	TokenHash []byte
}

// ObsAlertInput is a normalized observability alert (from omnilog / omni-metrics).
type ObsAlertInput struct {
	Source      string // logging | metrics
	ProjectKey  string
	Fingerprint string
	Title       string
	Rule        string
	DetailsMD   string
	StackTrace  string
	Severity    *domain.Severity
}

type BoardColumnInput struct {
	Name     string
	Statuses []string
	WipLimit *int
}

// UpdateBoardColumnInput is a partial edit; nil = unchanged. ClearWip removes
// the limit; Position swaps ordering with the neighbor at the target slot.
type UpdateBoardColumnInput struct {
	Name     *string
	Statuses *[]string
	WipLimit *int
	ClearWip bool
	Position *int
}

type CreateAutomationRuleInput struct {
	ProjectKey string // empty = all projects
	Name       string
	Priority   int
	Trigger    json.RawMessage
	Actions    json.RawMessage
	CreatedBy  uuid.UUID
}

// UpdateAutomationRuleInput is a partial edit; nil = unchanged.
type UpdateAutomationRuleInput struct {
	ID       uuid.UUID
	Name     *string
	Priority *int
	IsActive *bool
	Trigger  json.RawMessage // nil = unchanged
	Actions  json.RawMessage // nil = unchanged
}

type CreateWebhookInput struct {
	ProjectKey string // empty = all projects
	URL        string
	Secret     string
	Events     []string
	CreatedBy  uuid.UUID
}

// UpdateWebhookInput is a partial edit; nil = unchanged. An empty *Secret clears it.
type UpdateWebhookInput struct {
	ID       uuid.UUID
	URL      *string
	Secret   *string
	Events   *[]string
	IsActive *bool
}

type CreateAttachmentInput struct {
	IssueID     uuid.UUID
	UploaderID  uuid.UUID
	Filename    string
	ContentType string
	SizeBytes   int64
	ObjectKey   string
	Checksum    string
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
	DueAt           *time.Time
	EstimateMinutes *int
}

// ProjectViewInput creates a shared view.
type ProjectViewInput struct {
	ProjectKey  string
	AuthorID    uuid.UUID
	Name        string
	Query       string
	Description string
	Sort        string
}

// UpdateProjectViewInput is a partial edit; nil fields are left unchanged.
type UpdateProjectViewInput struct {
	ID          uuid.UUID
	Name        *string
	Query       *string
	Description *string
	Sort        *string
	Position    *int
	IsDefault   *bool
}

// UpdateSavedSearchInput is a partial edit to a personal view; nil fields unchanged.
type UpdateSavedSearchInput struct {
	ID          uuid.UUID
	Name        *string
	Query       *string
	Description *string
	Sort        *string
}

// IssueTemplateInput creates a template.
type IssueTemplateInput struct {
	ProjectKey        string
	Name              string
	Type              domain.IssueType
	BodyMD            string
	RequiredSections  []string
	DefaultLabels     []string
	DefaultComponent  string
	DefaultPriority   *string
	DefaultSeverity   *string
	DefaultAssigneeID *string
	IsDefault         bool
}

// UpdateIssueTemplateInput is a partial edit. The issue type is not editable: a
// template's defaults and required sections are written for one type, and moving it
// would silently apply the wrong shape to every issue filed from it afterwards.
type UpdateIssueTemplateInput struct {
	Name             *string
	BodyMD           *string
	RequiredSections *[]string
	DefaultLabels    *[]string
	DefaultComponent *string
	DefaultPriority  *string
	DefaultSeverity  *string
	IsDefault        *bool
	Position         *int
}

// FieldDefinitionInput declares a custom field on a project.
type FieldDefinitionInput struct {
	ProjectKey string
	Key        string
	Label      string
	Type       string
	Options    []string
	Required   bool
	AppliesTo  []domain.IssueType
	HelpText   string
}

// UpdateFieldDefinitionInput is a partial edit. Key and Type are deliberately absent:
// the key is what saved searches reference, and changing the type would strand every
// existing value in the wrong column.
type UpdateFieldDefinitionInput struct {
	Label     *string
	Options   *[]string
	Required  *bool
	AppliesTo *[]domain.IssueType
	HelpText  *string
	Position  *int
}

// FieldPredicate is a resolved `field:<key>:<value>` term. Problem is set instead of
// the rest when the term does not fit the definition, so the caller can answer 422
// naming the field rather than letting a failed cast become a 500.
type FieldPredicate struct {
	DefinitionID  uuid.UUID
	Type          string
	Column        string
	Cast          string
	Arg           any
	ArrayContains bool
	Problem       string
}

// IterationInput creates a time-boxed iteration. Dates are "YYYY-MM-DD".
type IterationInput struct {
	ProjectKey string
	Name       string
	StartsOn   string
	EndsOn     string
	Goal       string
}

// UpdateIterationInput is a partial edit; nil fields are unchanged. Setting State to
// "active" stands down whichever iteration currently holds it — see UpdateIteration.
type UpdateIterationInput struct {
	Name     *string
	StartsOn *string
	EndsOn   *string
	Goal     *string
	State    *string
}

// TimeEntryInput logs one stretch of work. An empty SpentOn means today.
type TimeEntryInput struct {
	IssueID uuid.UUID
	UserID  uuid.UUID
	Minutes int
	SpentOn string // YYYY-MM-DD, empty = today
	Note    string
}

// SLAPolicyInput creates a project SLA policy. A nil Severity or Type means "any".
type SLAPolicyInput struct {
	ProjectKey        string
	Severity          *domain.Severity
	Type              *domain.IssueType
	ResponseMinutes   int
	ResolutionMinutes int
}

// UpdateSLAPolicyInput is a partial edit. The scope (severity/type) is deliberately
// not editable — changing it would silently re-target every issue the policy governs,
// and the history of what was promised would be gone. Delete and recreate instead.
type UpdateSLAPolicyInput struct {
	ResponseMinutes   *int
	ResolutionMinutes *int
	IsActive          *bool
}

// SLAEscalation is one claimed threshold crossing, ready to announce.
type SLAEscalation struct {
	IssueID uuid.UUID
	Kind    string // response_warning | response_breached | resolution_warning | resolution_breached
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
	Labels          *[]string  // nil = unchanged; non-nil replaces the label set
	Components      *[]string  // nil = unchanged; non-nil replaces (names must exist in the project)
	MilestoneID     *uuid.UUID // nil = unchanged; zero UUID clears; must belong to the issue's project
	ReleaseID       *uuid.UUID // nil = unchanged; zero UUID clears; must belong to the issue's project
	// DueAt follows the same convention: nil = unchanged, the zero time clears it.
	DueAt *time.Time
	// EstimateMinutes: nil = unchanged, 0 clears, >0 sets.
	EstimateMinutes *int
}

type IssueFilter struct {
	// ProjectKey scopes the list to one project. Empty means every project the
	// caller can see — the cross-project "My work" queue relies on this.
	ProjectKey string
	// Statuses is a set: the issue matches when its status is any of these. Empty
	// means "no status constraint". `is:open` / `is:closed` expand to lifecycle
	// sets rather than a single value, so this is never a lone status.
	Statuses    []domain.IssueStatus
	AssigneeID  *uuid.UUID
	Type        *domain.IssueType
	Severity    *domain.Severity
	Label       string
	Component   string
	MilestoneID *uuid.UUID
	ReleaseID   *uuid.UUID
	Query       string // full-text
	// ShowArchived flips the list to archived-only. Default (false) excludes archived
	// issues; set via the `is:archived` filter term.
	ShowArchived bool
	ShowSnoozed  bool
	// ReporterID narrows to issues somebody filed (`reporter:@me`).
	ReporterID *uuid.UUID
	// Watching / Mentioned are "issues I have a stake in", resolved against MeUserID.
	// Both are no-ops when MeUserID is nil, so an unresolvable @me matches nothing
	// rather than everything.
	Watching  bool
	Mentioned bool
	MeUserID  *uuid.UUID
	// Due-date predicates from the `due:` term. DueOverdue and DueNone are separate
	// booleans rather than a DueBefore of now(), because "late" also requires the
	// issue to still be unfinished — an issue delivered a day late is not overdue now.
	DueOverdue bool
	DueNone    bool
	DueAny     bool
	DueBefore  *time.Time
	DueAfter   *time.Time
	// SLAStates filters on the worse of an issue's two SLA states; SLANone matches
	// issues no policy applies to, which is not the same as "meeting its targets".
	SLAStates []string
	SLANone   bool
	// IterationID scopes to one iteration; IterationNone matches unplanned issues.
	// An unresolvable `iteration:current` sets NoIteration so it matches nothing
	// rather than silently widening to the whole project.
	IterationID   *uuid.UUID
	IterationNone bool
	// IterationRef is the unresolved `iteration:` value ("current", "next", a name
	// or a uuid). ParseFilter records it; the handler resolves it against the store.
	IterationRef string
	// MatchNothing is set when a filter term resolved to nothing that exists. It has
	// to be its own flag: dropping an unresolvable constraint would widen the result
	// set to everything, which is the opposite of what was asked for.
	MatchNothing bool
	// FieldTerms are unresolved `field:<key>:<value>` terms; the handler resolves
	// them against the project's definitions. FieldPredicates is what issueWhere reads.
	FieldTerms      [][2]string
	FieldPredicates []FieldPredicate
	// Effort predicates from `estimate:`, `spent:` and `over-budget:`.
	EstimateNone bool
	EstimateAny  bool
	SpentOver    *int // minutes
	SpentUnder   *int // minutes
	OverBudget   *bool
	Sort         string
	Limit        int32
	Offset       int32
}
