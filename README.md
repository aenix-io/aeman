<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.svg">
    <img src="docs/logo.svg" alt="aeman" width="300">
  </picture>
</p>

# aeman

A short-term planning system for engineering teams — it keeps engineers focused, runs daily sprints, and makes unplanned work visible. **A git repository is the storage**: a board is a repository of small YAML/Markdown files and every action is a commit, so aeman has no database of its own and the board's history is a plain git log. The whole thing ships as one self-contained Go binary: an embedded React SPA (via `go:embed`), a JSON REST API, a WebSocket **watch** stream that keeps every open board updated live, and an MCP server for AI agents — all driving the same board service, with the repository as the single source of truth.

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
│  aeman binary (Go)                           │        │  git repository      │
│                                              │        │  (GitHub, GitLab, …) │
│  embedded SPA ───► REST /api/v1 ──┐          │  push  │                      │
│       ▲                           │  board   ┼───────►│  cards/…/<id>.md     │
│       └──── WS /api/v1/watch ◄────┤  service │  fetch │  teams/<id>.yaml     │
│                                   │  + cache ◄────────┼  projects/…          │
│  AI agents ───► MCP (stdio, /mcp)─┘  + clone │        │  one commit/action   │
└──────────────────────────────────────────────┘        └──────────────────────┘
```

- **Kubernetes-style API and live sync**: cards, sprints, notes and the board order are resources (`{kind, metadata, spec, status}`); a client LISTs them (`GET /api/v1/cards`) and then applies a WATCH stream of `ADDED / MODIFIED / DELETED` resource events over a WebSocket — optionally scoped to one view by the same selectors LIST takes. Every write — from the UI, the REST API or an agent over MCP — goes through one board service with a shared in-memory store, is answered at once, and reaches every open board in about a second (a client's own changes are not echoed back to it).
- **Every request is one commit.** The server keeps a shallow clone of the board's repositories under `--data`, commits each request's writes as one commit (author = the person, committer = the server, machine-readable `Aeman-*` trailers), pushes in the background and fetches other replicas' commits on a timer; a rejected push is re-applied on the new tip field by field. The card's activity feed *is* this history.
- **Visibility by repository.** A board may span several repositories (domains): a closed project in a private repository next to the shared one. A visitor sees the union of what they can read — an unreadable domain is absent, not empty — and writes need write access to the repository they land in. Design: [docs/design/git-backend.md](docs/design/git-backend.md).
- The browser never holds a token: the binary resolves the identity server-side (local `gh` login, or per-user OAuth sessions in the self-hosted mode); the push credential is the server's (`AEMAN_GIT_TOKEN`).

### Repository layout

```
board.yaml                        # schema, title
teams/<id>.yaml                   # a team and its sprint pointer (teams/_.yaml = no team)
projects/<id>/project.yaml        # a project; its epic columns and deadlines beside it
processes/<id>/process.yaml       # recurring work; its tasks beside it
cards/<a>/<b>/<id>.md             # a card: YAML front-matter, description, ## Notes
```

Ids are ULIDs; files keep unknown keys, so hand edits and other tools survive. `aeman init --repo <url>` bootstraps an empty repository; `aeman migrate --owner <org> --board <n> --repo <url>` moves a GitHub Projects v2 board over, once, with its history as commits.

## Requirements

- A git repository aeman can push to over HTTPS (`AEMAN_GIT_TOKEN`; the local mode falls back to `gh auth token`), or a local path for a single-user setup.
- [GitHub CLI (`gh`)](https://cli.github.com/) for the local identity and the migration (`gh auth login`).
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

`aeman serve` flags: `--repo name=url` (repeatable; the board's repositories, primary first — env `AEMAN_REPOS`), `--data` (where the clones live), `--history` (how far back the log is loaded in the background, default 2 weeks) and `--history-max` (how far a card's log may deepen on demand, default a year), `--sync-interval` (fetch cadence, 15 s), `--unpushed-warn` (age of an unpushed commit that turns `/api/healthz` degraded, 5 m), `--committer` and `--author-email`, `--addr`, `--open`, `--verbose`. Each flag has an `AEMAN_*` environment twin; the push credential is `AEMAN_GIT_TOKEN`, never a flag. ("Project" is aeman's own planning entity — a group of epic columns — not a repository.)

## API and MCP server

The same binary drives the board three ways: the embedded UI, a JSON HTTP API under `/api/v1`, and an MCP server for AI agents (`aeman mcp` on stdio, or mounted at `/mcp` in the self-hosted OAuth mode). All of them call the same board service, so a change made by an agent shows up on every open board live. `GET /api/v1` returns a machine-readable catalog of every endpoint; see [docs/api.md](docs/api.md) for the endpoints, the card model, the watch protocol, the MCP tool set and client configuration. The board logic itself is importable: the packages under `pkg/` (domain rules, board service, git storage, MCP tool set) let external tools — e.g. a local, privacy-preserving MCP server over its own clone — run the exact same board contract; see [docs/embedding.md](docs/embedding.md).

```sh
aeman mcp --repo board=https://github.com/acme/planning.git   # the MCP server on stdio, over its own clone
```

## Self-hosted deploy (multi-user)

For a shared instance where every visitor signs in with GitHub — their token decides which of the board's repositories they may read and write — set the OAuth environment variables and the binary switches from local `gh` mode to a GitHub OAuth web flow with per-user sessions:

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
