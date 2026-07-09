package domain

// Dashboard is the aggregated project-health overview.
type Dashboard struct {
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
