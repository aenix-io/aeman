<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.svg">
    <img src="docs/logo.svg" alt="aeman" width="300">
  </picture>
</p>

# aeman

A short-term planning system for engineering teams — it keeps engineers focused, runs daily sprints, and makes unplanned work visible. **GitHub Projects v2 is the storage**, so aeman has no database of its own. The whole thing ships as one self-contained Go binary: an embedded React SPA (via `go:embed`), a JSON REST API, a WebSocket **watch** stream that keeps every open board updated live, and an MCP server for AI agents — all driving the same board service, with GitHub as the single source of truth.

## Concept

Two complementary views — a personal day board and a team board:

- **Me** — your personal day board. Your cards for the selected day, stacked into four colour zones, with an editable notes log on the right. You can also **View as** another person to see (and act on) their board, with a one-click reset back to yourself.
  - **Gray** — regular, planned work.
  - **Green** — start only when every other zone is clear.
  - **Yellow** — popped up unplanned during the day.
  - **Red** — must be resolved before the end of the day.
- **Team** — the team board: a people × zones grid for the selected day, filtered by team. Columns are people (with their GitHub avatar and name), rows are the same colour zones. Columns can be dragged or shuffled, and a person keeps a column even on days they have no cards.

Each card carries a **readiness slider** (0–100%), a **stage** (Locked / Review / Recurrent / Done) that recolours the bar, an optional **team**, an age counter, and links back to its source issue. Click a card's avatar to reassign its team or person, or the day counter to edit its dates. There is intentionally no built-in time tracker.

Every open board is **live**: edits made by teammates — or by AI agents over MCP — appear on everyone's screen in about a second, without reloading.

### Sprints

Sprints are open-ended and advanced by hand: **Carry over** starts a team's new sprint on today and pulls its unfinished cards forward (with a *no team* group too). A card carries two dates — the sprint it belongs to (**sprint start**) and the day it actually started (**start**, kept as history) — so the Team board shows it on its sprint's day, on the day it was created, and on past sprint days it passed through. **+1 day / +1 week** defers a card counting from today, hiding it until its new day without losing that history. The current sprint is tracked per team, so an engineer on several teams sees each team's current cards at once. The full date model lives in [docs/dates.md](docs/dates.md).

### Weekly team plan

Below the team grid sits a weekly plan: business tasks assigned to a team for the week, split into two bands (by Wednesday / by Friday). A team lead drags a plan card onto a member to take it into work — the same card then shows up in the grid while staying in the plan, marked with a coloured left stripe. A thin overall progress bar tracks completion across the plan, and a per-week **Carry over** moves unfinished plan cards into the next week — a cycle independent of the daily sprints.

## Architecture

```
┌──────────────────────────────────────────────┐        ┌──────────────────────┐
│  aeman binary (Go)                           │        │  GitHub Projects v2  │
│                                              │        │  (data backend)      │
│  embedded SPA ───► REST /api/v1 ──┐          │        │                      │
│       ▲                           │  board   │   gh   │                      │
│       └──── WS /api/v1/watch ◄────┤  service ┼────────┼─►  GraphQL API      │
│                                   │  + cache │  token │                      │
│  AI agents ───► MCP (stdio, /mcp)─┘          │        │                      │
└──────────────────────────────────────────────┘        └──────────────────────┘
```

- **Kubernetes-style API and live sync**: cards, sprints, notes and the board order are resources (`{kind, metadata, spec, status}`); a client LISTs them (`GET /api/v1/cards`) and then applies a WATCH stream of `ADDED / MODIFIED / DELETED` resource events over a WebSocket — optionally scoped to one view by the same selectors LIST takes. Every write — from the UI, the REST API or an agent over MCP — goes through one board service with a shared in-memory store, reloads only the touched card, and reaches every open board in about a second (a client's own changes are not echoed back to it).
- The store is only a cache: GitHub stays the source of truth and the only persistence.
- The browser never holds a token: the binary resolves one server-side (local `gh auth token`, or per-user OAuth sessions in the self-hosted mode) for both the board service and the `/api/github/*` proxy used for profile lookups.
- The frontend keeps a small provider interface (the REST provider is the default; a direct-GraphQL provider remains as a reference), so additional backends (GitLab, Redmine, …) can be added.

### Fields

aeman keeps all of its state in GitHub Project fields and **creates the ones it needs lazily**: point it at any project and the first card or team change provisions the missing fields (Zone, Progress, Stage, Team, Start, Sprint Start, Day, Plan, Week). No manual setup required.

## Requirements

- [GitHub CLI (`gh`)](https://cli.github.com/) authenticated with the `project` and `repo` scopes (`gh auth login`).
- Go 1.26+ and Node.js 20+ to build from source.

## Build & run

```sh
make build          # builds the SPA, then the single binary
./aeman serve       # starts the server and opens the UI
```

During development:

```sh
make frontend       # build the SPA once into web/dist
make run            # go run ./cmd/aeman serve
```

`aeman serve` flags: `--addr` (listen address), `--owner` (default org/user), `--project` (default project number), `--lock-board` (pin the board and ignore client-supplied owner/project), `--open` (open browser), `--verbose`. The owner/project/lock-board defaults can also be set via `AEMAN_OWNER`, `AEMAN_PROJECT` and `AEMAN_LOCK_BOARD`.

## API and MCP server

The same binary drives the board three ways: the embedded UI, a JSON HTTP API under `/api/v1`, and an MCP server for AI agents (`aeman mcp` on stdio, or mounted at `/mcp` in the self-hosted OAuth mode). All of them call the same board service, so a change made by an agent shows up on every open board live. `GET /api/v1` returns a machine-readable catalog of every endpoint; see [docs/api.md](docs/api.md) for the endpoints, the card model, the watch protocol, the MCP tool set and client configuration. The board logic itself is importable: the packages under `pkg/` (domain rules, board service, GitHub backend, MCP tool set) let external tools — e.g. a local, privacy-preserving MCP server that talks to GitHub directly — run the exact same board contract; see [docs/embedding.md](docs/embedding.md).

```sh
aeman mcp --owner acme --project 7   # start the MCP server on stdio
```

## Self-hosted deploy (multi-user)

For a shared instance where every visitor signs in with GitHub and uses their own token, set the OAuth environment variables and the binary switches from local `gh` mode to a GitHub OAuth web flow with per-user sessions:

- `AEMAN_GITHUB_CLIENT_ID` / `AEMAN_GITHUB_CLIENT_SECRET` — from a GitHub OAuth App.
- `AEMAN_BASE_URL` — the public origin; the callback is `<AEMAN_BASE_URL>/auth/callback`.
- `AEMAN_SCOPES` — OAuth scopes (default `repo project`).

A `docker-compose.yml` (aeman + Caddy with automatic HTTPS) and a step-by-step guide are in [docs/deploy.md](docs/deploy.md):

```sh
cp .env.example .env   # fill in the OAuth + tunnel values
docker compose up -d --build
```

## License

[Apache License 2.0](LICENSE).
