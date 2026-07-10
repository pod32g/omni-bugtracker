package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID          uuid.UUID `json:"id"`
	IdentitySub string    `json:"-"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url"`
	Role        Role      `json:"role"`
}

type Project struct {
	ID                uuid.UUID  `json:"id"`
	Key               string     `json:"key"`
	Name              string     `json:"name"`
	DescriptionMD     string     `json:"description_md"`
	DefaultAssigneeID *uuid.UUID `json:"default_assignee_id,omitempty"`
	IsArchived        bool       `json:"is_archived"`
	CreatedAt         time.Time  `json:"created_at"`
}

// APIToken is metadata about a personal API token. The secret itself is never
// returned after creation — only its SHA-256 hash is stored.
type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Milestone is a project-scoped goalpost issues can be grouped under.
type Milestone struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	DescriptionMD string     `json:"description_md"`
	DueOn         *time.Time `json:"due_on,omitempty"`
	State         string     `json:"state"` // open | closed
	OpenIssues    int        `json:"open_issues"`
	ClosedIssues  int        `json:"closed_issues"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Release is a project-scoped shippable version issues can be targeted at.
type Release struct {
	ID         uuid.UUID  `json:"id"`
	Version    string     `json:"version"` // e.g. "2.1.0"
	Name       string     `json:"name"`
	NotesMD    string     `json:"notes_md"`
	State      string     `json:"state"` // draft | published
	GitTag     string     `json:"git_tag"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
	OpenIssues int        `json:"open_issues"`
	DoneIssues int        `json:"done_issues"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Component is a project-scoped area of ownership (e.g. "api", "web", "infra").
type Component struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	DescriptionMD string     `json:"description_md"`
	LeadID        *uuid.UUID `json:"lead_id,omitempty"`
	OpenIssues    int        `json:"open_issues"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Issue struct {
	ID              uuid.UUID   `json:"id"`
	Key             string      `json:"key"` // e.g. BUG-421
	ProjectKey      string      `json:"project_key"`
	Number          int32       `json:"number"`
	Type            IssueType   `json:"type"`
	Title           string      `json:"title"`
	DescriptionMD   string      `json:"description_md"`
	Status          IssueStatus `json:"status"`
	Severity        *Severity   `json:"severity,omitempty"`
	Priority        Priority    `json:"priority"`
	Reporter        *User       `json:"reporter,omitempty"`
	Assignee        *User       `json:"assignee,omitempty"`
	Labels          []string    `json:"labels"`
	Components      []string    `json:"components"`
	MilestoneID     *uuid.UUID  `json:"milestone_id,omitempty"`
	Milestone       string      `json:"milestone,omitempty"` // title, resolved via join
	ReleaseID       *uuid.UUID  `json:"release_id,omitempty"`
	Release         string      `json:"release,omitempty"` // version, resolved via join
	VersionAffected string      `json:"version_affected"`
	VersionFixed    string      `json:"version_fixed"`
	GitCommitSHA    string      `json:"git_commit_sha"`
	PullRequestURL  string      `json:"pull_request_url"`
	ReproStepsMD    string      `json:"repro_steps_md"`
	ExpectedMD      string      `json:"expected_md"`
	ActualMD        string      `json:"actual_md"`
	EnvironmentMD   string      `json:"environment_md"`
	Source          IssueSource `json:"source"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// Key builds the human-readable issue key from a project key and number.
func IssueKey(projectKey string, number int32) string {
	return projectKey + "-" + itoa(number)
}

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
