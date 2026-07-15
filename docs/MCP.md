# MCP server

`cmd/mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server that
exposes Omni-BugTracker to AI assistants (Claude Code, Claude Desktop, …) as typed
tools. It is a thin **client** of the REST API — it holds no database connection and
duplicates no business logic, so every call runs with the same RBAC as any other API
caller. You authenticate it with a personal `obt_` API token; each user brings their
own, preserving their role and permissions.

Transport is **stdio**: the AI client launches the binary as a subprocess and speaks
MCP over its stdin/stdout.

## Build

```bash
make mcp          # → ./bin/omni-bt-mcp
# or
go build -o bin/omni-bt-mcp ./cmd/mcp
```

## Configure

The server needs two things, via flags or environment variables (flags win):

| Setting     | Flag         | Env                  | Default                          |
|-------------|--------------|----------------------|----------------------------------|
| API base URL| `--base-url` | `OMNI_BT_API_URL`    | `http://localhost:8080/api/v1`   |
| API token   | `--token`    | `OMNI_BT_API_TOKEN`  | *(required)*                     |
| Timeout     | `--timeout`  | `OMNI_BT_API_TIMEOUT`| `30s`                            |

The base URL **must include the `/api/v1` prefix**. Mint a token in the web UI under
**Settings → API tokens** (it's shown once; it's `obt_`-prefixed and sent as a bearer).

### Register with Claude Code

Either drop a `.mcp.json` in your project (see `.mcp.json.example`):

```json
{
  "mcpServers": {
    "omni-bugtracker": {
      "command": "/absolute/path/to/bin/omni-bt-mcp",
      "env": {
        "OMNI_BT_API_URL": "http://localhost:8080/api/v1",
        "OMNI_BT_API_TOKEN": "obt_your_token_here"
      }
    }
  }
}
```

…or add it from the CLI:

```bash
claude mcp add omni-bugtracker \
  --env OMNI_BT_API_URL=http://localhost:8080/api/v1 \
  --env OMNI_BT_API_TOKEN=obt_your_token_here \
  -- /absolute/path/to/bin/omni-bt-mcp
```

Point `OMNI_BT_API_URL` at your deployment (e.g. `http://chizu:8092/api/v1`) or a local
dev API on `:8080`.

## Tools

68 tools spanning the API. Highlights:

- **Discovery**: `whoami`, `list_users`, `get_dashboard`, `list_projects`, `get_project`,
  `search_issues` (full-text), `list_issues` (GitHub-style filter grammar).
- **Issues**: `get_issue`, `create_issue`, `update_issue`, `transition_issue`
  (workflow-validated), `move_issue`, `delete_issue`, `bulk_update_issues`.
- **Collaboration**: comments (`list`/`add`/`update`/`delete`), relations, watchers,
  `get_issue_activity`, `list_issue_commits`.
- **Project config**: labels, components, milestones, releases, members
  (list/create/update/delete), project admin.
- **Boards / ops**: board + column management, saved searches, webhooks (+ deliveries /
  redeliver), automation rules & runs.
- **Attachments**: `list_attachments`, `get_attachment` (inlines text; summarizes binary),
  `delete_attachment`.

Notes for AI usage:

- Issues are addressed by **key** (`BUG-421`), projects by **key** (`BUG`). Most
  id-addressed entities (comments, relations, components, …) use their **UUID**.
- `create_issue` / `update_issue` / `bulk_update_issues` accept `assignee_email` and
  resolve it to a user id via `list_users`, or take a raw `assignee_id` UUID.
- `list_issues` `filter` grammar: `is:open assignee:@me severity:critical type:bug
  label:regression component:api milestone:<uuid> release:<uuid>` plus free text.
- Errors carry the HTTP status and problem detail (e.g. `403 (forbidden): missing
  issue:create`, `409 (invalid transition): open → resolved`) so the model can self-correct.

**Not supported over MCP**: uploading attachment bytes (multipart file bodies don't fit
tool arguments) — create the attachment via the web UI or REST API.

## Verify

```bash
# Registration + full catalog (no API needed):
go test ./internal/mcpserver -run TestToolCatalog -v

# Live round-trip against a running API (create → comment → transition → get + an
# illegal-transition error). Requires a token; uses/creates a scratch project (MCPT):
OMNI_BT_API_URL=http://localhost:8080/api/v1 \
OMNI_BT_API_TOKEN=obt_… \
go test ./internal/mcpserver -run TestStdioRoundTrip -v
```
