<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.svg">
    <img src="docs/logo.svg" alt="aeman" width="300">
  </picture>
</p>

# aeman

Backend-less project management, built on top of pluggable data providers. The first provider is **GitHub Projects v2**.

aeman has no database and no server-side state of its own. The UI is a single-page application that talks to a data provider through a small, extensible interface; the GitHub provider reads and writes a GitHub Project (v2). The whole thing ships as one self-contained Go binary with the compiled frontend embedded via `go:embed`.

## Concept

Two complementary views — a personal day board and a team board:

- **Me** — your personal day board. Your cards for the selected day, stacked into four colour zones, with an editable notes log on the right. You can also **View as** another person to see (and act on) their board, with a one-click reset back to yourself.
  - **Gray** — regular, planned work.
  - **Green** — start only when every other zone is clear.
  - **Yellow** — popped up unplanned during the day.
  - **Red** — must be resolved before the end of the day.
- **Team** — the team board: a people × zones grid for the selected day, filtered by team. Columns are people (with their GitHub avatar and name), rows are the same colour zones. Columns can be dragged or shuffled, and a person keeps a column even on days they have no cards.

Each card carries a **readiness slider** (0–100%), a **stage** (Locked / Review / Done) that recolours the bar, an optional **team**, an age counter, and links back to its source issue. Click a card's avatar to reassign its team or person, or the day counter to edit its start/finish dates. There is intentionally no built-in time tracker.

### Sprints

Sprints are open-ended and advanced by hand. A card belongs to a sprint (its **sprint start** day) and is shown on every day from its **start** date through that sprint day — so a long-running card stays visible on past days, while days after the last sprint go empty (the cue to start a new one). **Carry over** moves a team's unfinished cards into the selected day's sprint, with a *no team* option too. The current sprint is tracked per team, so an engineer on several teams sees each team's current cards at once.

### Weekly team plan

Below the team grid sits a weekly plan: business tasks assigned to a team for the week, split into two bands (by Wednesday / by Friday). A team lead drags a plan card onto a member to take it into work — the same card then shows up in the grid while staying in the plan, marked with a coloured left stripe. A thin overall progress bar tracks completion across the plan, and a per-week **Carry over** moves unfinished plan cards into the next week — a cycle independent of the daily sprints.

## Architecture

```
┌────────────────────────────┐        ┌──────────────────────┐
│  aeman binary (Go)         │        │  GitHub Projects v2  │
│                            │        │  (data backend)      │
│  ┌──────────────────────┐  │  gh    │                      │
│  │ embedded SPA (React) │  │ token  │                      │
│  │  Provider interface  │──┼────────┼─►  GraphQL / REST    │
│  └──────────────────────┘  │ proxy  │                      │
│  /api/github/* reverse     │        │                      │
│  proxy + gh auth token     │        │                      │
└────────────────────────────┘        └──────────────────────┘
```

- The browser never holds a token: the binary injects one from `gh auth token` and proxies `/api/github/*` to the GitHub API (also sidesteps CORS).
- The provider interface lives in the frontend, so additional backends (GitLab, Redmine, …) can be added without touching the Go layer.
- A future deployment mode can publish the same SPA as a static site on GitHub Pages, talking to the GitHub API directly.

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

`aeman serve` flags: `--addr` (listen address), `--owner` (default org/user), `--project` (default project number), `--lock-board` (pin the board and ignore client-supplied owner/project), `--open` (open browser), `--verbose`. The same defaults can be set via `AEMAN_ADDR`, `AEMAN_OWNER`, `AEMAN_PROJECT` and `AEMAN_LOCK_BOARD`.

## API and MCP server

The same binary exposes a Go-native GitHub Projects v2 client two ways: a JSON HTTP API under `/api/v1` and an MCP server for AI agents (`aeman mcp`, stdio transport). Both reuse aeman's token resolution (local `gh` token, or `GITHUB_TOKEN` for the MCP server) and share the same field mapping. See [docs/api.md](docs/api.md) for endpoints, the card model, the MCP tool set and client configuration.

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
