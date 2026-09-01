# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

aeman — a short-term planning system for engineering teams. **Git repositories are the only storage** — a board is one or more repositories (domains) of YAML/Markdown files, every action a commit; the whole thing ships as one self-contained Go binary: an embedded React SPA (`go:embed` via `web/embed.go`), a JSON REST API under `/api/v1`, a WebSocket watch stream, and an MCP server for AI agents — all driving the same board service, with the repositories as the single source of truth. GitHub (or another forge) is only the identity provider and the remote the repositories live on.

## Commands

```sh
make build          # SPA (npm ci + vite build) then the single Go binary
make backend        # Go binary only (expects web/dist to exist)
make frontend       # SPA once into web/dist
make run            # go run ./cmd/aeman serve (frontend must be built once)
make lint           # golangci-lint run (CI pins golangci-lint v2, .golangci.yaml)
make fmt            # golangci-lint fmt
make test           # go test ./...
```

- Single Go test: `go test ./pkg/board -run TestMeView` (any package/regexp).
- Frontend (from `web/`): `npm run typecheck`, `npm test` (vitest), single test file: `npx vitest run src/theme.test.ts`.
- CI (`.github/workflows/ci.yml`) runs golangci-lint, the frontend build + vitest, `make backend`, and `go test ./...` — all of it must pass locally before pushing.

## Architecture

The server holds an in-memory cache of the board (`internal/server/boardstore.go`) over **shallow clones** of the board's repositories under `--data`; every write path (UI, REST, MCP) goes through one board service, is answered from the cache at once, and lands as **one commit per request** (`gitstore.WithScope`) that a background sync pushes; a fetch tick brings other replicas' commits in, a rejected push is re-applied by replaying the unpushed commits field by field (`gitstore.Repo.Rebase`). A change reloads only the touched cards and fans out to every open board via the watch stream, filtered by what each visitor may read. Design: `docs/design/git-backend.md`; rules G1–G26 in `docs/design/behavior-matrix.md`.

Layering, bottom-up:

- `pkg/board` — the pure domain, no I/O: zones, stages and the **derived** states (Done and In Progress are computed, never stored), progress clamps, the day/sprint date model, the view filters (`MeView`, `TeamGrid`, `WeeklyPlan`), sprints and carry-over selection, rank keys (`rank.go`), the domain rule (`domain.go`: which repository a card lives in — linked cards first, then project, then team) and the per-visitor projection (`Visible`).
- `pkg/boardservice` — the service every caller shares: the admission chain (clamps, review linkage, cancel/reactivate), the card actions (create, defer, remove, carry-over, carry-week, send-to-review, …). Events are not written anywhere: **the commits are the activity log** (`LogReader`).
- `pkg/gitstore` — the storage: the repository layout (`cards/<a>/<b>/<ulid>.md`, roster YAML), file codecs that keep unknown keys, ULIDs (deterministic for process turns), commits with `Aeman-*` trailers built through go-git plumbing (no worktree), shallow clone / deepen-since / push / fetch / rebase, `MultiBackend` over several domains (roster fragments merged on read, cross-domain moves as create-then-delete), the card log walker.
- `pkg/apiserver` — the Kubernetes-style resource types (`{kind, metadata, spec, status}`) served over LIST + WATCH.
- `pkg/mcpserver` — the MCP tool set (same board service).
- `internal/server` — HTTP/WS, OAuth sessions (self-hosted mode) vs local `gh` identity (`internal/ghcli`), the board cache and write queue, the git sync (`gitsync.go`, `gitmode.go`), per-visitor domain rights from the forge (`access.go`, `visible.go`).
- `internal/migrate` — the one-way migration from a GitHub Projects v2 board (`aeman migrate`), with its own minimal GitHub reader (`ghsource`).
- `web/` — the React SPA. The domain rules are **mirrored** in the frontend, and the mirrors are files, not scattered component code: `web/src/date.ts` and `web/src/sprint.ts` (the date and sprint model), `web/src/weekly.ts` (the weekly plan's bands, incl. a slot's derived band and a debt's), `web/src/removal.ts` (what each × does, and the optimistic patch it leaves), `web/src/placements.ts` + `web/src/domains.ts` (columns, mirrors and the one repository comparison), `web/src/viewquery.ts` (what each view asks the server for, including which days are shown as a snapshot — the same condition the server applies), with `MeBoard.tsx`/`TeamBoard.tsx`/`ProjectBoard.tsx` calling them rather than re-deciding. A change to a filter, date, sprint, band, removal or domain rule lands in both the Go and the TS copy — with a vitest case beside the Go one — or the optimistic UI diverges from the server.

The packages under `pkg/` are importable by external tools (see `docs/embedding.md`) — they are a public contract, not internal plumbing.

## TDD is mandatory

Write the test FIRST, watch it fail, then implement (Red → Green → Refactor) — no domain or service change lands without one. The codebase is built for it: every `pkg/*` package has a sibling `_test.go` (`filters_test.go`, `sprint_test.go`, `service_test.go`, …) and `pkg/boardservice/boardservicetest` provides a fake backend, so a new rule starts as a failing case there, not as code. Behaviour changes also get a row in `docs/design/behavior-matrix.md`. Frontend logic changes (`date.ts`, `sprint.ts`, theme) must ship a vitest case the same way — coverage there is currently thinner than on the Go side (`theme.test.ts`, `viewquery.test.ts` show the pattern), so extend it rather than mirror it.

Tests here are not happy-path exercises — they are the **second documentation and a demonstration of the contract**. A rule's test spells out the edges that define it: the boundary days of a visibility window, the empty-sprint degenerate case, the clamp at 10/90, the stray that must NOT be adopted, the torn run that must be idempotent. A reader should be able to learn the rule from its test names and cases alone; a test that only proves "it works when everything is fine" documents nothing and does not count as coverage for a rule change.

## Docs that MUST stay in sync with the code

The date/sprint/visibility logic is subtle and duplicated across consumers; the docs are load-bearing, not decoration:

- `docs/dates.md` — the date model and the Team/Me/Weekly visibility rules.
- `docs/api.md` — the REST/WATCH/MCP surface.
- `docs/design/behavior-matrix.md` — the behaviour matrix new rules get rows in.

**A tool may write the board's repositories directly** — aeman is open source and its storage is just files in git, so anything can commit to a board without going through this server. What such a writer has to reproduce is specified in `docs/design/git-backend.md` and summarised in `docs/design/plugin-impact.md`: the layout and file formats, the rank keys, the domain rule, the move protocol and the commit trailers. Keep those two documents true when a PR changes domain rules or the storage schema — `pkg/board` semantics, `pkg/boardservice` admission/actions, the `pkg/gitstore` layout, file formats, trailers or the domain/move rules — because a writer following a stale spec silently produces states this server would never create (cards that never appear on the daily board, say). There is no longer a private companion repository to update alongside.
