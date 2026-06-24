# aeman

Backend-less project management in the spirit of Flant's **Nixon** and **Ford**, built on top of pluggable data providers. The first provider is **GitHub Projects v2**.

aeman has no database and no server-side state of its own. The UI is a single-page application that talks to a data provider through a small, extensible interface; the GitHub provider reads and writes a GitHub Project (v2). The whole thing ships as one self-contained Go binary with the compiled frontend embedded via `go:embed`.

## Concept

Two complementary views, modelled after Flant's tools:

- **Ford** — the engineer's day board. Cards for today, grouped into four colour zones:
  - **Gray** — regular, planned work.
  - **Green** — start only if everything in the other zones is done.
  - **Yellow** — popped up unplanned during the day.
  - **Red** — must be resolved before the end of the working day.
- **Nixon** — the planning board for leads/managers: backlog, prioritisation, layout across days and sprints.

Each card carries a **readiness slider** (0–100%) and links back to its source issue. There is intentionally no built-in time tracker.

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
