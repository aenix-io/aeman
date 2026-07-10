# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

aeman — a short-term planning system for engineering teams. **GitHub Projects v2 is the only storage**; the whole thing ships as one self-contained Go binary: an embedded React SPA (`go:embed` via `web/embed.go`), a JSON REST API under `/api/v1`, a WebSocket watch stream, and an MCP server for AI agents — all driving the same board service, with GitHub as the single source of truth.

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

The server is a **stateless mapping layer** over the GitHub GraphQL API — an in-memory cache (30s + WebSocket watch, `internal/server/boardstore.go`) is the only state it holds. Every write path (UI, REST, MCP) goes through one board service; a change reloads only the touched card and fans out to every open board via the watch stream.

Layering, bottom-up:

- `pkg/board` — the pure domain, no I/O: zones (resolved by option colour), stages and the **derived** states (Done and In Progress are computed, never stored), progress clamps, the day/sprint date model, the view filters (`MeView`, `TeamGrid`, `WeeklyPlan`), sprints and carry-over selection, the event/note log model.
- `pkg/boardservice` — the service every caller shares: the admission chain (clamps, review linkage, cancel/reactivate), the card actions (create, defer, remove, carry-over, carry-week, send-to-review, …), and the activity log — **every mutation records an event**.
- `pkg/ghprojects` — the storage mapping onto Projects v2: field roles matched by name (`domainRoleAliases`), zones by option colour, **lazy field provisioning** (`domainFieldSpecs`), draft-body note/event log vs the dedicated log comment on issue/PR cards. Option ids are re-created when options change — resolve them at read/write time, never cache.
- `pkg/apiserver` — the Kubernetes-style resource types (`{kind, metadata, spec, status}`) served over LIST + WATCH.
- `pkg/mcpserver` — the MCP tool set (same board service).
- `internal/server` — HTTP/WS, OAuth sessions (self-hosted mode) vs local `gh` token (`internal/ghcli`), the board cache.
- `web/` — the React SPA. The domain rules are **mirrored** in the frontend (`web/src/components/MeBoard.tsx`, `web/src/components/TeamBoard.tsx`, `web/src/date.ts`, `web/src/sprint.ts`): a change to a filter/date/sprint rule lands in both the Go and the TS copy, or the optimistic UI diverges from the server.

The packages under `pkg/` are importable by external tools (see `docs/embedding.md`) — they are a public contract, not internal plumbing.

## TDD is mandatory

Write the test FIRST, watch it fail, then implement (Red → Green → Refactor) — no domain or service change lands without one. The codebase is built for it: every `pkg/*` package has a sibling `_test.go` (`filters_test.go`, `sprint_test.go`, `service_test.go`, …) and `pkg/boardservice/boardservicetest` provides a fake backend, so a new rule starts as a failing case there, not as code. Behaviour changes also get a row in `docs/design/behavior-matrix.md`. Frontend logic changes (`date.ts`, `sprint.ts`, theme) must ship a vitest case the same way — coverage there is currently thinner than on the Go side (`theme.test.ts`, `viewquery.test.ts` show the pattern), so extend it rather than mirror it.

Tests here are not happy-path exercises — they are the **second documentation and a demonstration of the contract**. A rule's test spells out the edges that define it: the boundary days of a visibility window, the empty-sprint degenerate case, the clamp at 10/90, the stray that must NOT be adopted, the torn run that must be idempotent. A reader should be able to learn the rule from its test names and cases alone; a test that only proves "it works when everything is fine" documents nothing and does not count as coverage for a rule change.

## Docs that MUST stay in sync with the code

The date/sprint/visibility logic is subtle and duplicated across consumers; the docs are load-bearing, not decoration:

- `docs/dates.md` — the date model and the Team/Me/Weekly visibility rules.
- `docs/api.md` — the REST/WATCH/MCP surface.
- `docs/design/behavior-matrix.md` — the behaviour matrix new rules get rows in.

**Downstream skill docs live in `aenix-org/ccc`.** The aeman Claude Code plugin (`skills/aeman/skills/aeman/model.md` and `reference.md` in that repo) drives boards via `gh` directly, **bypassing this server**, by replicating the domain rules documented there. Any PR that changes domain rules or the storage schema — `pkg/board` semantics, `pkg/boardservice` admission/actions, `pkg/ghprojects` field/option mapping, note/event formats — must be accompanied by a PR to `aenix-org/ccc` updating those two docs (and bumping the plugin version in `skills/aeman/.claude-plugin/plugin.json`). Skipping it makes `gh`-driven writes silently produce states this server would never create — e.g. cards that never appear on the daily board.
