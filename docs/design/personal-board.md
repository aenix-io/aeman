# Personal board — private tasks beside the team board

Status: design + implementation, 2026-07-22. Author: tym83 (+Claude).

## What

A user attaches their **own GitHub Project** (a private project in their own
account) as a *personal board*. Personal cards then appear:

- **Me view** — two panes side by side: **Work** (the cards MeView already
  shows) and **Personal** (the same MeView filter run over the personal
  board). On narrow screens (≤820px) one pane is shown at a time with ‹ ›
  arrows to switch.
- **Team view** — a virtual **Personal** chip in the team filter. Selecting it
  repoints the whole Team view (grid, sprint, carry-over, weekly plan) at the
  personal board — so personal tasks get real sprints and plans, not a
  read-only list.

Nobody else can see any of it.

## Why this shape

Three candidate designs were considered:

1. **Pseudo-team "personal" inside the work project** — rejected. A team is a
   partition of one project: it would collide with sprint-per-team state, the
   roster union, carry-over and rename cascades — and it is not private (same
   project, same readers).
2. **Server-side federation** (one merged board over two projects) — rejected.
   The store, watch stream, selectors and board service are all keyed by one
   `(owner, project)`; teaching them sets of projects is a deep change with no
   payoff beyond what the client can do itself.
3. **Client-side federation, two instances** — chosen. The server already
   serves any number of boards; every REST/WS call carries `owner`+`project`.
   The SPA runs the board machinery TWICE (`useBoardData`, extracted from
   App): the work board and the personal board each own their state, their
   scoped watch socket and their server address. No merged card list, no
   per-card provenance tags — every component instance is wired to exactly
   one board, so every mutation, note and log call lands on the right
   project by construction. Server: zero changes.

## Privacy model (why zero server changes are needed)

- In self-hosted (OAuth) mode every GitHub call runs with the **requesting
  user's own token** (`internal/server/server.go:tokenForRequest`); in local
  mode the only user is the `gh` login.
- The board cache is gated per login: an entry is served only to logins whose
  own token has successfully loaded that board
  (`boardstore.go: boardEntry.authed / cached`). A private project in the
  user's account fails to load under anyone else's token, so no cache entry is
  ever visible to them. This is the same mechanism that already protects
  private team boards; a personal project inherits it unchanged.
- The *pointer* to the personal board (its project number) lives client-side
  in localStorage, keyed by login — it never reaches other users.

The recommended personal project is a **private Project v2 in the user's own
account** (`gh project create --owner @me --title "aeman personal"`). aeman
provisions its fields lazily on first write, as with any board.

## UX

### Setup

"Personal board" entry in the board toolbar (visible when signed in and not
impersonating): a small dialog asking for the project number (owner is fixed
to the user's login), with a hint on creating a private project. Stored as
`aeman.personal.<login>` = `{owner, number}`. Removing the pointer detaches
the board (cards vanish from the merged views; the project is untouched).

### Me view

- Desktop: `.me-left` becomes two panes — **Work** and **Personal** — each its
  own zone-band stack (the existing four bands). The pane split only exists
  when a personal board is attached; otherwise the view is exactly as today.
- Narrow (≤820px): one pane at a time; a ‹ Work/Personal › switcher bar
  above the board flips panes (arrows clamp, no wrap). Session-scoped state.
- The notes panel stays with the Work pane (one day-log, as today). A
  personal card's notes and activity live in its card detail, which loads
  the log from the personal project.
- "View as" shows the *viewed* user's work cards only — a personal pane is
  never rendered for someone else (their pointer and their project are both
  unreachable, by design).

### Team view

- A virtual **Personal** chip appears after the real team chips (only for the
  owner, only when a personal board is attached). It is not a team on any
  project: selecting it swaps the Team view's data source to the personal
  board with the *no-team* group — people×zones grid (one person: you), its
  own sprint state, its own carry-over, its own weekly plan. All existing
  team-mode machinery works because it is simply pointed at another board.
- The chip does not appear in TeamsModal (it is not part of the work board's
  roster), cannot be renamed, and is never written to any card.

### Cards

Personal cards render identically (zones, progress, stage, age). Team
assignment UI is hidden for personal cards (a personal board has no teams).
Cross-board moves are out of scope (phase 2: "move to work board" as
copy+close).

## Implementation map (frontend only, as landed)

- `web/src/personal.ts` — NEW: the per-login pointer (read/save/clear), the
  work-board guard (`samePointer`), the narrow-mode pane switcher transitions.
  Unit-tested (`personal.test.ts`).
- `web/src/useBoardData.ts` — NEW: the board machinery extracted verbatim from
  App (state, card mutators, lazy per-view fetch, scoped watch socket,
  presence, queue badge) so it can be instantiated per board.
- `web/src/App.tsx` — two `useBoardData` instances; the Me view renders two
  `MeBoard`s (work + embedded personal) inside `.me-split`; the Team view
  swaps the entire `TeamBoard` props set to the personal instance when the
  virtual chip is active; `PersonalDialog` attach/detach; a personal-board
  failure lands in its own dismissible warning, never the work board's error.
- `web/src/components/MeBoard.tsx` — `embedded` prop: no toolbar / notes
  panel / MCP link, no per-card log prefetch (card detail loads its own log).
- `web/src/components/TeamBoard.tsx`, `TeamChips.tsx` — `personalChip` /
  `extraChip`: a virtual, non-renamable, non-removable chip; in personal mode
  the roster is empty, the filter is pinned to the no-team group and the
  manage dialog is hidden.
- `web/src/components/PersonalDialog.tsx` — NEW: attach/detach, owner pinned
  to the signed-in login, refuses the work board itself.
- `web/src/components/CardDetail.tsx` — unchanged; the App picks which
  board/instance backs the open card.
- `web/src/providers/*` — unchanged.
- Docs: this file; `docs/api.md` untouched (no API change);
  `docs/design/behavior-matrix.md` gets rows for pane visibility and the
  virtual chip; `docs/dates.md` untouched (no date-rule change — the same
  MeView/TeamGrid filters run per board).
- Go/server/MCP: **no changes**. MCP agents can already target the personal
  board by passing its owner/project to the existing tools; a symbolic
  `@me` board alias is a possible follow-up, not part of this change.

## Edge cases

- **uid collisions**: not possible by construction — the two boards' cards
  never share a list or a component instance.
- **lock-board mode**: the lock pins the *work* board picker; the personal
  attachment is orthogonal and stays available (it cannot change the pinned
  board).
- **Personal board load failure** (deleted project, revoked scope): the work
  board must render as if no personal board were attached, plus a dismissible
  warning; a broken personal pointer must never take down the Me view.
- **Same project attached as both work and personal**: rejected in the setup
  dialog (identical `owner/number`).
- **Watch reconnects** are independent per socket; a personal-stream outage
  does not stall work-board liveness.

## Tests

- `web/src/personal.test.ts` — pointer persistence per login, tag/route
  helpers, merge with provenance, pane-state transitions (vitest).
- Component-level behaviours covered through the pure helpers they delegate to
  (the repo's existing pattern: logic in `*.ts`, thin components).
- Behaviour rows in `docs/design/behavior-matrix.md`.
