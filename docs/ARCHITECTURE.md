# Omni-BugTracker — Architecture

Developer-first, self-hosted issue & bug tracker for the Omni ecosystem.
Design philosophy: **simplicity over completeness, API-first, everything automatable.**

## Locked decisions

| Area | Decision | Why |
|---|---|---|
| Topology | **Modular monolith** in Go (one binary, `server` + `worker` modes) | One bounded context; avoids distributed-systems overhead. |
| API | **OpenAPI 3.1 as the published contract**, hand-written chi router, drift-checked by a test | The API *is* the contract; UI can't exceed it. (The handlers are not generated — see below.) |
| Data access | **pgx v5**, hand-written SQL, no ORM | Predictable queries, no reflection magic. |
| Jobs / events | **River** (Postgres job queue) with transactional `InsertTx` | Enqueue in the same tx as the domain write = built-in transactional outbox. No separate outbox table, no Redis for jobs. |
| Redis | Cache + rate limiting only | Job durability lives in Postgres via River. |
| Tenancy | **Single-tenant** (one org per deployment) | Chosen for simplicity; no `tenant_id`/RLS. |
| Auth | **Omni-Identity only** (OIDC/OAuth2/LDAP terminate there); we validate JWTs + issue hashed API tokens | No local passwords. |
| Search | Postgres **FTS** (generated `tsvector` columns + GIN) | The index is maintained by the database, so search cannot fall behind writes and there is no projection to fail. |
| Issues | **One `issues` table** with a `type` discriminator + nullable bug fields + `jsonb` escape hatch | No table-per-type, no custom-field engine. |

## A note on code generation

`make generate` runs three generators — sqlc, oapi-codegen, and openapi-typescript —
and **nothing in the build imports any of their output**. `internal/repo/gen`,
`internal/httpapi/gen` and `web/src/api/gen` are all produced, all gitignored, and all
unreferenced: data access is hand-written pgx in `internal/repo/pg`, the router is
hand-written chi, and the web client is hand-written in `web/src/lib/api.ts`.

This is written down because the docs previously implied otherwise, and because it is a
decision somebody should make deliberately rather than inherit:

- **Adopt** — swap `internal/repo/pg` method bodies for the sqlc calls (`store.go` says
  how), mount the generated server, and bind the TS client. Buys compile-time
  guarantees that the contract test currently approximates.
- **Delete** — drop `sqlc.yaml`, `db/queries/`, `api/oapi-codegen.yaml`, the `gen:api`
  script and the `generate` target. Buys one obvious way to do things and a shorter
  setup for a new contributor.

Doing neither is the only option with no upside: the generators still have to be
installed and still produce artifacts, and the inputs (`db/queries/*.sql`) drift from
the queries actually executed with nothing to notice.

## Runtime shape

```
React SPA ──HTTPS(JWT|token)──► API (chi) ──► services ──► repo (pgx) ──► Postgres
                                    │                                            ▲
                                    └── River.InsertTx (same tx as write) ───────┘
                                                    │
                                       Worker (River): notify · webhook · index
                                       · automation · git-ingest · obs-ingest
Adapters (circuit-broken): Identity · Notify · Logging · Metrics · Search · Upload
Redis: cache · rate limit
```

Every mutation: `BEGIN → mutate rows → INSERT activity → river.InsertTx(dispatch job) → COMMIT`.
Workers fan out to Notify, webhooks, automation rules, and the activity timeline.

## Modules (`internal/`)

`config · platform · httpapi · domain · service · repo · events · worker · integrations · auth · search`

## Data model (single-tenant)

Core: `users` (mirror of Identity subjects) · `projects` · `components` · `labels` · `milestones` ·
`releases` · **`issues`** (bug|task|feature|improvement) · `issue_labels` · `issue_components` ·
`issue_relations` · `issue_watchers` · `comments` · `attachments` (metadata; bytes on local disk
under `storage.attachments_dir`) ·
`git_commits` / `issue_commits` · `pull_requests` / `issue_pull_requests` · `activity` (append-only audit) ·
`webhooks` / `webhook_deliveries` · `automation_rules` / `automation_runs` · `integration_configs` ·
`api_tokens` · `saved_searches`.

Human IDs: `projects.next_issue_number` incremented in-tx → key rendered as `<PROJECT_KEY>-<number>` (e.g. `BUG-421`).

See `db/migrations/` for the authoritative schema and `api/openapi.yaml` for the API contract.

## API docs

The API serves its own docs: `api/openapi.yaml` is `go:embed`'d into the binary and exposed
as interactive **Swagger UI at `/docs`** (raw spec at `/openapi.yaml`) — so the docs can't
drift from the running build. The Swagger UI assets are vendored
(`internal/httpapi/swaggerui`) and served under `/swagger-ui/`, so the docs work with no CDN
or external calls. All three routes are unauthenticated (next to `/healthz`); "Try it out"
issues live `/api/v1` calls once you **Authorize** with an `obt_` bearer token.

## Integrations

| Service | Direction | Mechanism |
|---|---|---|
| Identity | inbound auth | OIDC discovery + JWKS (RS256), cached |
| Notify | outbound | worker → REST, idempotency key |
| Logging | in+out | we emit slog; it POSTs `/integrations/logging/alerts` → dedupe→bug |
| Metrics | in+out | we expose `/metrics`; it POSTs `/integrations/metrics/alerts` |

## Scalability notes

Per-project number counter (per-row contention, mitigate with batch reservation) · River queue throughput
(Postgres `SKIP LOCKED`, scale workers) · webhook fan-out (per-endpoint caps, backoff, DLQ) · obs-ingest
storms (fingerprint dedupe) · FTS at scale (partition, or a dedicated index if Postgres stops
coping — but only once it actually does) · activity growth
(time partitioning + BRIN) · attachments (direct-to-Upload, never through API).

## Build / run

`make tools` → `make generate` → `make migrate` → `make dev` (API) + `make worker`.
Full stack: `make up` (docker compose). See `deploy/` for compose + Helm.
