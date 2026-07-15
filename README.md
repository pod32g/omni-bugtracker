# Omni-BugTracker

A developer-first, self-hosted issue & bug tracker for the Omni ecosystem.
Git-native, API-first, automatable — not a Jira clone.

> **Status:** scaffold. Architecture is in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
> Hand-written skeleton is complete; run `make generate` to produce the sqlc, OpenAPI,
> and TypeScript-client code (kept out of git), then `go mod tidy`.

## Stack

- **Backend:** Go 1.23, chi, pgx + sqlc, River (Postgres job queue), koanf, slog, Prometheus, OpenAPI 3.1.
- **Frontend:** React 18, TypeScript, Vite, TailwindCSS, TanStack Query, generated API client. Dark mode by default.
- **Infra:** PostgreSQL 16, Redis 7, Docker.

## Quickstart (self-hosted)

```bash
cp .env.example .env          # edit for your Omni-Identity + service URLs
make up                       # postgres + redis + api + worker + web via docker compose
```

## Local development

```bash
make tools        # install sqlc / oapi-codegen / goose / river CLIs + npm deps
make generate     # regenerate SQL, HTTP, and TS-client code
docker compose -f deploy/docker-compose.yml up postgres redis -d
make migrate      # goose schema + River queue tables
make dev          # API on :8080
make worker       # background workers (another terminal)
make web          # frontend dev server (another terminal)
```

## Layout

```
api/            OpenAPI 3.1 spec + codegen config (source of truth for the HTTP contract)
cmd/            server · worker · migrate entrypoints
internal/       config · platform · httpapi · domain · service · repo · events · worker
                · integrations · auth · search
db/             goose migrations + sqlc query files
web/            React + Vite + Tailwind SPA
deploy/         docker-compose · Dockerfiles · Helm chart
docs/           ARCHITECTURE.md
```

## MCP server (AI clients)

`cmd/mcp` exposes the tracker to AI assistants (Claude Code, Claude Desktop) over the
Model Context Protocol as 68 typed tools — issues, comments, boards, webhooks,
automation, and more — all subject to your API token's RBAC. Build with `make mcp` and
point it at your API with a personal `obt_` token. See [`docs/MCP.md`](docs/MCP.md).

## Design choices

Single-tenant · River for durable jobs (Postgres-only; Redis is cache/rate-limit only) ·
one unified `issues` table · Omni-Identity is the only IdP · Postgres FTS with Omni-Search projection.
Rationale in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
