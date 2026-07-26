package domain

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
