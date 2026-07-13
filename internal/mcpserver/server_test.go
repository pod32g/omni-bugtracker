package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/omni/bugtracker/internal/mcpserver"
)

// connectInProcess wires an MCP client to a freshly-built server over an in-memory
// transport (no subprocess, no network). Tool handlers still call the configured
// API base URL when invoked, so read/write tests need a live API; tools/list does not.
func connectInProcess(t *testing.T, cfg mcpserver.Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := mcpserver.NewServer(cfg)
	clientT, serverT := mcp.NewInMemoryTransports()

	if _, err := srv.MCPServer().Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestToolCatalog verifies every tool registers (schema inference does not panic)
// and the expected comprehensive catalog is advertised. It needs no live API.
func TestToolCatalog(t *testing.T) {
	cs := connectInProcess(t, mcpserver.Config{BaseURL: "http://127.0.0.1:0/api/v1", Token: "obt_dummy", Timeout: time.Second})

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}

	// A representative slice across every group; if any is missing, registration broke.
	want := []string{
		"whoami", "list_users", "get_dashboard", "list_projects", "get_project",
		"create_project", "update_project", "archive_project",
		"search_issues", "list_issues", "get_issue", "create_issue", "update_issue",
		"transition_issue", "move_issue", "delete_issue", "bulk_update_issues",
		"list_comments", "add_comment", "update_comment", "delete_comment",
		"list_relations", "add_relation", "delete_relation",
		"list_watchers", "watch_issue", "unwatch_issue",
		"get_issue_activity", "list_issue_commits",
		"list_labels", "list_components", "create_component", "update_component", "delete_component",
		"list_milestones", "create_milestone", "update_milestone", "delete_milestone",
		"list_releases", "create_release", "update_release", "delete_release",
		"list_project_members", "set_project_member", "remove_project_member",
		"get_project_board", "update_board", "create_board_column", "update_board_column", "delete_board_column",
		"list_saved_searches", "save_saved_search", "delete_saved_search",
		"list_webhooks", "create_webhook", "update_webhook", "delete_webhook",
		"list_webhook_deliveries", "redeliver_webhook",
		"list_automation_rules", "create_automation_rule", "update_automation_rule",
		"delete_automation_rule", "list_automation_runs",
		"list_attachments", "get_attachment", "delete_attachment",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %q missing from catalog", name)
		}
	}
	if len(res.Tools) < len(want) {
		t.Errorf("catalog has %d tools, expected at least %d", len(res.Tools), len(want))
	}
	t.Logf("catalog advertises %d tools", len(res.Tools))
}

// callText invokes a tool and returns (text, isError). It fails the test on a
// protocol-level error (tool not found, transport dead).
func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

// TestStdioRoundTrip spawns the built binary over stdio (validating main.go + the
// stdio transport) and drives a real create → comment → transition → get flow, plus
// an illegal-transition error. It runs only when OMNI_BT_API_TOKEN is set (a live
// API is required); see the local test stack in the project docs.
func TestStdioRoundTrip(t *testing.T) {
	token := os.Getenv(mcpserver.EnvToken)
	if token == "" {
		t.Skipf("set %s (and %s) to run the live round-trip against a running API", mcpserver.EnvToken, mcpserver.EnvBaseURL)
	}
	baseURL := os.Getenv(mcpserver.EnvBaseURL)
	if baseURL == "" {
		baseURL = mcpserver.DefaultBaseURL
	}
	project := envOr("OMNI_BT_TEST_PROJECT", "MCPT")

	// Build the binary so we exercise the real stdio entrypoint.
	bin := filepath.Join(t.TempDir(), "omni-bt-mcp")
	build := exec.Command("go", "build", "-o", bin, "github.com/omni/bugtracker/cmd/mcp")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	ctx := context.Background()
	cmd := exec.Command(bin, "--base-url", baseURL, "--token", token)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to spawned server: %v", err)
	}
	defer cs.Close() //nolint:errcheck

	// whoami — proves auth works over the spawned stdio server.
	if text, isErr := callText(t, cs, "whoami", map[string]any{}); isErr {
		t.Fatalf("whoami failed: %s", text)
	} else if !strings.Contains(text, "role") {
		t.Fatalf("whoami missing role: %s", text)
	}

	// Ensure a scratch project exists (409 conflict is fine — it already exists).
	callText(t, cs, "create_project", map[string]any{"key": project, "name": "MCP smoke tests"})

	// create_issue
	title := fmt.Sprintf("mcp smoke %d", time.Now().UnixNano())
	created, isErr := callText(t, cs, "create_issue", map[string]any{
		"project_key": project, "title": title, "type": "task",
	})
	if isErr {
		t.Fatalf("create_issue failed: %s", created)
	}
	var issue struct {
		Key    string `json:"key"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(created), &issue); err != nil || issue.Key == "" {
		t.Fatalf("could not parse created issue key from: %s", created)
	}
	t.Logf("created %s", issue.Key)

	// add_comment
	if text, isErr := callText(t, cs, "add_comment", map[string]any{"key": issue.Key, "body_md": "hello from the MCP smoke test"}); isErr {
		t.Fatalf("add_comment failed: %s", text)
	}

	// transition open → in_progress (legal)
	if text, isErr := callText(t, cs, "transition_issue", map[string]any{"key": issue.Key, "to": "in_progress"}); isErr {
		t.Fatalf("legal transition failed: %s", text)
	}

	// get_issue reflects the new status
	got, isErr := callText(t, cs, "get_issue", map[string]any{"key": issue.Key})
	if isErr {
		t.Fatalf("get_issue failed: %s", got)
	}
	if !strings.Contains(got, `"status": "in_progress"`) {
		t.Fatalf("expected status in_progress, got: %s", got)
	}

	// list_issues with the filter grammar finds our in-progress issue.
	if text, isErr := callText(t, cs, "list_issues", map[string]any{"project_key": project, "filter": "is:in_progress"}); isErr {
		t.Fatalf("list_issues failed: %s", text)
	} else if !strings.Contains(text, issue.Key) {
		t.Fatalf("list_issues(is:in_progress) did not include %s: %s", issue.Key, text)
	}

	// search_issues (full-text) finds it by a distinctive word from the title.
	distinctive := strings.Fields(title)[len(strings.Fields(title))-1] // the timestamp
	if text, isErr := callText(t, cs, "search_issues", map[string]any{"query": distinctive}); isErr {
		t.Fatalf("search_issues failed: %s", text)
	} else if !strings.Contains(text, issue.Key) {
		t.Fatalf("search_issues(%q) did not include %s: %s", distinctive, issue.Key, text)
	}

	// get_project_board lazily creates and returns the board.
	if text, isErr := callText(t, cs, "get_project_board", map[string]any{"key": project}); isErr {
		t.Fatalf("get_project_board failed: %s", text)
	}

	// assignee resolution: create an issue assigned by email (resolved via /users).
	me, _ := callText(t, cs, "whoami", map[string]any{})
	var self struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	_ = json.Unmarshal([]byte(me), &self)
	if self.Email != "" {
		assigned, isErr := callText(t, cs, "create_issue", map[string]any{
			"project_key": project, "title": "mcp assignee " + distinctive, "assignee_email": self.Email,
		})
		if isErr {
			t.Fatalf("create_issue with assignee_email failed: %s", assigned)
		}
		var a struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(assigned), &a); err != nil || a.Key == "" {
			t.Fatalf("could not parse assignee-test issue key: %s", assigned)
		}
		// The create response omits the assignee join; get_issue resolves it.
		full, isErr := callText(t, cs, "get_issue", map[string]any{"key": a.Key})
		if isErr {
			t.Fatalf("get_issue(%s) failed: %s", a.Key, full)
		}
		if !strings.Contains(full, self.Email) {
			t.Fatalf("assignee_email %q did not resolve onto %s: %s", self.Email, a.Key, full)
		}
	}

	// illegal transition in_progress → closed must surface a 409 error to the model.
	text, isErr := callText(t, cs, "transition_issue", map[string]any{"key": issue.Key, "to": "closed"})
	if !isErr {
		t.Fatalf("expected illegal transition to error, got success: %s", text)
	}
	if !strings.Contains(text, "invalid transition") && !strings.Contains(strings.ToLower(text), "in_progress") {
		t.Fatalf("error should explain the illegal transition, got: %s", text)
	}
	t.Logf("illegal transition correctly rejected: %s", strings.TrimSpace(text))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
