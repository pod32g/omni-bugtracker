package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is an authenticated HTTP client for the Omni-BugTracker REST API. It is
// deliberately thin: build a request, attach the bearer token, decode JSON, and
// translate RFC-9457 problem+json errors into a Go error the model can read.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// NewClient returns a Client for the given (already-normalized) config.
func NewClient(cfg Config) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		hc:      &http.Client{Timeout: cfg.Timeout},
	}
}

// apiError carries a parsed problem+json body (internal/httpapi/problem.go) so the
// failure surfaces to the AI with its title, human detail, and per-field errors —
// e.g. "403 forbidden: missing issue:create" or "409 invalid transition: open → resolved".
type apiError struct {
	Status int
	Title  string
	Detail string
	Fields map[string]string
}

func (e *apiError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %d", e.Status)
	if e.Title != "" {
		fmt.Fprintf(&b, " (%s)", e.Title)
	}
	if e.Detail != "" {
		fmt.Fprintf(&b, ": %s", e.Detail)
	}
	if len(e.Fields) > 0 {
		fmt.Fprintf(&b, " — %s", joinFields(e.Fields))
	}
	return b.String()
}

func joinFields(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, "; ")
}

// call performs an API request and returns the raw JSON body. body is JSON-encoded
// when non-nil. A 2xx with an empty body (e.g. 204 No Content) returns nil, nil.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s %s: %w", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, parseAPIError(resp.StatusCode, data)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	return json.RawMessage(data), nil
}

// get / post / patch / delete are convenience wrappers over call.
func (c *Client) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.call(ctx, http.MethodGet, path, query, nil)
}

func (c *Client) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return c.call(ctx, http.MethodPost, path, nil, body)
}

func (c *Client) patch(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return c.call(ctx, http.MethodPatch, path, nil, body)
}

func (c *Client) put(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return c.call(ctx, http.MethodPut, path, nil, body)
}

func (c *Client) delete(ctx context.Context, path string) (json.RawMessage, error) {
	return c.call(ctx, http.MethodDelete, path, nil, nil)
}

// getRaw performs a GET and returns the raw response body and headers without JSON
// decoding — used for the attachment download endpoint, which serves bytes.
func (c *Client) getRaw(ctx context.Context, path string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("call GET %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, nil, parseAPIError(resp.StatusCode, data)
	}
	return data, resp.Header, nil
}

func parseAPIError(status int, data []byte) error {
	e := &apiError{Status: status}
	var p struct {
		Title  string            `json:"title"`
		Detail string            `json:"detail"`
		Errors map[string]string `json:"errors"`
	}
	if json.Unmarshal(data, &p) == nil {
		e.Title, e.Detail, e.Fields = p.Title, p.Detail, p.Errors
	}
	// Fall back to the raw body when the response isn't problem+json.
	if e.Title == "" && e.Detail == "" && len(e.Fields) == 0 {
		e.Detail = strings.TrimSpace(string(data))
	}
	return e
}
