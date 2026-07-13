package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerItems() {
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_labels",
		Title:       "List labels",
		Description: "List the labels defined in a project.",
	}, s.listLabels)

	// components
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_components",
		Title:       "List components",
		Description: "List a project's components (areas of ownership) with open-issue counts.",
	}, s.listComponents)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_component",
		Title:       "Create component",
		Description: "Create a component in a project. Requires project:manage.",
	}, s.createComponent)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_component",
		Title:       "Update component",
		Description: "Update a component (by UUID): name, description, or lead. Requires project:manage.",
	}, s.updateComponent)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_component",
		Title:       "Delete component",
		Description: "Delete a component by UUID. Requires project:manage.",
	}, s.deleteComponent)

	// milestones
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_milestones",
		Title:       "List milestones",
		Description: "List a project's milestones with open/closed issue counts.",
	}, s.listMilestones)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_milestone",
		Title:       "Create milestone",
		Description: "Create a milestone in a project. due_on is optional (YYYY-MM-DD). Requires project:manage.",
	}, s.createMilestone)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_milestone",
		Title:       "Update milestone",
		Description: "Update a milestone (by UUID): title, description, due_on (YYYY-MM-DD), or state (open|closed). Requires project:manage.",
	}, s.updateMilestone)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_milestone",
		Title:       "Delete milestone",
		Description: "Delete a milestone by UUID. Requires project:manage.",
	}, s.deleteMilestone)

	// releases
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_releases",
		Title:       "List releases",
		Description: "List a project's releases with open/done issue counts.",
	}, s.listReleases)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_release",
		Title:       "Create release",
		Description: "Create a release (version required, e.g. 2.1.0). Requires project:manage.",
	}, s.createRelease)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_release",
		Title:       "Update release",
		Description: "Update a release (by UUID): version, name, notes, git_tag, or state (draft|published). Requires project:manage.",
	}, s.updateRelease)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_release",
		Title:       "Delete release",
		Description: "Delete a release by UUID. Requires project:manage.",
	}, s.deleteRelease)

	// members
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_project_members",
		Title:       "List project members",
		Description: "List a project's members and their per-project roles.",
	}, s.listProjectMembers)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "set_project_member",
		Title:       "Add/update project member",
		Description: "Add a user to a project or change their project role. role is one of: owner, admin, maintainer, member, reporter, bot. Requires project:manage.",
	}, s.setProjectMember)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "remove_project_member",
		Title:       "Remove project member",
		Description: "Remove a user's membership from a project. Requires project:manage.",
	}, s.removeProjectMember)
}

func (s *Server) listLabels(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key)+"/labels", nil))
}

func (s *Server) listComponents(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key)+"/components", nil))
}

type createComponentArgs struct {
	ProjectKey    string `json:"project_key" jsonschema:"project key, e.g. BUG"`
	Name          string `json:"name" jsonschema:"component name, e.g. api"`
	DescriptionMD string `json:"description_md,omitempty" jsonschema:"optional markdown description"`
	LeadID        string `json:"lead_id,omitempty" jsonschema:"uuid of the component lead"`
}

func (s *Server) createComponent(ctx context.Context, _ *mcp.CallToolRequest, a createComponentArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"name": a.Name}
	putStr(body, "description_md", a.DescriptionMD)
	putStr(body, "lead_id", a.LeadID)
	return result(s.c.post(ctx, "/projects/"+seg(a.ProjectKey)+"/components", body))
}

type updateComponentArgs struct {
	ID            string  `json:"id" jsonschema:"component UUID"`
	Name          *string `json:"name,omitempty" jsonschema:"new name"`
	DescriptionMD *string `json:"description_md,omitempty" jsonschema:"new description"`
	LeadID        *string `json:"lead_id,omitempty" jsonschema:"new lead uuid"`
}

func (s *Server) updateComponent(ctx context.Context, _ *mcp.CallToolRequest, a updateComponentArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "name", a.Name)
	putPtr(body, "description_md", a.DescriptionMD)
	putPtr(body, "lead_id", a.LeadID)
	return result(s.c.patch(ctx, "/components/"+seg(a.ID), body))
}

func (s *Server) deleteComponent(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/components/"+seg(a.ID)))
}

func (s *Server) listMilestones(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key)+"/milestones", nil))
}

type createMilestoneArgs struct {
	ProjectKey    string `json:"project_key" jsonschema:"project key, e.g. BUG"`
	Title         string `json:"title" jsonschema:"milestone title"`
	DescriptionMD string `json:"description_md,omitempty" jsonschema:"optional markdown description"`
	DueOn         string `json:"due_on,omitempty" jsonschema:"optional due date, YYYY-MM-DD"`
}

func (s *Server) createMilestone(ctx context.Context, _ *mcp.CallToolRequest, a createMilestoneArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"title": a.Title}
	putStr(body, "description_md", a.DescriptionMD)
	putStr(body, "due_on", a.DueOn)
	return result(s.c.post(ctx, "/projects/"+seg(a.ProjectKey)+"/milestones", body))
}

type updateMilestoneArgs struct {
	ID            string  `json:"id" jsonschema:"milestone UUID"`
	Title         *string `json:"title,omitempty" jsonschema:"new title"`
	DescriptionMD *string `json:"description_md,omitempty" jsonschema:"new description"`
	DueOn         *string `json:"due_on,omitempty" jsonschema:"new due date YYYY-MM-DD, or empty string to clear"`
	State         *string `json:"state,omitempty" jsonschema:"open or closed"`
}

func (s *Server) updateMilestone(ctx context.Context, _ *mcp.CallToolRequest, a updateMilestoneArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "title", a.Title)
	putPtr(body, "description_md", a.DescriptionMD)
	putPtr(body, "due_on", a.DueOn)
	putPtr(body, "state", a.State)
	return result(s.c.patch(ctx, "/milestones/"+seg(a.ID), body))
}

func (s *Server) deleteMilestone(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/milestones/"+seg(a.ID)))
}

func (s *Server) listReleases(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key)+"/releases", nil))
}

type createReleaseArgs struct {
	ProjectKey string `json:"project_key" jsonschema:"project key, e.g. BUG"`
	Version    string `json:"version" jsonschema:"version string, e.g. 2.1.0"`
	Name       string `json:"name,omitempty" jsonschema:"optional release name"`
	NotesMD    string `json:"notes_md,omitempty" jsonschema:"optional release notes (markdown)"`
	GitTag     string `json:"git_tag,omitempty" jsonschema:"optional git tag"`
}

func (s *Server) createRelease(ctx context.Context, _ *mcp.CallToolRequest, a createReleaseArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"version": a.Version}
	putStr(body, "name", a.Name)
	putStr(body, "notes_md", a.NotesMD)
	putStr(body, "git_tag", a.GitTag)
	return result(s.c.post(ctx, "/projects/"+seg(a.ProjectKey)+"/releases", body))
}

type updateReleaseArgs struct {
	ID      string  `json:"id" jsonschema:"release UUID"`
	Version *string `json:"version,omitempty" jsonschema:"new version"`
	Name    *string `json:"name,omitempty" jsonschema:"new name"`
	NotesMD *string `json:"notes_md,omitempty" jsonschema:"new notes"`
	GitTag  *string `json:"git_tag,omitempty" jsonschema:"new git tag"`
	State   *string `json:"state,omitempty" jsonschema:"draft or published"`
}

func (s *Server) updateRelease(ctx context.Context, _ *mcp.CallToolRequest, a updateReleaseArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "version", a.Version)
	putPtr(body, "name", a.Name)
	putPtr(body, "notes_md", a.NotesMD)
	putPtr(body, "git_tag", a.GitTag)
	putPtr(body, "state", a.State)
	return result(s.c.patch(ctx, "/releases/"+seg(a.ID), body))
}

func (s *Server) deleteRelease(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/releases/"+seg(a.ID)))
}

func (s *Server) listProjectMembers(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key)+"/members", nil))
}

type setMemberArgs struct {
	ProjectKey string `json:"project_key" jsonschema:"project key, e.g. BUG"`
	UserID     string `json:"user_id" jsonschema:"the user's UUID"`
	Role       string `json:"role" jsonschema:"owner, admin, maintainer, member, reporter, or bot"`
}

func (s *Server) setProjectMember(ctx context.Context, _ *mcp.CallToolRequest, a setMemberArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.put(ctx, "/projects/"+seg(a.ProjectKey)+"/members/"+seg(a.UserID), map[string]any{"role": a.Role}))
}

type memberArgs struct {
	ProjectKey string `json:"project_key" jsonschema:"project key, e.g. BUG"`
	UserID     string `json:"user_id" jsonschema:"the user's UUID"`
}

func (s *Server) removeProjectMember(ctx context.Context, _ *mcp.CallToolRequest, a memberArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/projects/"+seg(a.ProjectKey)+"/members/"+seg(a.UserID)))
}
