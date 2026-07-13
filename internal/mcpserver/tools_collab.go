package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerCollab() {
	// comments
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_comments",
		Title:       "List comments",
		Description: "List comments on an issue (with author and timestamps).",
	}, s.listComments)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "add_comment",
		Title:       "Add comment",
		Description: "Add a markdown comment to an issue. Requires comment:create.",
	}, s.addComment)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_comment",
		Title:       "Edit comment",
		Description: "Edit a comment (by its UUID). Only the original author may edit; stamps edited_at.",
	}, s.updateComment)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_comment",
		Title:       "Delete comment",
		Description: "Soft-delete a comment (by its UUID). Allowed for the author or project managers.",
	}, s.deleteComment)

	// relations
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_relations",
		Title:       "List issue relations",
		Description: "List an issue's relations to other issues.",
	}, s.listRelations)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "add_relation",
		Title:       "Link issues",
		Description: "Create a relation from an issue to another. kind is one of: blocks, blocked_by, duplicates, relates, caused_by. Requires issue:update.",
	}, s.addRelation)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_relation",
		Title:       "Unlink issues",
		Description: "Delete a relation by its UUID.",
	}, s.deleteRelation)

	// watchers
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_watchers",
		Title:       "List watchers",
		Description: "List users watching an issue, plus whether you are watching it.",
	}, s.listWatchers)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "watch_issue",
		Title:       "Watch issue",
		Description: "Start watching an issue (as the token's user), to receive its notifications.",
	}, s.watchIssue)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "unwatch_issue",
		Title:       "Unwatch issue",
		Description: "Stop watching an issue.",
	}, s.unwatchIssue)

	// history
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_issue_activity",
		Title:       "Get issue activity",
		Description: "Return the humanized activity timeline of an issue (status changes, field edits, comments, links).",
	}, s.getIssueActivity)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_issue_commits",
		Title:       "List issue commits",
		Description: "List git commits/PRs linked to an issue via the git integration.",
	}, s.listIssueCommits)
}

func (s *Server) listComments(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key)+"/comments", nil))
}

type addCommentArgs struct {
	Key    string `json:"key" jsonschema:"issue key, e.g. BUG-421"`
	BodyMD string `json:"body_md" jsonschema:"comment text (markdown)"`
}

func (s *Server) addComment(ctx context.Context, _ *mcp.CallToolRequest, a addCommentArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/issues/"+seg(a.Key)+"/comments", map[string]any{"body_md": a.BodyMD}))
}

type editCommentArgs struct {
	ID     string `json:"id" jsonschema:"comment UUID"`
	BodyMD string `json:"body_md" jsonschema:"new comment text (markdown)"`
}

func (s *Server) updateComment(ctx context.Context, _ *mcp.CallToolRequest, a editCommentArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.patch(ctx, "/comments/"+seg(a.ID), map[string]any{"body_md": a.BodyMD}))
}

type idArgs struct {
	ID string `json:"id" jsonschema:"the entity's UUID"`
}

func (s *Server) deleteComment(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/comments/"+seg(a.ID)))
}

func (s *Server) listRelations(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key)+"/relations", nil))
}

type addRelationArgs struct {
	Key         string `json:"key" jsonschema:"source issue key, e.g. BUG-421"`
	Kind        string `json:"kind" jsonschema:"blocks, blocked_by, duplicates, relates, or caused_by"`
	TargetIssue string `json:"target_issue" jsonschema:"the other issue's key, e.g. BUG-88"`
}

func (s *Server) addRelation(ctx context.Context, _ *mcp.CallToolRequest, a addRelationArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/issues/"+seg(a.Key)+"/relations", map[string]any{
		"kind":      a.Kind,
		"issue_key": a.TargetIssue,
	}))
}

func (s *Server) deleteRelation(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/relations/"+seg(a.ID)))
}

func (s *Server) listWatchers(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key)+"/watchers", nil))
}

func (s *Server) watchIssue(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.put(ctx, "/issues/"+seg(a.Key)+"/watchers/me", nil))
}

func (s *Server) unwatchIssue(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/issues/"+seg(a.Key)+"/watchers/me"))
}

func (s *Server) getIssueActivity(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key)+"/activity", nil))
}

func (s *Server) listIssueCommits(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key)+"/commits", nil))
}
