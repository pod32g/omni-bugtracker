package mcpserver

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxInlineAttachment bounds how many bytes of a textual attachment we inline into
// a tool result, to avoid flooding the model's context.
const maxInlineAttachment = 64 * 1024

func (s *Server) registerAttachments() {
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_attachments",
		Title:       "List attachments",
		Description: "List an issue's attachments (filename, content type, size, checksum, uploader).",
	}, s.listAttachments)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "get_attachment",
		Title:       "Get attachment",
		Description: "Fetch an attachment by UUID. Textual attachments (text/*, json, csv, yaml, markdown) are returned inline (up to 64KB); binary attachments return a summary only — download those via the web UI or REST API. Uploading attachments is not supported over MCP.",
	}, s.getAttachment)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_attachment",
		Title:       "Delete attachment",
		Description: "Delete an attachment by UUID. Allowed for the uploader or project managers.",
	}, s.deleteAttachment)
}

func (s *Server) listAttachments(ctx context.Context, _ *mcp.CallToolRequest, a issueKeyArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/issues/"+seg(a.Key)+"/attachments", nil))
}

func (s *Server) getAttachment(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	data, header, err := s.c.getRaw(ctx, "/attachments/"+seg(a.ID))
	if err != nil {
		return nil, nil, err
	}
	ct := header.Get("Content-Type")
	filename := dispositionFilename(header.Get("Content-Disposition"))

	if isTextual(ct) {
		text := string(data)
		note := ""
		if len(data) > maxInlineAttachment {
			text = string(data[:maxInlineAttachment])
			note = fmt.Sprintf("\n\n… [truncated: %d of %d bytes shown]", maxInlineAttachment, len(data))
		}
		header := fmt.Sprintf("attachment %s (%s, %d bytes):\n\n", nameOr(filename, a.ID), ct, len(data))
		return textResult(header + text + note)
	}
	return textResult(fmt.Sprintf(
		"attachment %s is binary (%s, %d bytes) — download it via the web UI or GET /attachments/%s; MCP inlines text only.",
		nameOr(filename, a.ID), ct, len(data), a.ID))
}

func (s *Server) deleteAttachment(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/attachments/"+seg(a.ID)))
}

// isTextual reports whether a content type is safe to inline as text.
func isTextual(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/csv",
		"application/x-yaml", "application/yaml", "application/markdown":
		return true
	}
	return strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml")
}

func dispositionFilename(cd string) string {
	if cd == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(cd); err == nil {
		return params["filename"]
	}
	return ""
}

func nameOr(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}
