package domain

import "time"

// Dashboard is the aggregated project-health overview.
type Dashboard struct {
	// ProjectKey is the scope these figures were computed over; empty means every
	// project. Echoed back so the UI can label the numbers honestly.
	ProjectKey         string         `json:"project_key,omitempty"`
	OpenIssues         int            `json:"open_issues"`
	CriticalIssues     int            `json:"critical_issues"`
	AvgResolutionHours float64        `json:"avg_resolution_hours"`
	MTTRHours          float64        `json:"mttr_hours"`
	RegressionRate     float64        `json:"regression_rate"`
	IssuesByStatus     map[string]int `json:"issues_by_status"`
	IssuesByComponent  map[string]int `json:"issues_by_component"`
	TeamWorkload       map[string]int `json:"team_workload"`
	RecentActivity     []Activity     `json:"recent_activity"`
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
