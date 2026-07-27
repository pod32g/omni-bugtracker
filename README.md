# Omni-BugTracker

A developer-first, self-hosted issue & bug tracker for the Omni ecosystem.
Git-native, API-first, automatable — not a Jira clone.

> **Status:** actively developed; runs as a self-hosted deployment. Architecture is in
> [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Generated code (sqlc, OpenAPI types, TS client) is kept out
> of git — run `make generate`, then `go mod tidy`.

## Stack

- **Backend:** Go 1.23, chi, pgx + sqlc, River (Postgres job queue), koanf, slog, Prometheus, OpenAPI 3.1.
- **Frontend:** React 18, TypeScript, Vite, TailwindCSS, TanStack Query, generated API client. Light + dark themes.
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
cmd/            server · worker · migrate · obt (CLI) entrypoints
internal/       config · platform · httpapi · domain · service · repo · events · worker
                · integrations · auth · search
db/             goose migrations + sqlc query files
web/            React + Vite + Tailwind SPA
deploy/         docker-compose · Dockerfiles · Helm chart
docs/           ARCHITECTURE.md
```

## `obt` — the terminal client

```
make obt                                # builds ./bin/obt
obt ls "is:open assignee:@me"           # any filter the UI understands
obt show BUG-42                         # or just `obt show` on a bug-42-… branch
obt new -t bug -T "Sign-in 500s"        # $EDITOR for the body
obt mv in_progress                      # key inferred from the branch
obt comment "fixed by the retry change"
obt spend 90m -m "chasing the retry loop"
obt --json ls "is:open sla:breached" | jq -r '.items[].key'
```

The issue key defaults to the one named by the current branch (`bug-42-fix-paging` →
`BUG-42`), and the project to `-p`, then the config, then the git remote. Every command
takes `--json`. Exit codes are distinct so CI can gate on them: `2` usage, `4` not found,
`5` forbidden, `6` rejected, `7` server or transport.

Config lives at `~/.config/obt/config.toml` (or `$OBT_CONFIG`); `$OBT_SERVER` and
`$OBT_TOKEN` override it, which is how it should be used in CI.

```toml
default = "prod"

[hosts.prod]
server  = "http://tracker.example:8092"
token   = "obt_…"
project = "BUG"
```

## API docs

Interactive **Swagger UI** is served by the API at [`/docs`](http://localhost:8080/docs)
(the raw OpenAPI 3.1 spec is at `/openapi.yaml`). Click **Authorize**, paste a personal
`obt_` token, and "Try it out" issues live calls against `/api/v1`. The spec is embedded
in the server binary, so it's always in sync with the deployed build.

## Design choices

Single-tenant · River for durable jobs (Postgres-only; Redis is cache/rate-limit only) ·
one unified `issues` table · Omni-Identity is the only IdP · search is native Postgres FTS
(generated `tsvector` columns; there is no external index to keep in step).
Rationale in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
