package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the reported MCP server version.
const Version = "0.1.0"

const instructions = `Omni-BugTracker MCP server — read and write issues, comments, projects,
boards, webhooks and automation in the tracker, subject to your API token's RBAC.

Conventions:
- Issues are addressed by KEY like "BUG-421" (project key + number), not UUID.
- Projects are addressed by their uppercase KEY like "BUG".
- Assignees can be given by email (assignee_email) — resolved to a user id for you —
  or by raw assignee_id (uuid). Use list_users / whoami to discover ids and emails.
- list_issues accepts a GitHub-style filter string: space-separated terms such as
  "is:open assignee:@me severity:critical type:bug label:regression" plus free text
  for full-text; "@me" means the token's own user.
- Status changes go through transition_issue and are workflow-validated; a rejected
  transition returns an error naming the illegal from → to.
- Tool results are the API's JSON responses; errors include the HTTP status and detail.`

// Server wires the MCP protocol server to the REST client and owns tool registration.
type Server struct {
	srv *mcp.Server
	c   *Client
}

// NewServer constructs the MCP server and registers every tool group.
func NewServer(cfg Config) *Server {
	s := &Server{
		srv: mcp.NewServer(&mcp.Implementation{
			Name:    "omni-bugtracker",
			Title:   "Omni-BugTracker",
			Version: Version,
		}, &mcp.ServerOptions{Instructions: instructions}),
		c: NewClient(cfg),
	}
	s.registerMeta()
	s.registerIssues()
	s.registerCollab()
	s.registerItems()
	s.registerBoards()
	s.registerOps()
	s.registerAttachments()
	return s
}

// Run serves the MCP protocol over stdio until the context is cancelled or the
// client disconnects (closing stdin).
func (s *Server) Run(ctx context.Context) error {
	return s.srv.Run(ctx, &mcp.StdioTransport{})
}

// MCPServer exposes the underlying SDK server (used by tests that connect in-memory).
func (s *Server) MCPServer() *mcp.Server { return s.srv }

// ── result helpers ──

// jsonResult renders an API JSON response as pretty text tool output. The typed
// output is left as any/nil so no output schema is advertised.
func jsonResult(raw json.RawMessage) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: prettyJSON(raw)}},
	}, nil, nil
}

// textResult returns a plain-text tool result (for 204-style no-content actions).
func textResult(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// result adapts a (rawJSON, err) client call into a tool result. Errors are returned
// to the SDK, which marks the result as an error and includes the message as text so
// the model can see and self-correct. Empty (204) bodies become a success note.
func result(raw json.RawMessage, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 {
		return textResult("OK (no content)")
	}
	return jsonResult(raw)
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "(no content)"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// seg escapes a value for safe use as a single URL path segment.
func seg(s string) string { return url.PathEscape(s) }
