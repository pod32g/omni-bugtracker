# Omni-BugTracker — Architecture

Developer-first, self-hosted issue & bug tracker for the Omni ecosystem.
Design philosophy: **simplicity over completeness, API-first, everything automatable.**

## Locked decisions

| Area | Decision | Why |
|---|---|---|
| Topology | **Modular monolith** in Go (one binary, `server` + `worker` modes) | One bounded context; avoids distributed-systems overhead. |
| API | **Design-first OpenAPI 3.1** → generated Go handlers + TS client | The API *is* the contract; UI can't exceed it. |
| Data access | **pgx v5 + sqlc** (type-safe SQL, no ORM) | Predictable queries, no reflection magic. |
| Jobs / events | **River** (Postgres job queue) with transactional `InsertTx` | Enqueue in the same tx as the domain write = built-in transactional outbox. No separate outbox table, no Redis for jobs. |
| Redis | Cache + rate limiting only | Job durability lives in Postgres via River. |
| Tenancy | **Single-tenant** (one org per deployment) | Chosen for simplicity; no `tenant_id`/RLS. |
| Auth | **Omni-Identity only** (OIDC/OAuth2/LDAP terminate there); we validate JWTs + issue hashed API tokens | No local passwords. |
| Search | Postgres **FTS** primary + **Omni-Search** projection | Local search always works; global search when Omni-Search is up. |
| Issues | **One `issues` table** with a `type` discriminator + nullable bug fields + `jsonb` escape hatch | No table-per-type, no custom-field engine. |

## Runtime shape

```
React SPA ──HTTPS(JWT|token)──► API (chi) ──► services ──► repo (sqlc/pgx) ──► Postgres
                                    │                                            ▲
                                    └── River.InsertTx (same tx as write) ───────┘
                                                    │
                                       Worker (River): notify · webhook · index
                                       · automation · git-ingest · obs-ingest
Adapters (circuit-broken): Identity · Notify · Logging · Metrics · Search · Upload
Redis: cache · rate limit
```

Every mutation: `BEGIN → mutate rows → INSERT activity → river.InsertTx(dispatch job) → COMMIT`.
Workers fan out to Notify, webhooks, Omni-Search indexing, automation rules, and the activity timeline.

## Modules (`internal/`)

`config · platform · httpapi · domain · service · repo · events · worker · integrations · auth · search`

## Data model (single-tenant)

Core: `users` (mirror of Identity subjects) · `projects` · `components` · `labels` · `milestones` ·
`releases` · **`issues`** (bug|task|feature|improvement) · `issue_labels` · `issue_components` ·
`issue_relations` · `issue_watchers` · `comments` · `attachments` (metadata; bytes in Omni-Upload) ·
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
| Search | outbound | index worker pushes docs; `/search` fans out |
| Upload | outbound | presigned direct upload; we store metadata only |

## Scalability notes

Per-project number counter (per-row contention, mitigate with batch reservation) · River queue throughput
(Postgres `SKIP LOCKED`, scale workers) · webhook fan-out (per-endpoint caps, backoff, DLQ) · obs-ingest
storms (fingerprint dedupe) · FTS at scale (delegate to Omni-Search, partition) · activity growth
(time partitioning + BRIN) · attachments (direct-to-Upload, never through API).

## Build / run

`make tools` → `make generate` → `make migrate` → `make dev` (API) + `make worker`.
Full stack: `make up` (docker compose). See `deploy/` for compose + Helm.
