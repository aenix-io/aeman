# aeman API and MCP server

aeman exposes its board service three ways: the embedded UI, a JSON HTTP API under `/api/v1`, and an MCP (Model Context Protocol) server for AI agents. All three call the same board logic over a shared in-memory store, so they behave identically — and every change any of them makes is pushed to all connected clients over the WebSocket watch stream.

The API is Kubernetes-style: a small set of **resources** (`Card`, `Sprint`, `Note`, `Ordering`) shaped as `{kind, metadata, spec, status}`, LIST with selectors that reproduce the UI's views, PATCH for edits, and **actions** for everything with board-level rules. Clients state intent; the server applies the rules (clamps, review links, the date model of [dates.md](dates.md)) and streams the results. The design rationale lives in [design/api-redesign.md](design/api-redesign.md).

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

### Resources

| Method & path | Purpose |
| --- | --- |
| `GET /api/v1/board` | Board identity and the team roster. |
| `GET /api/v1/cards` | LIST cards (selectors below), in board order. A listing is the **board-row shape**: no `spec.description` — `status.links` carries the refs extracted from it (capped at 50), and the body itself is one `GET /cards/{uid}` away. `?fields=full` opts a genuine bulk reader into complete cards. |
| `POST /api/v1/cards` | Create a card (201). A title that is nothing but a GitHub issue/PR URL becomes that item's real title, with the link moved into the description (one-time, never re-synced). |
| `GET /api/v1/cards/{uid}` | One card. |
| `PATCH /api/v1/cards/{uid}` | Edit spec fields; only present fields apply, empty clears. |
| `DELETE /api/v1/cards/{uid}` | Hard delete (cascades to the linked review card). |
| `GET /api/v1/cards/{uid}/links` | URLs from the card's description: GitHub issue/PR references first (resolved to their live titles and states), plain links after. |
| `GET /api/v1/cards/{uid}/log` | The card's activity feed: recorded events (who changed the stage/progress/assignee/review/plan, when) merged chronologically with its work notes — the per-day delta without morning reports. Changes made outside aeman (directly in the GitHub Projects UI) are not recorded. |
| `GET /api/v1/cards/{uid}/notes` | The card's work notes. |
| `POST /api/v1/cards/{uid}/notes` | Append a note `{text}` (201). |
| `PATCH /api/v1/cards/{uid}/notes/{noteId}` | Edit a note `{text}`. |
| `DELETE /api/v1/cards/{uid}/notes/{noteId}` | Delete a note. |
| `GET /api/v1/sprints` | Per-team sprint pointers. |
| `PATCH /api/v1/sprints` | Set a pointer directly `{team, current, previous}`. |
| `GET /api/v1/ordering` | The board-level manual card order (a uid list). |
| `GET /api/v1/watch` | WebSocket stream of resource events (below). |

Note mutations return the card's full `NoteList`, so clients converge on the server's view of the thread.

### Actions

Actions carry the board rules — the client never reimplements them.

| Method & path | Body | Purpose |
| --- | --- | --- |
| `POST /api/v1/cards/{uid}/actions/remove` | `{from: "grid"\|"plan"}` | The smart ×: a current-sprint card demotes to the previous sprint; an untouched taken plan card releases back to the plan while a worked one sheds only its plan membership; a pure (unassigned, never-worked) plan card is deleted for real. |
| `POST /api/v1/cards/{uid}/actions/move` | `{after}` | Reorder after another card (`""` = to the top). |
| `POST /api/v1/cards/{uid}/actions/defer` | `{days}` | Push the scheduled day N days ahead of today (presses stack; a card created today relocates fully). |
| `POST /api/v1/cards/{uid}/actions/in-progress` | `{}` | The implicit In Progress status. |
| `POST /api/v1/cards/{uid}/actions/send-to-review` | `{reviewer, day}` | Create the linked review card (201) — or reassign the existing one (200). |
| `POST /api/v1/cards/{uid}/actions/remove-reviewer` | `{}` | Delete the linked review card. |
| `POST /api/v1/cards/{uid}/actions/take-into-plan` | `{engineer, zone, day}` | Take a weekly-plan card into work. |
| `POST /api/v1/cards/{uid}/actions/release-from-plan` | `{}` | Release a card from the weekly plan. |
| `POST /api/v1/sprints/actions/carry-over` | `{team, dryRun}` | Advance the team's sprint to today and carry its unfinished cards; finished recurrent cards reseed fresh copies. |
| `POST /api/v1/sprints/actions/carry-week` | `{team, week, dryRun}` | Pull unfinished plan cards from earlier weeks into the week (same recurrent reseeding). |

The carry actions return `{carried, reseeded}` counts; with `dryRun: true` they report the counts without changing anything — that backs the UI's confirm dialogs.

### Card shape

```json
{
  "kind": "Card",
  "metadata": {
    "uid": "PVTI_...",
    "contentId": "DI_...",
    "isDraft": true,
    "url": "https://github.com/...",
    "number": 12,
    "repository": "acme/repo",
    "author": "octocat",
    "createdAt": "2026-06-20T10:00:00Z"
  },
  "spec": {
    "title": "Wire up the API",
    "description": "Free-form details — on full resources only: a LIST omits it (that IS the \"not loaded\" marker; a full resource always carries it, even empty)",
    "team": "platform",
    "zone": "urgent",
    "assignees": ["octocat"],
    "progress": 40,
    "stage": "review",
    "dates": { "start": "2026-07-01", "end": "2026-07-04", "sprint": "2026-07-01" },
    "plan": { "band": "wed", "week": "2026-06-29" },
    "reviewOf": "PVTI_..."
  },
  "status": {
    "complete": false,
    "inProgress": false,
    "reviewedBy": "lllamnyp",
    "links": [
      { "kind": "pull", "url": "https://github.com/acme/repo/pull/7", "owner": "acme", "repo": "repo", "number": 7 }
    ]
  }
}
```

- **Zones are semantic**: `urgent`, `unplanned`, `planned`, `niceToHave` (or empty). The UI's colours are presentation.
- **`spec.dates`** is the date model of [dates.md](dates.md): `start` (the scheduled day), `end` (the visible range's end), `sprint` (sprint membership). PATCHing `dates.start` runs the calendar rule — the sprint follows the sprint that was active on the start day; patch only `dates.end` or `dates.sprint` for a granular change.
- **`spec.stage`** is `locked`, `review`, `recurrent` or empty. Done is **derived** (`status.complete`): 100% with no stage. Patching `stage: "done"` clears the stage and fills 100%; review/locked clamp progress to [10, 90]. Taking a card off review cancels its unfinished linked review card server-side.
- **`status`** is server-derived and read-only: `complete`, `inProgress`, `reviewedBy` (the assignee of the unfinished linked review card), and `links` — the references extracted from the description (unresolved; `GET /cards/{uid}/links` resolves GitHub refs to live titles and states). `status.links` is what lets a listing drop the description without blinding a row's links indicator.

### LIST selectors

`GET /api/v1/cards` reproduces the UI's views server-side:

- **No view** — defaults to the caller's personal **Me** board (their own cards in the active sprint). Who-am-I is resolved server-side (session/token login), so no `user` is needed; an explicit `?user=` still wins. This is where everyone works day to day.
- `?view=all` — every card on the board (the old bare-list behaviour; still honours the field/team filters).
- `?view=team&team=platform&day=2026-07-02` — the Team grid (the lead view) for a team on a day; `team=` accepts a comma-separated set (`team=platform,marketing`) so the multi-team board loads in one request. Day defaults to today.
- `?view=me&user=octocat&day=` — the personal day view for a specific user (empty user = the caller; on the Me view an empty user resolves to who-am-i via the handler).
- `?view=weekly&team=platform&week=2026-06-29` — the weekly plan (week = a Monday, defaults to the current week); the response also carries `weekly: {progress}` (recurrent cards excluded).
- Field selectors — `stage=`, `zone=`, `assignee=` — compose with a view or apply to all cards.
- `focus=true` — keep only cards workable right now (drops done, on-review and locked); the "what can I pick up now" filter.
- `reviews=true` — on a me/team view, append each card's linked review card so a client rendering the reviewer badge has it without a second request (the UI uses this; off by default so an agent's Me list isn't padded with review cards).
- `fields=full` — complete cards with descriptions, for genuine bulk readers (analytics over card bodies). The default is the board-row shape: reading one card's body is `GET /cards/{uid}`, not a fatter list.
- On the me / all lists, `team=` filters by a comma-separated set (`team=marketing,portal`) matching any of them.

### Live updates: list + watch

Clients follow the Kubernetes list/watch pattern:

1. LIST: `GET /api/v1/cards` (+ `/sprints`) — the current state.
2. WATCH: `GET /api/v1/watch?owner=&project=&client=<id>` — upgrade to a WebSocket; each text frame is one event:

```json
{ "type": "ADDED" | "MODIFIED" | "DELETED", "kind": "Card" | "Sprint" | "Ordering", "object": { ... } }
```

Apply Card events by `metadata.uid`; Sprint events replace a team's pointer; an Ordering event carries the full uid list to re-sort by. On reconnect, re-list to reconcile.

The optional `client` id keys **echo suppression**: send the same value in the `X-Aeman-Client` header on your own mutations and the server will not stream your own changes back on that watch connection (your optimistic state and the mutation responses already carry them).

**Scoped watch**: pass the same selector parameters as LIST (`view=`, `team=`, `stage=`, ...) and the subscription tracks that selection — a card entering it arrives as `ADDED` and one leaving it as `DELETED`, so a thin client can mirror a single view without knowing the board rules. Memberships are re-diffed when a sprint pointer moves and when the local day rolls over. `resources=cards,sprints,ordering` picks the kinds.

### Examples

```sh
# All cards, in board order
curl 'http://127.0.0.1:8765/api/v1/cards?owner=acme&project=7'

# The team grid for today
curl 'http://127.0.0.1:8765/api/v1/cards?owner=acme&project=7&view=team&team=platform'

# Create an urgent card assigned to octocat
curl -X POST 'http://127.0.0.1:8765/api/v1/cards?owner=acme&project=7' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Fix the build","zone":"urgent","assignees":["octocat"]}'

# Bump readiness to 80%
curl -X PATCH 'http://127.0.0.1:8765/api/v1/cards/PVTI_xxx?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"progress":80}'

# Add a note
curl -X POST 'http://127.0.0.1:8765/api/v1/cards/PVTI_xxx/notes?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"text":"Deployed to staging"}'

# Preview a carry-over, then run it
curl -X POST 'http://127.0.0.1:8765/api/v1/sprints/actions/carry-over?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"team":"platform","dryRun":true}'
curl -X POST 'http://127.0.0.1:8765/api/v1/sprints/actions/carry-over?owner=acme&project=7' \
  -H 'Content-Type: application/json' -d '{"team":"platform"}'
```

## MCP server

`aeman mcp` starts a Model Context Protocol server on **stdio** (the right transport for a local, single-user MCP). In the self-hosted OAuth mode the same tool set is served over HTTP at `/mcp`. The tools are a one-to-one projection of the HTTP API — same resources, same actions, same semantic zone names, item ids called `uid`:

| Tool | Purpose |
| --- | --- |
| `get_board` | Board identity and team roster. |
| `list_cards` | LIST with the same selectors (`view`, `team`, `day`, `user`, `week`, `stage`, `zone`, `assignee`, `focus`). Returns board ROWS (no descriptions; `status.links` carries the extracted refs). `title=<substring>` resolves a card someone mentioned by name to its uid in one cheap call; `full=true` opts a bulk reader into complete cards. No view defaults to your own Me board (who-am-i resolved server-side); `view=all` is the whole board, `view=team` the lead view. |
| `get_card` / `list_notes` / `list_links` | One card IN FULL — the detail pane, and the way to read a body after a `list_cards` row; its notes; its description links (GitHub refs resolved with titles). |
| `list_log` | The card's activity feed: events (stage/progress/review/plan changes with actor) + notes, one chronological list — read a card's delta instead of asking for morning reports. |
| `create_card` | Create a card (joins or starts its team's sprint; plan cards via `plan`+`week`). A title that is only a GitHub issue/PR URL is auto-filled from that item. |
| `update_card` | The PATCH: only provided fields apply, empty clears. The `description` is the card's shared body — and the place for reference links: include full URLs of related open PRs/issues in free form (encouraged); they are surfaced on the card and GitHub refs resolve to live titles/states (`list_links`). |
| `delete_card` / `remove_card` | Hard delete; the smart × (`from: grid\|plan`). |
| `move_card` / `defer_card` | Reorder; push the scheduled day ahead. |
| `send_to_review` / `remove_reviewer` | The review-card cycle (send reassigns when a review card exists). |
| `take_into_plan` / `release_from_plan` | Weekly-plan membership. |
| `carry_over` / `carry_week` | Sprint/week carry with `dryRun` count reports. |
| `add_note` / `edit_note` / `delete_note` | The note thread. |

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
