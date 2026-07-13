package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// noArgs is the input type for tools that take no parameters. An empty struct
// still infers to a JSON-schema object, as the SDK requires.
type noArgs struct{}

func (s *Server) registerMeta() {
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "whoami",
		Title:       "Who am I",
		Description: "Return the authenticated user (id, email, display name, role) for the current API token. Useful to confirm auth and to learn your own user id for assignee:@me-style operations.",
	}, s.whoami)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_users",
		Title:       "List users",
		Description: "List known users (id, email, display name, role). Use this to find the assignee_id or email for an assignment.",
	}, s.listUsersTool)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_dashboard",
		Title:       "Get dashboard overview",
		Description: "Return the dashboard overview: open/critical counts, average resolution time, MTTR, regression rate, issues-by-status, team workload and recent activity.",
	}, s.getDashboard)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_projects",
		Title:       "List projects",
		Description: "List all projects (key, name, description, archived flag). Project keys are the uppercase prefixes used in issue keys (e.g. BUG in BUG-421).",
	}, s.listProjects)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_project",
		Title:       "Get project",
		Description: "Get one project by key, including your effective role on it (my_role).",
	}, s.getProject)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_project",
		Title:       "Create project",
		Description: "Create a project. Requires project:manage. Key must be 2–10 uppercase letters/digits starting with a letter (e.g. BUG, WEB, API2).",
	}, s.createProject)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_project",
		Title:       "Update project",
		Description: "Update a project's name, description, archived flag, or default assignee. Only provided fields change. Requires project:manage.",
	}, s.updateProject)

	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "archive_project",
		Title:       "Archive project",
		Description: "Soft-archive a project (sets is_archived=true). Requires project:manage.",
	}, s.archiveProject)
}

func (s *Server) whoami(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/me", nil))
}

func (s *Server) listUsersTool(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/users", nil))
}

func (s *Server) getDashboard(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/dashboards/overview", nil))
}

func (s *Server) listProjects(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects", nil))
}

type projectKeyArgs struct {
	Key string `json:"key" jsonschema:"project key, e.g. BUG"`
}

func (s *Server) getProject(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key), nil))
}

type createProjectArgs struct {
	Key           string `json:"key" jsonschema:"project key: 2–10 uppercase letters/digits starting with a letter, e.g. BUG"`
	Name          string `json:"name" jsonschema:"human-readable project name"`
	DescriptionMD string `json:"description_md,omitempty" jsonschema:"optional markdown description"`
}

func (s *Server) createProject(ctx context.Context, _ *mcp.CallToolRequest, a createProjectArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/projects", map[string]any{
		"key":            a.Key,
		"name":           a.Name,
		"description_md": a.DescriptionMD,
	}))
}

type updateProjectArgs struct {
	Key               string  `json:"key" jsonschema:"project key to update, e.g. BUG"`
	Name              *string `json:"name,omitempty" jsonschema:"new name"`
	DescriptionMD     *string `json:"description_md,omitempty" jsonschema:"new markdown description"`
	IsArchived        *bool   `json:"is_archived,omitempty" jsonschema:"archive (true) or unarchive (false)"`
	DefaultAssigneeID *string `json:"default_assignee_id,omitempty" jsonschema:"uuid of the default assignee for new issues"`
}

func (s *Server) updateProject(ctx context.Context, _ *mcp.CallToolRequest, a updateProjectArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "name", a.Name)
	putPtr(body, "description_md", a.DescriptionMD)
	putPtr(body, "is_archived", a.IsArchived)
	putPtr(body, "default_assignee_id", a.DefaultAssigneeID)
	return result(s.c.patch(ctx, "/projects/"+seg(a.Key), body))
}

func (s *Server) archiveProject(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/projects/"+seg(a.Key)))
}
