# aeman API and MCP server

aeman exposes its board service three ways: the embedded UI, a JSON HTTP API under `/api/v1`, and an MCP (Model Context Protocol) server for AI agents. All three call the same board logic over a shared in-memory store, so they behave identically — and every change any of them makes is pushed to all connected clients over the WebSocket watch stream.

## Authentication

Neither surface invents its own auth. They reuse aeman's existing token resolution.

The HTTP API resolves a GitHub token the same way the `/api/github` reverse proxy does: from the local `gh` CLI (`gh auth token`) in the default local run mode, or from the visitor's session in the self-hosted OAuth mode.

The stdio MCP server (`aeman mcp`) runs as a local, single-user process and mirrors local mode: it reads `GITHUB_TOKEN` (or `GH_TOKEN`) from the environment, falling back to `gh auth token`. In the self-hosted OAuth mode the MCP server is also mounted over HTTP at `/mcp`, authenticated with per-user OAuth tokens.

## Board selection and lock-board

Both surfaces target one project board, identified by an `owner` (GitHub org or user) and a `project` number.

The HTTP API takes them as query parameters (`?owner=acme&project=7`), falling back to the server's `--owner`/`--project` defaults. The MCP tools take optional `owner`/`project` arguments, falling back to the `aeman mcp` defaults.

When started with `--lock-board` (or `AEMAN_LOCK_BOARD=1`), aeman pins the board to its configured `--owner`/`--project` and ignores any client-supplied owner/project. Use this when exposing aeman to clients that must not roam across projects.

## HTTP API

Base path: `/api/v1`. All requests and responses are JSON. Errors are returned as `{"error": "..."}` with an appropriate status code (400 bad request, 401 no token, 404 board/card/note not found, 422 missing field or no underlying issue, 502 upstream GitHub error).

`GET /api/v1` itself is a public, machine-readable catalog of every endpoint below.

### Reads

| Method & path | Purpose |
| --- | --- |
| `GET /api/v1/board` | Board identity, field metadata and per-team sprint pointers. |
| `GET /api/v1/snapshot` | Full board snapshot: identity, fields, **all cards** and sprint pointers (the watch LIST). |
| `GET /api/v1/watch` | WebSocket stream of board change events (see below). |
| `GET /api/v1/team?team=&day=` | Team grid view for a team on a day (day defaults to today). |
| `GET /api/v1/me?user=&day=` | Personal day view (user = GitHub login, empty = everyone). |
| `GET /api/v1/weekly?team=&week=` | Weekly plan, split into wed/fri bands (week = a Monday). |

### Writes

| Method & path | Body | Purpose |
| --- | --- | --- |
| `POST /api/v1/cards` | `{title*, zone, day, start, sprintStart, plan, week, team, assignee, reviewOf, startNewSprint}` | Create a draft-issue card (201). Joins the team's current sprint by default; `plan`+`week` create a weekly-plan card instead. |
| `POST /api/v1/carry-over` | `{team}` | Advance the team's sprint to today and carry its unfinished cards forward (concurrent server-side). |
| `POST /api/v1/carry-week` | `{team, week}` | Pull unfinished plan cards from earlier weeks into the week. |
| `POST /api/v1/sprint-state` | `{team, current, previous}` | Set a team's sprint pointer directly. |
| `DELETE /api/v1/cards/{id}` | — | Delete a card (cascades to its linked review card). |
| `POST /api/v1/cards/{id}/stage` | `{stage}` | Set the stage: `locked`, `review`, `done`, or `""` to clear. |
| `POST /api/v1/cards/{id}/in-progress` | `{}` | Move to the implicit In Progress status. |
| `POST /api/v1/cards/{id}/progress` | `{progress}` | Set readiness (0–100), running the done auto-link. |
| `POST /api/v1/cards/{id}/zone` | `{zone}` | Set the colour zone (`gray`/`green`/`yellow`/`red`, `""` clears). |
| `POST /api/v1/cards/{id}/day` | `{day}` | Set the due day. |
| `POST /api/v1/cards/{id}/start` | `{start}` | Set the scheduled start date. |
| `POST /api/v1/cards/{id}/sprint-start` | `{sprintStart}` | Set the sprint the card belongs to. |
| `POST /api/v1/cards/{id}/plan` | `{plan}` | Set the weekly-plan band (`wed`/`fri`, `""` releases). |
| `POST /api/v1/cards/{id}/week` | `{week}` | Set the plan week (a Monday). |
| `POST /api/v1/cards/{id}/assignee` | `{login}` | Set or clear (`""`) the assignee. |
| `POST /api/v1/cards/{id}/team` | `{team, day}` | Move to a team (joins its sprint). |
| `POST /api/v1/cards/{id}/take-plan` | `{engineer, zone, day}` | Take a plan card into work. |
| `POST /api/v1/cards/{id}/release-plan` | `{}` | Release a card from the weekly plan. |
| `POST /api/v1/cards/{id}/move` | `{afterId}` | Reorder after another card (`""` = to the top). |
| `POST /api/v1/cards/{id}/note` | `{text}` | Append a work note. |
| `PATCH /api/v1/cards/{id}/notes/{noteId}` | `{text}` | Edit a work note. |
| `DELETE /api/v1/cards/{id}/notes/{noteId}` | — | Delete a work note. |
| `POST /api/v1/cards/{id}/description` | `{description}` | Set the free-form description. |
| `POST /api/v1/cards/{id}/rename` | `{title}` | Rename a card. |
| `POST /api/v1/cards/{id}/review` | `{reviewer, day}` | Send to review (creates a linked review card). |
| `POST /api/v1/cards/{id}/review/reassign` | `{reviewer, day}` | Point the review at another reviewer. |
| `POST /api/v1/cards/{id}/review/remove` | `{}` | Delete the linked review card. |

Card-mutating endpoints respond with the updated card.

### Live updates: snapshot + watch

Clients follow the Kubernetes list/watch pattern:

1. `GET /api/v1/snapshot?owner=&project=` — the full board.
2. `GET /api/v1/watch?owner=&project=&client=<id>` — upgrade to a WebSocket; each text frame is one event:

```json
{ "type": "ADDED" | "MODIFIED" | "DELETED" | "RELOAD", "card": { ... } }
```

Apply `ADDED`/`MODIFIED`/`DELETED` to the listed board by `card.itemId`. `RELOAD` carries no card and asks the client to re-list (a sprint pointer moved, or cards were reordered). On reconnect, re-list to reconcile.

The optional `client` id keys **echo suppression**: send the same value in the `X-Aeman-Client` header on your own mutations and the server will not stream your own changes back on that watch connection (your optimistic state and the mutation responses already carry them).

### Card shape

A card maps a project item onto the well-known fields aeman understands (resolved by field name, case-insensitively):

```json
{
  "itemId": "PVTI_...",
  "contentId": "DI_...",
  "isDraft": true,
  "title": "Wire up the API",
  "url": "https://github.com/...",
  "number": 12,
  "repository": "acme/repo",
  "state": "OPEN",
  "assignees": ["octocat"],
  "author": "octocat",
  "team": "platform",
  "zone": "red",
  "zoneOptionId": "abc123",
  "progress": 40,
  "stage": "review",
  "day": "2026-07-04",
  "startDate": "2026-07-01",
  "sprintStart": "2026-07-01",
  "plan": "wed",
  "week": "2026-06-29",
  "reviewOf": "PVTI_...",
  "sprintTitle": "Sprint 3",
  "status": "In Progress",
  "description": "Free-form details",
  "notes": [{ "id": "...", "body": "...", "createdAt": "...", "author": "octocat", "source": "comment" }],
  "createdAt": "2026-06-20T10:00:00Z"
}
```

`zone` is the colour zone derived from the zone single-select option colour (gray/green/yellow/red ↔ Planned/If-time-left/Unplanned/Critical). `notes` come from issue/PR comments, or from dated lines in a draft issue's body when the card has no comment thread. `startDate` vs `sprintStart` and the visibility rules they drive are documented in [dates.md](dates.md).

### Sprint membership on create (`startNewSprint`)

On create, a card joins its team's **current sprint** (`sprintStart` = the sprint's start day); a team with no sprint yet records the card's day as its first one. `startNewSprint: true` forces the pointer to (re)start on the card's day. Explicit `start`/`sprintStart` values override the defaults, and `plan`+`week` create a weekly-plan card with no sprint dates at all.

### Examples

```sh
# Full board snapshot
curl 'http://127.0.0.1:8765/api/v1/snapshot?owner=acme&project=7'

# Create a critical card assigned to octocat
curl -X POST 'http://127.0.0.1:8765/api/v1/cards?owner=acme&project=7' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Fix the build","zone":"red","assignee":"octocat"}'

# Bump readiness to 80%
curl -X POST 'http://127.0.0.1:8765/api/v1/cards/PVTI_xxx/progress?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"progress":80}'

# Add a note
curl -X POST 'http://127.0.0.1:8765/api/v1/cards/PVTI_xxx/note?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"text":"Deployed to staging"}'

# Start a new sprint for a team, carrying unfinished cards
curl -X POST 'http://127.0.0.1:8765/api/v1/carry-over?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"team":"platform"}'
```

## MCP server

`aeman mcp` starts a Model Context Protocol server on **stdio** (the right transport for a local, single-user MCP). In the self-hosted OAuth mode the same tool set is served over HTTP at `/mcp`. Every tool maps to one board-service operation:

| Tool | Purpose |
| --- | --- |
| `get_board` | Board identity, field metadata and per-team sprint pointers. |
| `team_view` | Team grid cards for a team on a day. |
| `me_view` | A person's day-board cards. |
| `weekly_plan` | A team's weekly-plan cards, split into wed/fri bands. |
| `create_card` | Create a card that joins (or starts) its team's sprint. |
| `carry_over` | Advance a team's sprint to today, carrying unfinished cards. |
| `carry_week` | Pull unfinished plan cards into the target week. |
| `set_stage` / `set_in_progress` / `set_progress` | Stage and readiness. |
| `send_to_review` / `reassign_reviewer` / `remove_reviewer` | The review-card cycle. |
| `set_assignee` / `set_team` | Reassign person or team. |
| `take_into_plan` / `release_from_plan` | Weekly-plan membership. |
| `move_card` / `delete_card` / `rename_card` / `add_note` | Card basics. |

Each tool accepts optional `owner`/`project` to pick the board, defaulting to the server's configuration (and ignored under `--lock-board`). Changes made by agents are streamed to every open board over the watch, like any other write.

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
