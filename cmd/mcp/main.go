// Command mcp runs the Omni-BugTracker MCP (Model Context Protocol) server over
// stdio, so AI assistants (Claude Code, Claude Desktop, …) can work with the tracker
// through typed tools. It is a thin client of the REST API: point it at an API base
// URL and give it a personal `obt_` token; every action runs with that token's RBAC.
//
// Usage:
//
//	omni-bt-mcp [--base-url URL] [--token TOKEN] [--timeout DURATION]
//
// Configuration precedence: command-line flags override environment variables
// (OMNI_BT_API_URL, OMNI_BT_API_TOKEN, OMNI_BT_API_TIMEOUT), which override defaults.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/omni/bugtracker/internal/mcpserver"
)

func main() {
	cfg := mcpserver.ConfigFromEnv()

	flag.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "API base URL including /api/v1 (env "+mcpserver.EnvBaseURL+")")
	flag.StringVar(&cfg.Token, "token", cfg.Token, "personal API token, obt_… (env "+mcpserver.EnvToken+")")
	flag.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "per-request timeout (env "+mcpserver.EnvTimeout+")")
	flag.Parse()

	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "omni-bt-mcp:", err)
		os.Exit(2)
	}

	// Terminate cleanly on SIGINT/SIGTERM; the SDK also stops when stdin closes.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := mcpserver.NewServer(cfg)
	if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "omni-bt-mcp:", err)
		os.Exit(1)
	}
}
