package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerIssues() {
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "search_issues",
		Title:       "Search issues",
		Description: "Full-text search across issue titles/descriptions and comments (Postgres FTS). Returns ranked hits with issue keys.",
	}, s.searchIssues)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_issues",
		Title:       "List issues",
		Description: "List issues in a project with an optional GitHub-style filter. Filter terms: is:/status:<status> assignee:@me|<uuid> severity:<sev> type:<type> label:<name> component:<name> milestone:<uuid> release:<uuid>, plus bare words for full-text. Statuses: open, in_progress, blocked, ready_for_review, resolved, closed, reopened.",
	}, s.listIssues)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_issue",
		Title:       "Get issue",
		Description: "Get one issue by key (e.g. BUG-421) with full detail: fields, assignee, labels, components, open blockers, milestone/release, timestamps.",
	}, s.getIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_issue",
		Title:       "Create issue",
		Description: "Create an issue in a project. Only title is required. type defaults to bug, priority to p2. Assign via assignee_email (resolved for you) or assignee_id. Requires issue:create on the project.",
	}, s.createIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_issue",
		Title:       "Update issue",
		Description: "Patch an issue's fields (only provided fields change). Does NOT change status — use transition_issue for that. Requires issue:update.",
	}, s.updateIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "transition_issue",
		Title:       "Transition issue status",
		Description: "Change an issue's status through the validated workflow. Allowed targets depend on the current status; an illegal transition returns an error naming from → to. Requires issue:transition.",
	}, s.transitionIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "move_issue",
		Title:       "Move issue to another project",
		Description: "Re-home an issue into another project; it is reassigned a new number/key there. Requires issue:update on the source and issue:create on the target.",
	}, s.moveIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_issue",
		Title:       "Delete issue",
		Description: "Delete an issue by key. Requires issue:delete.",
	}, s.deleteIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "archive_issue",
		Title:       "Archive issue",
		Description: "Archive an issue by key — hidden from default lists and search but recoverable and still reachable by key. Its status is unchanged. Use list_issues with filter 'is:archived' to see archived issues. Requires issue:update.",
	}, s.archiveIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "unarchive_issue",
		Title:       "Unarchive issue",
		Description: "Restore a previously archived issue back into default lists and search. Requires issue:update.",
	}, s.unarchiveIssue)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "bulk_update_issues",
		Title:       "Bulk update issues",
		Description: "Apply a patch, a status transition, and/or a project move to up to 100 issues addressed by UUID (the id field from get_issue/list_issues, NOT the key). Each issue is processed independently; the result reports how many were updated and which failed.",
	}, s.bulkUpdateIssues)
}

type searchArgs struct {
	Query string `json:"query" jsonschema:"full-text query, at least 2 characters"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results (default 20, max 50)"`
}

func (s *Server) searchIssues(ctx context.Context, _ *mcp.CallToolRequest, a searchArgs) (*mcp.CallToolResult, any, error) {
	q := query()
	q.Set("q", a.Query)
	setInt(q, "limit", a.Limit)
	return result(s.c.get(ctx, "/search", q))
}

type listIssuesArgs struct {
	ProjectKey string `json:"project_key" jsonschema:"project key to list issues from, e.g. BUG"`
	Filter     string `json:"filter,omitempty" jsonschema:"GitHub-style filter, e.g. 'is:open assignee:@me severity:critical'; @me = the token's own user"`
	Sort       string `json:"sort,omitempty" jsonschema:"sort order, e.g. created, updated, priority"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max results per page (default 50, max 200)"`
	Offset     int    `json:"offset,omitempty" jsonschema:"skip this many results — page through a project with more issues than the limit, using the response's total"`
}

func (s *Server) listIssues(ctx context.Context, _ *mcp.CallToolRequest, a listIssuesArgs) (*mcp.CallToolResult, any, error) {
	q := query()
	setStr(q, "filter", a.Filter)
	setStr(q, "sort", a.Sort)
	setInt(q, "limit", a.Limit)
	setInt(q, "offset", a.Offset)
	return result(s.c.get(ctx, "/projects/"+seg(a.ProjectKey)+"/issues", q))
}

type issueKeyArgs struct {
	Key string `json:"key" jsonschema:"issue key, e.g. BUG-421"`
}

func (s *Server) getIssue(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key), nil))
}

type createIssueArgs struct {
	ProjectKey      string   `json:"project_key" jsonschema:"project key to create the issue in, e.g. BUG"`
	Title           string   `json:"title" jsonschema:"short summary (required)"`
	Type            string   `json:"type,omitempty" jsonschema:"bug, task, feature, or improvement (default bug)"`
	DescriptionMD   string   `json:"description_md,omitempty" jsonschema:"markdown body"`
	Severity        string   `json:"severity,omitempty" jsonschema:"critical, high, medium, or low"`
	Priority        string   `json:"priority,omitempty" jsonschema:"p0, p1, p2, or p3 (default p2)"`
	AssigneeID      string   `json:"assignee_id,omitempty" jsonschema:"assignee user uuid (or use assignee_email)"`
	AssigneeEmail   string   `json:"assignee_email,omitempty" jsonschema:"assignee email, resolved to a user id"`
	Labels          []string `json:"labels,omitempty" jsonschema:"label names (created on first use)"`
	Components      []string `json:"components,omitempty" jsonschema:"component names"`
	VersionAffected string   `json:"version_affected,omitempty" jsonschema:"version where the bug appears"`
	ReproStepsMD    string   `json:"repro_steps_md,omitempty" jsonschema:"bug: steps to reproduce (markdown)"`
	ExpectedMD      string   `json:"expected_md,omitempty" jsonschema:"bug: expected behavior"`
	ActualMD        string   `json:"actual_md,omitempty" jsonschema:"bug: actual behavior"`
	EnvironmentMD   string   `json:"environment_md,omitempty" jsonschema:"bug: environment details"`
}

func (s *Server) createIssue(ctx context.Context, _ *mcp.CallToolRequest, a createIssueArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"title": a.Title}
	putStr(body, "type", a.Type)
	putStr(body, "description_md", a.DescriptionMD)
	putStr(body, "severity", a.Severity)
	putStr(body, "priority", a.Priority)
	putStr(body, "version_affected", a.VersionAffected)
	putStr(body, "repro_steps_md", a.ReproStepsMD)
	putStr(body, "expected_md", a.ExpectedMD)
	putStr(body, "actual_md", a.ActualMD)
	putStr(body, "environment_md", a.EnvironmentMD)
	putList(body, "labels", a.Labels)
	putList(body, "components", a.Components)
	assignee, err := s.resolveAssignee(ctx, a.AssigneeID, a.AssigneeEmail)
	if err != nil {
		return nil, nil, err
	}
	putPtr(body, "assignee_id", assignee)
	return result(s.c.post(ctx, "/projects/"+seg(a.ProjectKey)+"/issues", body))
}

type updateIssueArgs struct {
	Key             string   `json:"key" jsonschema:"issue key to update, e.g. BUG-421"`
	Title           string   `json:"title,omitempty" jsonschema:"new title"`
	DescriptionMD   string   `json:"description_md,omitempty" jsonschema:"new markdown body"`
	Type            string   `json:"type,omitempty" jsonschema:"bug, task, feature, or improvement"`
	Severity        string   `json:"severity,omitempty" jsonschema:"critical, high, medium, or low"`
	Priority        string   `json:"priority,omitempty" jsonschema:"p0, p1, p2, or p3"`
	AssigneeID      string   `json:"assignee_id,omitempty" jsonschema:"assignee user uuid (or use assignee_email)"`
	AssigneeEmail   string   `json:"assignee_email,omitempty" jsonschema:"assignee email, resolved to a user id"`
	Labels          []string `json:"labels,omitempty" jsonschema:"replace labels with this set"`
	Components      []string `json:"components,omitempty" jsonschema:"replace components with this set"`
	MilestoneID     string   `json:"milestone_id,omitempty" jsonschema:"milestone uuid to target"`
	ReleaseID       string   `json:"release_id,omitempty" jsonschema:"release uuid to target"`
	VersionAffected string   `json:"version_affected,omitempty"`
	VersionFixed    string   `json:"version_fixed,omitempty"`
	ReproStepsMD    string   `json:"repro_steps_md,omitempty"`
	ExpectedMD      string   `json:"expected_md,omitempty"`
	ActualMD        string   `json:"actual_md,omitempty"`
	EnvironmentMD   string   `json:"environment_md,omitempty"`
}

func (s *Server) updateIssue(ctx context.Context, _ *mcp.CallToolRequest, a updateIssueArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putStr(body, "title", a.Title)
	putStr(body, "description_md", a.DescriptionMD)
	putStr(body, "type", a.Type)
	putStr(body, "severity", a.Severity)
	putStr(body, "priority", a.Priority)
	putStr(body, "milestone_id", a.MilestoneID)
	putStr(body, "release_id", a.ReleaseID)
	putStr(body, "version_affected", a.VersionAffected)
	putStr(body, "version_fixed", a.VersionFixed)
	putStr(body, "repro_steps_md", a.ReproStepsMD)
	putStr(body, "expected_md", a.ExpectedMD)
	putStr(body, "actual_md", a.ActualMD)
	putStr(body, "environment_md", a.EnvironmentMD)
	putList(body, "labels", a.Labels)
	putList(body, "components", a.Components)
	assignee, err := s.resolveAssignee(ctx, a.AssigneeID, a.AssigneeEmail)
	if err != nil {
		return nil, nil, err
	}
	putPtr(body, "assignee_id", assignee)
	return result(s.c.patch(ctx, "/issues/"+seg(a.Key), body))
}

type transitionArgs struct {
	Key string `json:"key" jsonschema:"issue key, e.g. BUG-421"`
	To  string `json:"to" jsonschema:"target status: open, in_progress, blocked, ready_for_review, resolved, closed, or reopened"`
}

func (s *Server) transitionIssue(ctx context.Context, _ *mcp.CallToolRequest, a transitionArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/issues/"+seg(a.Key)+"/transition", map[string]any{"to": a.To}))
}

type moveIssueArgs struct {
	Key              string `json:"key" jsonschema:"issue key to move, e.g. BUG-421"`
	TargetProjectKey string `json:"target_project_key" jsonschema:"destination project key, e.g. WEB"`
}

func (s *Server) moveIssue(ctx context.Context, _ *mcp.CallToolRequest, a moveIssueArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/issues/"+seg(a.Key)+"/move", map[string]any{"target_project_key": a.TargetProjectKey}))
}

func (s *Server) deleteIssue(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/issues/"+seg(a.Key)))
}

func (s *Server) archiveIssue(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/issues/"+seg(a.Key)+"/archive", nil))
}

func (s *Server) unarchiveIssue(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/issues/"+seg(a.Key)+"/unarchive", nil))
}

type bulkArgs struct {
	IDs              []string `json:"ids" jsonschema:"issue UUIDs (the id field, not the key), 1–100 of them"`
	Priority         string   `json:"priority,omitempty" jsonschema:"set priority: p0, p1, p2, or p3"`
	Severity         string   `json:"severity,omitempty" jsonschema:"set severity: critical, high, medium, or low"`
	AssigneeID       string   `json:"assignee_id,omitempty" jsonschema:"set assignee by uuid (or use assignee_email)"`
	AssigneeEmail    string   `json:"assignee_email,omitempty" jsonschema:"set assignee by email"`
	Labels           []string `json:"labels,omitempty" jsonschema:"replace labels"`
	Components       []string `json:"components,omitempty" jsonschema:"replace components"`
	MilestoneID      string   `json:"milestone_id,omitempty" jsonschema:"set milestone uuid"`
	ReleaseID        string   `json:"release_id,omitempty" jsonschema:"set release uuid"`
	Status           string   `json:"status,omitempty" jsonschema:"transition all issues to this status"`
	TargetProjectKey string   `json:"target_project_key,omitempty" jsonschema:"move all issues to this project key"`
	Archived         *bool    `json:"archived,omitempty" jsonschema:"true to archive all issues, false to unarchive"`
}

func (s *Server) bulkUpdateIssues(ctx context.Context, _ *mcp.CallToolRequest, a bulkArgs) (*mcp.CallToolResult, any, error) {
	patch := map[string]any{}
	putStr(patch, "priority", a.Priority)
	putStr(patch, "severity", a.Severity)
	putStr(patch, "milestone_id", a.MilestoneID)
	putStr(patch, "release_id", a.ReleaseID)
	putList(patch, "labels", a.Labels)
	putList(patch, "components", a.Components)
	assignee, err := s.resolveAssignee(ctx, a.AssigneeID, a.AssigneeEmail)
	if err != nil {
		return nil, nil, err
	}
	putPtr(patch, "assignee_id", assignee)

	body := map[string]any{"ids": a.IDs}
	if len(patch) > 0 {
		body["patch"] = patch
	}
	putStr(body, "status", a.Status)
	putStr(body, "target_project_key", a.TargetProjectKey)
	putPtr(body, "archived", a.Archived)
	return result(s.c.post(ctx, "/issues/bulk", body))
}
