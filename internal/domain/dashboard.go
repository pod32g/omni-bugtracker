package domain

import "time"

// Dashboard is the aggregated project-health overview.
type Dashboard struct {
	// ProjectKey is the scope these figures were computed over; empty means every
	// project. Echoed back so the UI can label the numbers honestly.
	ProjectKey         string  `json:"project_key,omitempty"`
	OpenIssues         int     `json:"open_issues"`
	CriticalIssues     int     `json:"critical_issues"`
	AvgResolutionHours float64 `json:"avg_resolution_hours"`
	MTTRHours          float64 `json:"mttr_hours"`
	RegressionRate     float64 `json:"regression_rate"`
	// Overdue / SLA counts. Open issues only — a breach that was already paid for by
	// shipping late is history, and a band that counted it would never go down.
	OverdueIssues     int            `json:"overdue_issues"`
	SLAAtRiskIssues   int            `json:"sla_at_risk_issues"`
	SLABreachedIssues int            `json:"sla_breached_issues"`
	IssuesByStatus    map[string]int `json:"issues_by_status"`
	IssuesByComponent map[string]int `json:"issues_by_component"`
	TeamWorkload      map[string]int `json:"team_workload"`
	// RemainingByAssignee is open *estimated* work per person, in minutes. Distinct
	// from TeamWorkload, which counts issues: four one-hour issues and one two-week
	// issue are the same workload by count and nothing alike by effort.
	RemainingByAssignee map[string]int `json:"remaining_by_assignee"`
	RecentActivity      []Activity     `json:"recent_activity"`
}

// Report is the trend view the dashboard cannot give: the dashboard is entirely
// point-in-time, so nothing there shows a direction.
type Report struct {
	Flow       []ReportPoint      `json:"flow"`
	Age        []ReportAge        `json:"age"`
	Throughput []ReportThroughput `json:"throughput"`
	// Percentiles in hours. P50 and P90 rather than a mean, because a mean hides the
	// tail and the tail is what people complain about.
	ResolveP50     float64 `json:"resolve_p50_hours"`
	ResolveP90     float64 `json:"resolve_p90_hours"`
	RespondP50     float64 `json:"respond_p50_hours"`
	RespondP90     float64 `json:"respond_p90_hours"`
	ResolvedCount  int     `json:"resolved_count"`
	RespondedCount int     `json:"responded_count"`
}

// ReportPoint is one week of created-vs-resolved.
type ReportPoint struct {
	Period   time.Time `json:"period"`
	Created  int       `json:"created"`
	Resolved int       `json:"resolved"`
}

// ReportAge is one (age bucket, severity) cell of the open backlog.
type ReportAge struct {
	Bucket   string `json:"bucket"`
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

// ReportThroughput is how much one person resolved over the range.
type ReportThroughput struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// OpsSnapshot is the operator's view: what the queue is doing and whether outbound
// deliveries are landing. Every figure already existed in Postgres or the process — the
// gap was that a self-hosted tool with no ops surface makes its operator guess.
type OpsSnapshot struct {
	Queues     []QueueState     `json:"queues"`
	Failures   []JobFailure     `json:"failures"`
	Deliveries []DeliveryHealth `json:"deliveries"`
	// QueueError is set when River's tables cannot be read, so the page can say why
	// instead of failing to load — the one page that must survive things being broken.
	QueueError string `json:"queue_error,omitempty"`
}

type QueueState struct {
	Queue      string `json:"queue"`
	State      string `json:"state"`
	Count      int    `json:"count"`
	MaxAttempt int    `json:"max_attempt"`
}

type JobFailure struct {
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	Error       string     `json:"error"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type DeliveryHealth struct {
	URL       string     `json:"url"`
	Succeeded int        `json:"succeeded"`
	Total     int        `json:"total"`
	LastAt    *time.Time `json:"last_at,omitempty"`
}
