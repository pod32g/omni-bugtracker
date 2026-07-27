package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/omni/bugtracker/internal/domain"
)

// Client is a thin typed wrapper over the REST API.
//
// It decodes into internal/domain types rather than into a generated client's models:
// the handlers marshal those structs directly, so this cannot drift from what the
// server actually sends. The OpenAPI document the ticket proposed generating from is
// itself behind the API (it describes 49 paths and none of the ones added most
// recently), so generating from it would have pinned the CLI to a stale contract.
type Client struct {
	server string
	token  string
	http   *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		server: cfg.Server,
		token:  cfg.Token,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError carries the status and the problem+json detail, so a script can branch on
// the code and a human sees what the server actually said.
type APIError struct {
	Status int
	Title  string
	Detail string
	Fields map[string]string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%d)", e.Title, e.Status)
	if e.Detail != "" {
		fmt.Fprintf(&b, ": %s", e.Detail)
	}
	for field, msg := range e.Fields {
		fmt.Fprintf(&b, "\n  %s: %s", field, msg)
	}
	return b.String()
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c.server == "" {
		return fmt.Errorf("no server configured — set it in %s, or pass --server / $OBT_SERVER", configPath())
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+"/api/v1"+path, reader)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		return decodeProblem(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// decodeProblem turns an RFC 9457 body into an APIError, falling back to the raw body
// when the server returned something else (a proxy's HTML error page, most likely).
func decodeProblem(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var problem struct {
		Title  string            `json:"title"`
		Detail string            `json:"detail"`
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(raw, &problem); err != nil || problem.Title == "" {
		return &APIError{Status: resp.StatusCode, Title: resp.Status, Detail: strings.TrimSpace(string(raw))}
	}
	return &APIError{
		Status: resp.StatusCode, Title: problem.Title,
		Detail: problem.Detail, Fields: problem.Errors,
	}
}

// ── endpoints ──

func (c *Client) Whoami(ctx context.Context) (domain.User, error) {
	var u domain.User
	err := c.do(ctx, http.MethodGet, "/me", nil, &u)
	return u, err
}

type issueList struct {
	Items  []domain.Issue      `json:"items"`
	Total  int                 `json:"total"`
	Effort domain.EffortRollup `json:"effort"`
}

// ListIssues runs a filter. An empty project key uses the cross-project endpoint, so
// `obt ls` outside a known repo still works.
func (c *Client) ListIssues(ctx context.Context, project, filter, sort string, limit int) (issueList, error) {
	q := url.Values{}
	q.Set("filter", filter)
	if sort != "" {
		q.Set("sort", sort)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	path := "/issues?" + q.Encode()
	if project != "" {
		path = "/projects/" + url.PathEscape(project) + "/issues?" + q.Encode()
	}
	var out issueList
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (c *Client) GetIssue(ctx context.Context, key string) (domain.Issue, error) {
	var i domain.Issue
	err := c.do(ctx, http.MethodGet, "/issues/"+url.PathEscape(key), nil, &i)
	return i, err
}

func (c *Client) CreateIssue(ctx context.Context, project string, body map[string]any) (domain.Issue, error) {
	var i domain.Issue
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(project)+"/issues", body, &i)
	return i, err
}

func (c *Client) UpdateIssue(ctx context.Context, key string, body map[string]any) (domain.Issue, error) {
	var i domain.Issue
	err := c.do(ctx, http.MethodPatch, "/issues/"+url.PathEscape(key), body, &i)
	return i, err
}

func (c *Client) Transition(ctx context.Context, key, to string) (domain.Issue, error) {
	var i domain.Issue
	err := c.do(ctx, http.MethodPost, "/issues/"+url.PathEscape(key)+"/transition",
		map[string]any{"to": to}, &i)
	return i, err
}

func (c *Client) AddComment(ctx context.Context, key, body string) (domain.Comment, error) {
	var out domain.Comment
	err := c.do(ctx, http.MethodPost, "/issues/"+url.PathEscape(key)+"/comments",
		map[string]any{"body_md": body}, &out)
	return out, err
}

func (c *Client) ListComments(ctx context.Context, key string) ([]domain.Comment, error) {
	var out struct {
		Items []domain.Comment `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/issues/"+url.PathEscape(key)+"/comments?limit=100", nil, &out)
	return out.Items, err
}

func (c *Client) SetWatching(ctx context.Context, key string, watching bool) error {
	method := http.MethodPut
	if !watching {
		method = http.MethodDelete
	}
	return c.do(ctx, method, "/issues/"+url.PathEscape(key)+"/watchers/me", nil, nil)
}

func (c *Client) LogTime(ctx context.Context, key, duration, note string) (domain.TimeEntry, error) {
	var out domain.TimeEntry
	err := c.do(ctx, http.MethodPost, "/issues/"+url.PathEscape(key)+"/time",
		map[string]any{"duration": duration, "note": note}, &out)
	return out, err
}

func (c *Client) ListUsers(ctx context.Context) ([]domain.User, error) {
	var out struct {
		Items []domain.User `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/users", nil, &out)
	return out.Items, err
}

func (c *Client) ListProjects(ctx context.Context) ([]domain.Project, error) {
	var out struct {
		Items []domain.Project `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/projects", nil, &out)
	return out.Items, err
}
