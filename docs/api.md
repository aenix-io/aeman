# aeman API and MCP server

aeman ships a Go-native client for GitHub Projects v2 (`internal/ghprojects`) and exposes it two ways: a JSON HTTP API served by the same binary as the SPA, and an MCP (Model Context Protocol) server for AI agents. Both share the exact same client code and field mapping, so they behave identically.

## Authentication

Neither surface invents its own auth. They reuse aeman's existing token resolution.

The HTTP API resolves a GitHub token the same way the `/api/github` reverse proxy does: from the local `gh` CLI (`gh auth token`) in the default local run mode. The browser never holds a token. If aeman later gains an OAuth multi-user mode, the API picks up its per-session tokens automatically, because it goes through the same `TokenSource`.

The MCP server runs as a local, single-user process and mirrors local mode: it reads `GITHUB_TOKEN` (or `GH_TOKEN`) from the environment, falling back to `gh auth token`.

## Board selection and lock-board

Both surfaces target one project board, identified by an `owner` (GitHub org or user) and a `project` number.

The HTTP API takes them as query parameters (`?owner=acme&project=7`), falling back to the server's `--owner`/`--project` defaults. The MCP tools take optional `owner`/`project` arguments, falling back to the `aeman mcp` defaults.

When started with `--lock-board` (or `AEMAN_LOCK_BOARD=1`), aeman pins the board to its configured `--owner`/`--project` and ignores any client-supplied owner/project. Use this when exposing aeman to clients that must not roam across projects.

## HTTP API

Base path: `/api/v1`. All requests and responses are JSON. Errors are returned as `{"error": "..."}` with an appropriate status code (400 bad request, 401 no token, 404 board/card not found, 422 missing field or no underlying issue, 502 upstream GitHub error).

| Method & path | Purpose |
| --- | --- |
| `GET /api/v1/board?owner=&project=` | Board identity and field metadata (id, number, title, url, owner, fields with single-select options). |
| `GET /api/v1/cards?owner=&project=` | All cards: `{ "cards": [ ... ] }`. |
| `POST /api/v1/cards?owner=&project=` | Create a draft-issue card. Body: `CreateCardInput`. Returns the created card (201). |
| `PATCH /api/v1/cards/{itemId}?owner=&project=` | Partial update. Body: `UpdateCardInput`. |
| `POST /api/v1/cards/{itemId}/move?owner=&project=` | Reorder. Body: `{ "afterId": "<itemId>" \| null }`; null moves to the top. |
| `DELETE /api/v1/cards/{itemId}?owner=&project=` | Remove the card from the board. |
| `POST /api/v1/cards/{itemId}/notes?owner=&project=` | Add a note. Body: `{ "text": "..." }`. |

### Card shape

A card maps a project item onto the well-known fields aeman understands (resolved by field name, case-insensitively):

```json
{
  "itemId": "PVTI_...",
  "contentId": "DI_...",
  "title": "Wire up the API",
  "isDraft": true,
  "url": "https://github.com/...",
  "number": 12,
  "repository": "acme/repo",
  "state": "OPEN",
  "assignees": ["octocat"],
  "zone": "red",
  "zoneOptionId": "...",
  "progress": 40,
  "day": "2026-06-26",
  "sprintTitle": "Sprint 3",
  "status": "In progress",
  "team": "Platform",
  "createdAt": "2026-06-20T10:00:00Z",
  "notes": [{ "id": "...", "body": "...", "createdAt": "...", "author": "octocat", "source": "comment" }],
  "fields": { "Zone": "Critical", "Progress": "40", "Estimate": "3" }
}
```

`zone` is the Ford colour zone derived from the zone single-select option colour (gray/green/yellow/red ↔ Planned/If-time-left/Unplanned/Critical). `fields` carries every field value verbatim by field name, so callers can read board-specific fields aeman has no dedicated role for. `notes` come from issue/PR comments, or from dated lines in a draft issue's body when the card has no comment thread.

### Create / update inputs

`CreateCardInput`: `{ "title" (required), "zone", "assignee", "day", "status", "team", "progress", "fields" }`.

`UpdateCardInput` is a partial patch — only the keys you send are changed: `{ "title", "zone", "progress", "day", "assignee", "status", "team", "fields" }`. For `zone`, `day`, `assignee`, `status` and `team`, an empty string clears the value. `fields` is an object keyed by board field name; aeman dispatches on each field's data type (single-select by option name, plus date/number/text).

Field roles are matched by name: zone (`zone`, `priority zone`, `зона`), progress (`progress`, `readiness`, `% done`, …), day (`day`, `date`, `due date`, …), sprint (`sprint`, `iteration`, …), status (`status`, `stage`, …), team (`team`, `group`, …).

### Examples

```sh
# List cards on a board
curl 'http://127.0.0.1:8765/api/v1/cards?owner=acme&project=7'

# Create a critical card assigned to octocat
curl -X POST 'http://127.0.0.1:8765/api/v1/cards?owner=acme&project=7' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Fix the build","zone":"red","assignee":"octocat","day":"2026-06-26"}'

# Bump readiness to 80%
curl -X PATCH 'http://127.0.0.1:8765/api/v1/cards/PVTI_xxx?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"progress":80}'

# Add a note
curl -X POST 'http://127.0.0.1:8765/api/v1/cards/PVTI_xxx/notes?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"text":"Deployed to staging"}'
```

## MCP server

`aeman mcp` starts a Model Context Protocol server on **stdio** (the correct transport for a local, single-user MCP). It exposes the same operations as tools:

| Tool | Purpose |
| --- | --- |
| `get_board` | Board identity and field metadata. |
| `list_cards` | All cards on the board. |
| `create_card` | Create a draft-issue card (title required; optional zone/assignee/day/status/team/progress). |
| `update_card` | Partial update by `itemId` (title/zone/progress/day/assignee/status/team). |
| `move_card` | Reorder by `itemId`, optional `afterId`. |
| `delete_card` | Delete by `itemId`. |
| `add_note` | Add a note to a card by `itemId`. |

Each tool accepts optional `owner`/`project` to pick the board, defaulting to the server's configuration (and ignored under `--lock-board`).

### Flags and environment

`aeman mcp` flags: `--owner`, `--project`, `--lock-board`, `--verbose`. Environment: `GITHUB_TOKEN`/`GH_TOKEN` (token), `AEMAN_OWNER`, `AEMAN_PROJECT`, `AEMAN_LOCK_BOARD`.

Logs go to stderr, never stdout, so they never corrupt the JSON-RPC stream.

### Configuring it in an MCP client

Claude Code / Claude Desktop style config:

```json
{
  "mcpServers": {
    "aeman": {
      "command": "aeman",
      "args": ["mcp", "--owner", "acme", "--project", "7"],
      "env": { "GITHUB_TOKEN": "ghp_..." }
    }
  }
}
```

Or add it from the command line:

```sh
claude mcp add aeman --env GITHUB_TOKEN=ghp_... -- aeman mcp --owner acme --project 7
```

If you omit `GITHUB_TOKEN`, the server falls back to `gh auth token`, so an authenticated `gh` is enough for local use.
