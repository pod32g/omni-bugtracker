// Package mcpserver implements a Model Context Protocol (MCP) server that exposes
// the Omni-BugTracker REST API to AI assistants as typed tools. It is a thin,
// standalone HTTP *client* of the existing API (see internal/service/handlers.go):
// it holds no database connection and duplicates no business logic, so every call
// is subject to the same RBAC as any other API caller. Authentication is a personal
// `obt_`-prefixed API token — each user brings their own, preserving their role.
package mcpserver

import (
	"errors"
	"os"
	"strings"
	"time"
)

// Config is the minimal bootstrap the MCP server needs. Unlike internal/config,
// this is a plain client config (no DSN, no identity) sourced from flags/env.
type Config struct {
	// BaseURL is the API root including the version prefix, e.g.
	// http://localhost:8080/api/v1 (local dev) or http://chizu:8092/api/v1.
	BaseURL string
	// Token is a personal API token (obt_…), sent as an HTTP bearer.
	Token string
	// Timeout bounds each HTTP request to the API.
	Timeout time.Duration
}

const (
	// DefaultBaseURL points at a locally-running API (see the local test stack).
	DefaultBaseURL = "http://localhost:8080/api/v1"
	// DefaultTimeout is generous enough for search/list on a warm database.
	DefaultTimeout = 30 * time.Second

	// EnvBaseURL / EnvToken / EnvTimeout are the environment fallbacks, following
	// the repo's OMNI_BT_ prefix convention.
	EnvBaseURL = "OMNI_BT_API_URL"
	EnvToken   = "OMNI_BT_API_TOKEN"
	EnvTimeout = "OMNI_BT_API_TIMEOUT"
)

// ConfigFromEnv builds a Config from the environment, applying defaults. Callers
// (main) may then override individual fields from command-line flags.
func ConfigFromEnv() Config {
	c := Config{
		BaseURL: getenv(EnvBaseURL, DefaultBaseURL),
		Token:   os.Getenv(EnvToken),
		Timeout: DefaultTimeout,
	}
	if s := os.Getenv(EnvTimeout); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			c.Timeout = d
		}
	}
	return c
}

// Normalize trims a trailing slash off the base URL and applies a default timeout.
func (c *Config) Normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
}

// Validate fails fast on missing required values, with actionable guidance.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return errors.New("no API token: set " + EnvToken + " to an obt_… personal token (Settings → API tokens) or pass --token")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("no API base URL: set " + EnvBaseURL + " or pass --base-url (e.g. http://localhost:8080/api/v1)")
	}
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
