# aeman API v2 — Kubernetes-style board API

Status: **draft, awaiting review**. Nothing here is implemented yet.

## Goals

- Redesign the API from scratch, modelled on Kubernetes: cards are first-class
  objects with a full schema, collections support LIST + WATCH (WebSocket), and
  imperative verbs are explicit *actions* on resources.
- **Every rule that lives in the frontend today moves to the backend.** The
  frontend (and any MCP agent) sends *intent*; the backend decides the outcome.
  One canonical implementation of clamps, cascades, visibility and sprint logic.
- The API surface mirrors **what the user sees**: views (Team day grid, Me day,
  Weekly plan) are server-computed queries, not client-side filters.
- MCP is redesigned alongside and stays a 1:1 projection of the API.
- The storage backend stays an interface (GitHub Projects v2 today, a git
  backend possible later).

## Non-goals

- Multi-cluster/board federation, RBAC beyond the existing token model.
- Keeping v1 request/response shapes. v1 is removed once the frontend and MCP
  are ported (no external consumers today besides them).
- A real resourceVersion-based watch resume (GitHub gives us no versions; see
  Watch).

## Resource model

Everything lives under a board scope:

```
/api/v2/boards/{owner}/{project}/...
```

`--lock-board` pins `{owner}/{project}` as today. Four resource kinds:

| Kind | What it is | Mutability |
| --- | --- | --- |
| `Board` | Singleton: field metadata, team roster, sprint pointers summary | read-only |
| `Card` | A project item with every field the UI renders | CRUD + actions |
| `Sprint` | A team's sprint pointer (current/previous), name = team | read + actions |
| `Note` | A work note (issue comment / draft-log line), subresource of Card | CRUD |

### Card

```json
{
  "kind": "Card",
  "metadata": {
    "uid": "PVTI_…",
    "contentId": "DI_…",
    "isDraft": true,
    "url": "https://github.com/…",
    "number": 12,
    "repository": "acme/repo",
    "author": "octocat",
    "createdAt": "2026-06-20T10:00:00Z"
  },
  "spec": {
    "title": "Wire up the API",
    "description": "Free-form details",
    "team": "platform",
    "zone": "red",
    "assignee": "octocat",
    "progress": 40,
    "stage": "review",
    "dates": { "start": "2026-07-01", "end": "2026-07-03", "sprint": "2026-07-01" },
    "plan": { "band": "wed", "week": "2026-06-29" },
    "reviewOf": "PVTI_…"
  },
  "status": {
    "complete": false,
    "inProgress": false,
    "rank": "0|c00010:",
    "reviewedBy": "lllamnyp"
  }
}
```

- `metadata` — identity and immutable facts.
- `spec` — user intent. Everything the user can edit.
- `status` — **derived** by the server, never written by clients:
  - `complete` — done stage, or 100% with no/recurrent stage (the one rule,
    owned by the server).
  - `inProgress` — the implicit status (no stage, 10–90%).
  - `rank` — global manual ordering key (see Ordering).
  - `reviewedBy` — the linked review card's assignee, resolved server-side (the
    UI shows "On review: X" without scanning the board).

### Sprint

```json
{ "kind": "Sprint", "metadata": { "team": "platform" },
  "spec": { "current": "2026-07-02", "previous": "2026-07-01" } }
```

### Note

```json
{ "kind": "Note", "metadata": { "id": "IC_… | PVTI_…:3", "cardUid": "PVTI_…",
    "author": "octocat", "createdAt": "…", "source": "comment|draft" },
  "spec": { "text": "Deployed to staging" } }
```

Notes are their own collection under a card: reading, filtering and the
draft-log parsing all happen server-side. Card objects do **not** embed notes
(they embed a `notesCount` in status if the UI needs a badge — TBD).

## Verbs

### Declarative (spec edits)

| Verb | Path | Notes |
| --- | --- | --- |
| `GET` | `/cards`, `/cards/{uid}`, `/sprints`, `/board`, `/cards/{uid}/notes` | LIST supports selectors (below) |
| `POST` | `/cards` | create; admission fills defaults (dates, sprint join, first-sprint record) |
| `PATCH` | `/cards/{uid}` | partial `spec` update — **admission applies every rule** |
| `DELETE` | `/cards/{uid}` | real delete, cascades the linked review card |
| `POST/PATCH/DELETE` | `/cards/{uid}/notes[/{id}]` | note CRUD |

`PATCH /cards/{uid}` is the single write path for field edits. The server runs
the same admission chain regardless of who calls (UI, MCP, curl):

- progress: clamp for review/locked (10–90); dropping a legacy done below full
  clears it; a review card's progress drives the original's review stage
  (server-side `syncOriginalReview` — the UI no longer does this).
- stage: done is derived (clears stage + fills 100); review/locked knock a full
  card to 90; leaving review cancels the linked review card (demote-or-delete);
  recurrent is unclamped.
- dates: the calendar semantics (`start` sets the sprint via
  `activeSprint(team, start)`, `end` bounds the visible range).
- team: joins the team's current sprint.

The response is always the resulting Card — the client reconciles its
optimistic state from it.

### Actions (imperative subresources)

Everything that is not a plain field edit is an explicit action. The backend
owns the whole outcome; the frontend fires one request.

| Action | Path | Semantics (all server-side) |
| --- | --- | --- |
| remove | `POST /cards/{uid}/actions/remove` | The × button: demote to the previous sprint while the card is in the current one, else real delete; plan cards unassign/demote per the weekly rules. One method — the backend decides. |
| move | `POST /cards/{uid}/actions/move` `{after: uid \| first: true}` | Reorder (see Ordering) |
| defer | `POST /cards/{uid}/actions/defer` `{days: 1\|7}` | +1 day / +1 week: from `max(today, start)`; a same-day (0d) card relocates fully |
| send-to-review | `POST /cards/{uid}/actions/send-to-review` `{reviewer, day?}` | creates the linked review card, puts the original on review |
| remove-reviewer | `POST /cards/{uid}/actions/remove-reviewer` | deletes the linked review card |
| take-into-plan | `POST /cards/{uid}/actions/take-into-plan` `{engineer, zone, day?}` | plan card → grid |
| release-from-plan | `POST /cards/{uid}/actions/release-from-plan` | grid → plan only |
| carry-over | `POST /sprints/{team}/actions/carry-over` | closing sprint's unfinished cards move, finished recurrent reseeds |
| carry-week | `POST /sprints/{team}/actions/carry-week` `{week?}` | weekly analogue, same-title dedup |

Action responses return the affected card(s) so callers need no follow-up GET.

## Views: LIST with selectors

The frontend renders three views; v2 makes them server-side queries so the
visibility rules (sprint days, ranges, deferred windows, passed-through
history) have exactly one implementation:

```
GET /cards?view=team&team=cozystack&day=2026-07-02
GET /cards?view=me&user=kvaps&day=2026-07-02
GET /cards?view=weekly&team=portal&week=2026-06-29
GET /cards                      # unfiltered: every card (MCP, debugging)
GET /cards?stage=review&assignee=kvaps   # plain field selectors compose
```

- LIST responses are **sorted by `status.rank`** — the client renders in order,
  grouping (zones × people) stays presentational.
- The weekly view response carries the computed band split and the plan
  progress number (recurrent excluded), so the progress bar is served, not
  computed client-side.

## Watch

```
GET /api/v2/boards/{o}/{p}/watch?resources=cards,sprints&view=team&team=x&day=…&client=<id>
```

WebSocket; each frame is one event:

```json
{ "type": "ADDED" | "MODIFIED" | "DELETED", "object": { …Card or Sprint… } }
```

- **Selector-scoped**: the server evaluates view membership per event. A card
  that stops matching the subscribed view (deferred away, carried, demoted) is
  delivered as `DELETED` *for that subscription*; one that starts matching
  arrives as `ADDED`. This is the k8s field-selector watch semantics and it is
  what lets the frontend stop filtering entirely: it renders exactly the
  objects in its subscription.
- An unscoped watch (`resources=cards`, no view) streams everything —
  the current informer behaviour, kept for agents and debugging.
- `client=<id>` keeps the existing echo suppression (own changes are not echoed;
  the mutation response is the reconciliation point).
- No resourceVersion resume (GitHub has no versions): on reconnect the client
  re-LISTs, as today. The store keeps a monotonic revision internally; exposing
  resume tokens is a possible later step.
- Day rollover: view membership depends on "today", so at local midnight the
  server re-evaluates subscriptions and emits the membership deltas.

## Ordering and reordering (the open question)

GitHub Projects has one global manual order; k8s has none. v2 models it as a
**server-synthesized rank** on each card:

- On every full board load the store assigns each card a lexicographic rank
  from its position in GitHub's item order (`status.rank`).
- `actions/move {after}` computes a midpoint rank between the neighbours,
  updates the one card locally, and mirrors the move to GitHub
  (`updateProjectV2ItemPosition`). **Exactly one MODIFIED event per move** — no
  event storms, clients just re-sort by rank.
- Rank space exhaustion between two adjacent ranks triggers a local rebalance
  of the affected segment (bounded burst of MODIFIED events; rare).
- Ranks are an in-memory synthesis — GitHub order remains the source of truth
  and every re-list re-derives them.

Rejected alternative: a single `Ordering` resource holding the uid list. It
concentrates churn in one hot object every client must re-read on any move, and
breaks the "cards are self-contained objects" property.

## What moves out of the frontend

The complete inventory (today ~40 handlers in `TeamBoard`/`MeBoard`):

| Frontend today | v2 home |
| --- | --- |
| progress clamps, done auto-link, in-progress nudges | PATCH admission |
| stage side-effects (fill 100, knock to 90, cancel linked review) | PATCH admission |
| `syncOriginalReview` (review progress ↔ original stage) | PATCH admission (server-ordered, race-free) |
| demote-then-delete, plan-card removal rules | `actions/remove` |
| defer (+1d/+1w, 0d full move) | `actions/defer` |
| calendar set-dates (sprint join via activeSprint) | PATCH admission |
| carry over / carry week + recurrent reseed + confirm counts | `actions/carry-over`/`carry-week` (dry-run flag returns the counts for the confirm dialog) |
| create defaults (dates, sprint join, first sprint, plan cards) | POST admission |
| view filters (team day rules, me rules, weekly bands) | LIST/watch selectors |
| plan progress bar | weekly view response |
| review-of resolution ("On review: X") | `status.reviewedBy` |
| draft-body note parsing | Notes subresource |
| drag reorder afterId computation | `actions/move` (client still picks the drop target) |

The frontend keeps: rendering, grouping, drag targets, optimistic application
of its own intent (reconciled by mutation responses and watch events).

## MCP v2

Tools become a 1:1 projection of the API, same names as the actions:

```
get_board · list_cards(view/team/day/user/week + field selectors) · get_card
create_card · update_card(patch) · delete_card · remove_card · move_card
defer_card · send_to_review · remove_reviewer · take_into_plan
release_from_plan · carry_over · carry_week
list_notes · add_note · edit_note · delete_note
```

`update_card` takes the same spec patch as HTTP PATCH; every admission rule
applies identically. The MCP server keeps stdio + `/mcp` transports and the
lock-board behaviour.

## Backend interface

The storage adapter narrows to primitives — all policy lives above it:

```go
type Backend interface {
    LoadBoard(ctx, owner, project) (Board, error)      // full snapshot, ordered
    LoadCards(ctx, board, uids) ([]Card, error)        // partial re-read
    CreateCard(ctx, board, CreateInput) (Card, error)
    DeleteCard / MoveCard / Rename / SetField…         // as today
    Notes: List/Add/Edit/Delete
    SetSprintState(ctx, board, team, current, previous)
}
```

This is essentially today's `boardservice.Backend`; v2 keeps it so a git
backend can implement the same surface later. The k8s-style service (admission,
views, rank synthesis, watch hub) is backend-agnostic and lives in one place
(`internal/apiserver`, evolving today's `boardservice` + `boardstore`).

## Migration plan

1. **Service core**: admission chain + actions on the existing
   store/backend; unit tests port from `boardservice` (most logic already
   lives server-side after the recent work — this consolidates the rest).
2. **HTTP v2** (`/api/v2/...`) + selector-scoped watch + rank synthesis.
3. **Frontend port**: provider v2, views from LIST/watch subscriptions, delete
   local rule code (~40 handlers shrink to intent + optimistic apply).
4. **MCP v2**: new tool set on the same service.
5. **Remove v1** (HTTP routes + old MCP tools) once the frontend and the aeman
   skills are on v2.

Each phase ships behind the existing live-update infrastructure (store, echo
suppression, WS hub) — no regression window for current users.

## Open questions

1. **spec/status split vs flat card.** This doc proposes the split (clear
   derived-vs-intent boundary, k8s-idiomatic). A flat object is friendlier to
   quick agent edits. Decide before phase 1.
2. **Confirm dialogs**: carry-over/carry-week/remove need counts before acting.
   Proposal: `?dryRun=true` on actions returns the would-be result (k8s has
   dry-run semantics) — the UI confirms, then re-fires for real.
3. **notesCount in Card.status** — cheap badge vs keeping notes fully separate.
4. **Watch multiplexing**: one socket per subscription (simple, proposed) vs
   one socket with subscribe/unsubscribe frames (fewer connections; more
   protocol). Proposed: per-subscription sockets first.
