# aeman

Backend-less project management in the spirit of Flant's **Nixon** and **Ford**, built on top of pluggable data providers. The first provider is **GitHub Projects v2**.

aeman has no database and no server-side state of its own. The UI is a single-page application that talks to a data provider through a small, extensible interface; the GitHub provider reads and writes a GitHub Project (v2). The whole thing ships as one self-contained Go binary with the compiled frontend embedded via `go:embed`.

## Concept

Two complementary views, inspired by Flant's **Ford** (engineer's day) and **Nixon** (planning):

- **Me** — your personal day board. Your cards for the selected day, stacked into four colour zones, with a notes log on the right:
  - **Gray** — regular, planned work.
  - **Green** — start only when every other zone is clear.
  - **Yellow** — popped up unplanned during the day.
  - **Red** — must be resolved before the end of the day.
- **Team** — the team board: a people × zones grid for the selected day, filtered by team. Columns are people (with their GitHub avatar and name), rows are the same colour zones. Columns can be dragged or shuffled, and a person keeps a column even on days they have no cards.

Each card carries a **readiness slider** (0–100%), a **stage** (Locked / Review / Done) that recolours the bar, an optional **team**, and links back to its source issue. Click a card's avatar to reassign its team or person. There is intentionally no built-in time tracker.

### Sprints

Sprints are open-ended and advanced by hand. A card belongs to a sprint (its **sprint start** day) and is shown on every day from its **start** date through that sprint day — so a long-running card stays visible on past days, while days after the last sprint go empty (the cue to start a new one). **Start sprint** carries a team's unfinished cards into a new sprint dated the selected day, with a *no team* option too. The current sprint is tracked per team, so an engineer on several teams sees each team's current cards at once.

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

aeman keeps all of its state in GitHub Project fields and **creates the ones it needs lazily**: point it at any project and the first card or team change provisions the missing fields (Zone, Progress, Stage, Team, Start, Sprint Start, Day). No manual setup required.

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

`aeman serve` flags: `--addr` (listen address), `--owner` (default org/user), `--project` (default project number), `--open` (open browser), `--verbose`.

## License

[Apache License 2.0](LICENSE).
