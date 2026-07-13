package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerOps() {
	// saved searches (personal)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_saved_searches",
		Title:       "List saved searches",
		Description: "List your personal saved searches (name + filter query).",
	}, s.listSavedSearches)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "save_saved_search",
		Title:       "Save a search",
		Description: "Create or update a personal saved search by name. query is a filter string (same grammar as list_issues).",
	}, s.saveSavedSearch)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_saved_search",
		Title:       "Delete a saved search",
		Description: "Delete one of your saved searches by UUID.",
	}, s.deleteSavedSearch)

	// webhooks (require webhook:edit)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_webhooks",
		Title:       "List webhooks",
		Description: "List outbound webhook subscriptions. Requires webhook:edit.",
	}, s.listWebhooks)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_webhook",
		Title:       "Create webhook",
		Description: "Create an outbound webhook. url must be http(s). events is the list of event types to deliver (empty = all). project_key optionally scopes it. Requires webhook:edit.",
	}, s.createWebhook)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_webhook",
		Title:       "Update webhook",
		Description: "Update a webhook (by UUID): url, secret, events, or is_active. Requires webhook:edit.",
	}, s.updateWebhook)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_webhook",
		Title:       "Delete webhook",
		Description: "Delete a webhook by UUID. Requires webhook:edit.",
	}, s.deleteWebhook)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_webhook_deliveries",
		Title:       "List webhook deliveries",
		Description: "List recent delivery attempts for a webhook (by webhook UUID), with status codes and outcomes.",
	}, s.listWebhookDeliveries)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "redeliver_webhook",
		Title:       "Redeliver webhook event",
		Description: "Re-enqueue a past delivery (by webhook UUID + delivery UUID) with its original payload. Requires webhook:edit.",
	}, s.redeliverWebhook)

	// automation (require automation:edit)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_automation_rules",
		Title:       "List automation rules",
		Description: "List automation rules (trigger + actions). Requires automation:edit.",
	}, s.listAutomationRules)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "create_automation_rule",
		Title:       "Create automation rule",
		Description: "Create an automation rule. trigger.event names the event to match ('*' = any) with optional conditions; actions is a non-empty list of {kind, value} where kind is set_priority, set_severity, set_assignee, add_label, set_status, or add_comment. Requires automation:edit.",
	}, s.createAutomationRule)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "update_automation_rule",
		Title:       "Update automation rule",
		Description: "Update an automation rule (by UUID): name, priority, is_active, trigger, and/or actions. Requires automation:edit.",
	}, s.updateAutomationRule)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "delete_automation_rule",
		Title:       "Delete automation rule",
		Description: "Delete an automation rule by UUID. Requires automation:edit.",
	}, s.deleteAutomationRule)
	mcp.AddTool(s.srv, &mcp.Tool{
		Name:        "list_automation_runs",
		Title:       "List automation runs",
		Description: "List recent automation rule executions (the audit trail). Requires automation:edit.",
	}, s.listAutomationRuns)
}

// ── saved searches ──

func (s *Server) listSavedSearches(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/me/saved-searches", nil))
}

type saveSearchArgs struct {
	Name  string `json:"name" jsonschema:"a name for the saved search"`
	Query string `json:"query" jsonschema:"the filter query to save (same grammar as list_issues)"`
}

func (s *Server) saveSavedSearch(ctx context.Context, _ *mcp.CallToolRequest, a saveSearchArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/me/saved-searches", map[string]any{"name": a.Name, "query": a.Query}))
}

func (s *Server) deleteSavedSearch(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/me/saved-searches/"+seg(a.ID)))
}

// ── webhooks ──

func (s *Server) listWebhooks(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/webhooks", nil))
}

type createWebhookArgs struct {
	URL        string   `json:"url" jsonschema:"delivery URL (http or https)"`
	Secret     string   `json:"secret,omitempty" jsonschema:"HMAC signing secret"`
	Events     []string `json:"events,omitempty" jsonschema:"event types to deliver (empty = all)"`
	ProjectKey string   `json:"project_key,omitempty" jsonschema:"scope deliveries to a project key"`
}

func (s *Server) createWebhook(ctx context.Context, _ *mcp.CallToolRequest, a createWebhookArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"url": a.URL}
	putStr(body, "secret", a.Secret)
	putList(body, "events", a.Events)
	putStr(body, "project_key", a.ProjectKey)
	return result(s.c.post(ctx, "/webhooks", body))
}

type updateWebhookArgs struct {
	ID       string    `json:"id" jsonschema:"webhook UUID"`
	URL      *string   `json:"url,omitempty" jsonschema:"new delivery URL (http or https)"`
	Secret   *string   `json:"secret,omitempty" jsonschema:"new signing secret"`
	Events   *[]string `json:"events,omitempty" jsonschema:"replace the event list"`
	IsActive *bool     `json:"is_active,omitempty" jsonschema:"enable or disable delivery"`
}

func (s *Server) updateWebhook(ctx context.Context, _ *mcp.CallToolRequest, a updateWebhookArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "url", a.URL)
	putPtr(body, "secret", a.Secret)
	putPtr(body, "events", a.Events)
	putPtr(body, "is_active", a.IsActive)
	return result(s.c.patch(ctx, "/webhooks/"+seg(a.ID), body))
}

func (s *Server) deleteWebhook(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/webhooks/"+seg(a.ID)))
}

func (s *Server) listWebhookDeliveries(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/webhooks/"+seg(a.ID)+"/deliveries", nil))
}

type redeliverArgs struct {
	WebhookID  string `json:"webhook_id" jsonschema:"webhook UUID"`
	DeliveryID string `json:"delivery_id" jsonschema:"delivery UUID to re-send"`
}

func (s *Server) redeliverWebhook(ctx context.Context, _ *mcp.CallToolRequest, a redeliverArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.post(ctx, "/webhooks/"+seg(a.WebhookID)+"/deliveries/"+seg(a.DeliveryID)+"/redeliver", nil))
}

// ── automation ──

type automationTrigger struct {
	Event      string         `json:"event" jsonschema:"event type to match, or * for any"`
	Conditions map[string]any `json:"conditions,omitempty" jsonschema:"optional field conditions the event must satisfy"`
}

type automationAction struct {
	Kind  string `json:"kind" jsonschema:"set_priority, set_severity, set_assignee, add_label, set_status, or add_comment"`
	Value string `json:"value" jsonschema:"the value for the action (e.g. p1, a label name, a status, or comment text)"`
}

func (s *Server) listAutomationRules(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/automation/rules", nil))
}

type createRuleArgs struct {
	Name       string             `json:"name" jsonschema:"rule name"`
	ProjectKey string             `json:"project_key,omitempty" jsonschema:"scope to a project key (empty = global)"`
	Priority   int                `json:"priority,omitempty" jsonschema:"evaluation order, lower runs first (default 100)"`
	Trigger    automationTrigger  `json:"trigger" jsonschema:"the event trigger"`
	Actions    []automationAction `json:"actions" jsonschema:"non-empty list of actions to apply"`
}

func (s *Server) createAutomationRule(ctx context.Context, _ *mcp.CallToolRequest, a createRuleArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"name": a.Name, "trigger": a.Trigger, "actions": a.Actions}
	putStr(body, "project_key", a.ProjectKey)
	if a.Priority > 0 {
		body["priority"] = a.Priority
	}
	return result(s.c.post(ctx, "/automation/rules", body))
}

type updateRuleArgs struct {
	ID       string              `json:"id" jsonschema:"rule UUID"`
	Name     *string             `json:"name,omitempty" jsonschema:"new name"`
	Priority *int                `json:"priority,omitempty" jsonschema:"new evaluation order"`
	IsActive *bool               `json:"is_active,omitempty" jsonschema:"enable or disable the rule"`
	Trigger  *automationTrigger  `json:"trigger,omitempty" jsonschema:"replace the trigger"`
	Actions  *[]automationAction `json:"actions,omitempty" jsonschema:"replace the actions"`
}

func (s *Server) updateAutomationRule(ctx context.Context, _ *mcp.CallToolRequest, a updateRuleArgs) (*mcp.CallToolResult, any, error) {
	body := map[string]any{}
	putPtr(body, "name", a.Name)
	putPtr(body, "priority", a.Priority)
	putPtr(body, "is_active", a.IsActive)
	putPtr(body, "trigger", a.Trigger)
	putPtr(body, "actions", a.Actions)
	return result(s.c.patch(ctx, "/automation/rules/"+seg(a.ID), body))
}

func (s *Server) deleteAutomationRule(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.delete(ctx, "/automation/rules/"+seg(a.ID)))
}

func (s *Server) listAutomationRuns(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	return result(s.c.get(ctx, "/automation/runs", nil))
}
