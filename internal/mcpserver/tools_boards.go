package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerBoards() {
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_project_board",
		Title:       "Get project board",
		Description: "Get (or lazily create) a project's configurable Kanban board: columns, their statuses, WIP limits, and swimlane setting.",
	}, s.getProjectBoard)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_board",
		Title:       "Update board",
		Description: "Update a board (by UUID): name and/or swimlane (none, assignee, priority). Requires project:manage.",
	}, s.updateBoard)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_board_column",
		Title:       "Create board column",
		Description: "Add a column to a board (by board UUID). statuses is a non-empty list of workflow statuses the column collects (open, in_progress, blocked, ready_for_review, resolved, closed, reopened). wip_limit is optional. Requires project:manage.",
	}, s.createBoardColumn)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_board_column",
		Title:       "Update board column",
		Description: "Update a board column (by column UUID): name, statuses, position, and wip_limit (use -1 to clear the limit). Requires project:manage.",
	}, s.updateBoardColumn)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_board_column",
		Title:       "Delete board column",
		Description: "Delete a board column by UUID. Requires project:manage.",
	}, s.deleteBoardColumn)
}

func (s *Server) getProjectBoard(ctx context.Context, _ *mcp.CallToolRequest, a projectKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/projects/"+seg(a.Key)+"/board", nil))
}

type updateBoardArgs struct {
	ID       string  `json:"id" jsonschema:"board UUID (from get_project_board)"`
	Name     *string `json:"name,omitempty" jsonschema:"new board name"`
	Swimlane *string `json:"swimlane,omitempty" jsonschema:"none, assignee, or priority"`
}

func (s *Server) updateBoard(ctx context.Context, _ *mcp.CallToolRequest, a updateBoardArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "name", a.Name)
	putPtr(body, "swimlane", a.Swimlane)
	return result(s.c.patch(ctx, "/boards/"+seg(a.ID), body))
}

type createColumnArgs struct {
	BoardID  string   `json:"board_id" jsonschema:"board UUID (from get_project_board)"`
	Name     string   `json:"name" jsonschema:"column name, e.g. In Progress"`
	Statuses []string `json:"statuses" jsonschema:"non-empty list of workflow statuses this column collects"`
	WipLimit *int     `json:"wip_limit,omitempty" jsonschema:"optional work-in-progress limit"`
}

func (s *Server) createBoardColumn(ctx context.Context, _ *mcp.CallToolRequest, a createColumnArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"name": a.Name, "statuses": a.Statuses}
	putPtr(body, "wip_limit", a.WipLimit)
	return result(s.c.post(ctx, "/boards/"+seg(a.BoardID)+"/columns", body))
}

type updateColumnArgs struct {
	ID       string    `json:"id" jsonschema:"board column UUID"`
	Name     *string   `json:"name,omitempty" jsonschema:"new column name"`
	Statuses *[]string `json:"statuses,omitempty" jsonschema:"new non-empty list of workflow statuses"`
	WipLimit *int      `json:"wip_limit,omitempty" jsonschema:"new WIP limit; -1 clears it"`
	Position *int      `json:"position,omitempty" jsonschema:"new position (0-based order)"`
}

func (s *Server) updateBoardColumn(ctx context.Context, _ *mcp.CallToolRequest, a updateColumnArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "name", a.Name)
	putPtr(body, "statuses", a.Statuses)
	putPtr(body, "wip_limit", a.WipLimit)
	putPtr(body, "position", a.Position)
	return result(s.c.patch(ctx, "/board-columns/"+seg(a.ID), body))
}

func (s *Server) deleteBoardColumn(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/board-columns/"+seg(a.ID)))
}
