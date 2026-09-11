# The view is where the caller stands

Status: **proposed**. The rules it names are implemented; the routes are not yet.

This is the second half of what [api-redesign.md](api-redesign.md) set out to do. That one made the API a resource API — objects with a schema, LIST and WATCH, actions as explicit verbs — and it landed. What it did not finish is the sentence right under its goals: *the API surface mirrors what the user sees*. It does not, quite. A person opens a BOARD and presses something on it; an agent sends a card with fields on it and hopes the fields add up to the same thing.

## What is wrong

ADR 0002 says the API and MCP mirror the frontend, not the backend. Two audits of the live tool set found where that stopped being true, and the pattern in both is the same: **the board's vocabulary is views and gestures, and the API's vocabulary is fields.**

A person creating a card does it on a board, and the board is what makes the card what it is: typed into the Me board it is theirs, today, in this sprint; typed into a Triage week it is scheduled for that week and stands on no day; typed into the drawer it is parked; typed into a Project column it is a slot whose row is its start date. An agent creating the same four cards sends `personal`, `week`, `parked`, `epic`+`project` — four flags that encode the four boards, and nothing tells it which combinations are boards and which are states no board draws: `parked` with a `week` is a card in the drawer and in the plan at once, which the service now refuses, and `personal` with a `team` is a card in a repository whose board has no teams.

The same split runs through the × — one gesture that means four different things and is answered by `intent` — and through listing, where `view=` is one selector among fifteen, so `view=me&team=platform&zone=urgent&focus=true` reads as a database query rather than as "open my board".

The fix is not more fields. It is to put the board in the address.

## The shape

```
GET    /api/v1/views                                  the boards this caller may open
GET    /api/v1/views/{view}/cards?…                   LIST, scoped to that board
GET    /api/v1/views/{view}/watch?…                   WATCH, the same scope
GET    /api/v1/views/{view}/logs?day=&uids=           one day's feed for the cards it draws
POST   /api/v1/views/{view}/cards                     create — what a card MEANS on that board
POST   /api/v1/views/{view}/cards/{uid}/actions/{g}   a board GESTURE: remove, place, untriage, finished-earlier
```

and, unchanged, the card addressed as itself:

```
GET|PATCH|DELETE /api/v1/cards/{uid}                  the object, one uid, one canonical address
GET    /api/v1/cards/{uid}/log | /links | /notes      its subresources
POST   /api/v1/cards/{uid}/actions/{a}                pane actions: defer, in-progress, reopen, move,
                                                      send-to-review, remove-reviewer, mirror, unmirror,
                                                      remove-from-project
```

### A view is not a namespace

The path looks like a Kubernetes namespace and the resemblance stops at the shape, so it is worth being exact about the difference before anyone builds on it.

A namespaced object lives in exactly one namespace, and that namespace is part of its identity. A card is drawn on SEVERAL boards at once — today's work is on its owner's Me board, on the team's grid, in the Triage week it was scheduled for, and possibly in a Project column — and moves between them without changing what it is. `/views/me/cards` and `/views/team/cards` can return the same card, and both are telling the truth.

So the segment does not say where the card lives. **It says where the caller is standing**, which is a different fact and the one that has been missing. It is what makes a gesture mean something ("remove it from here"), what makes a create mean something ("a card of this board"), and what a listing is scoped by. A card's own identity stays where identity belongs: `/api/v1/cards/{uid}`, one address, whatever board you found it on.

That is also why PATCH and DELETE stay off the view. Patching a field is not a gesture on a board — it is the field-level door, deliberately kept (it is `kubectl patch`), and it is safe to keep because every rule it could break now lives in the service where both doors pass. Deleting is "this card was a mistake", which is true from anywhere.

### The views

| view | the board | who it lists |
| --- | --- | --- |
| `me` | the Me day board | the caller's own cards on `day` (`user=` for a lead looking at someone else's) |
| `team` | the lead's day grid | the cards of `team=` on `day`, everyone's |
| `triage` | the weeks grid | the cards of `team=` in the weeks `from`..`from+weeks` |
| `backlog` | the Triage drawer | the parked cards of `team=` — a place of its own, not a week (B11) |
| `project` | the Project board | every card filed under a column |
| `personal` | the caller's own repository | their personal board, which their read turns over (P7) |
| `all` | no board | everything the caller may read — the escape hatch, and it has to be said out loud |

`process` is not here: the Process board draws structure (processes and their tasks), which is view-less below, and the cards it shows are `project`'s.

`GET /api/v1/views` answers with the list a caller may open, each with the selectors it takes and requires — so a client (or an agent) can discover the surface instead of being told it in a description.

## What the view does to a write

Three things, in the order they bite.

**It fixes what a create means.** The view carries the defaults the board's add-box carries, and refuses the fields the board does not own:

| create in | the card that comes out | refused |
| --- | --- | --- |
| `me` | on the caller, `day` (today by default), the team's current sprint | `epic`, `parked`, `personal` |
| `team` | on `team=`, `day`, that team's current sprint; `assignee` optional (the Unassigned column) | `epic`, `parked`, `personal` |
| `triage` | scheduled for `week` — no dates, no sprint | `day`, `parked`, `personal` |
| `backlog` | on `team=`'s shelf: parked, no week, no dates, no sprint | `week`, `day`, `epic` |
| `project` | a slot under `epic` (+`project`): its row is the week of `dates.start`, no sprint | `parked`, `personal`, `team`-only fields |
| `personal` | in the caller's own repository: no team, no column, no band | `team`, `epic`, `week`, `parked` |
| `all` | exactly what the fields say, as today | nothing |

`personal` and `parked` stop being flags: each was a board wearing a field's clothes, and the board is now in the address. `noSprint` stays a field of the `me` and `team` creates, because it is a real question the board asks — the dialog that offers "this sprint" or "the next one" when a card is typed for a day ahead — and a question is not a board.

**It says which gestures are on offer.** A board gesture is answered by the board that draws it, and by no other:

| gesture | offered by | means |
| --- | --- | --- |
| `remove` | `me`, `team`, `triage`, `project`, `backlog`, `personal` | the ×, with the intents that board offers (`removal.ts` already decides this per board; the view is how it says so) |
| `place` | `triage` | give the card a week |
| `untriage` | `triage` | take the week back, into the strip |
| `finished-earlier` | `me`, `team` | work done in the sprint before this one, marked done now |

A gesture asked of a view that does not draw it is **404** — the same answer as asking for a route that is not there, because that is what it is.

**It gates the card.** A gesture through a view is refused for a card that board does not draw: **404, naming the view**. A person cannot press × on a card they cannot see, and an agent standing on the Me board should not be able to either — it has to say `view=team&team=X`, or `view=all`, and then it has said which board it is acting from. The gate is on the card as it stands BEFORE the write, never after: deferring a card off today's board is the point of deferring, and a gate on the result would forbid exactly the gestures that move work.

`all` is the escape hatch and is meant to be used by things that genuinely have no board — a migration, a sweep, an embedder. It is not a default anywhere.

## What stays view-less

The board's structure: the roster and the pointers, one per board, owned by no view.

```
/api/v1/board            /api/v1/sprints          /api/v1/people/{login}
/api/v1/teams/…          /api/v1/projects/…       /api/v1/epics/…
/api/v1/processes/…      /api/v1/processes/tasks/…  /api/v1/deadlines/…
/api/v1/me/personal      /api/v1/presence         /api/v1/healthz
```

These are the cluster-scoped half of the analogy, and it holds better here than the namespace half does: a team's sprint pointer is not a thing you see one of per board.

`GET /api/v1/ordering` is deleted rather than moved. Nothing reads it — the board's order rides the cards.

## MCP

Every card tool takes `view`, with the same values, the same defaults and the same refusals. `list_cards` keeps its selectors under it. The default stays `me`, resolved server-side, because that is where everyone works and an agent that says nothing should act on its own cards and nobody else's.

The tools the boards have and MCP does not are added in the same pass, since "what the UI can do" is the list MCP is measured against: `finished_earlier`, `untriage`, `reorder_teams`, `reorder_epics`, `delete_team`, `set_sprint_state`, `list_day_logs`, the personal trio (`link_personal`, `unlink_personal`, `get_personal`), and the sprint pointers in `get_board`. The parameter gaps go with them: `send_to_review.zone`, `create_card`'s parent and its view-shaped fields, `list_cards`'s `reviews`, `from` and `weeks`.

## The frontend

`apiProvider` becomes a factory over the view — `apiProvider(view)` — and `App.tsx` memoises one per active board, which it already tracks. Every call site keeps its shape; the path gains a prefix. The boards then stop deciding what their × means and what their add-box fills in, because the view they are already in says both.

This is also the point of the exercise for the SPA, not just for agents: `removal.ts` and the add-box defaults are the frontend's copies of rules the server now holds. They stay as the optimistic patch — the board has to draw the outcome before the answer comes back — but they stop being the only place the rule is written.

## Rules and tests

New matrix rows, one per decision that can be got wrong:

- the view fixes the create (each row of the create table is a case);
- a gesture is offered by the boards that draw it and 404s elsewhere;
- a gesture is refused for a card the view does not draw, before the write;
- `all` is never a default;
- the view-less half stays view-less.

Tests go where the rule goes: `pkg/boardservice` for the defaults and the gate (they are service rules, so MCP and HTTP get them from one place), `internal/server` for the routing and the status codes, `pkg/mcpserver` for the tool surface, and a vitest case for the provider's paths.

## What breaks

Everything that calls the old card collection, which is the SPA and MCP — both in this repository, both ported in the same change. There is no deprecation window and no `/api/v2`: the API has no other consumers, and [api-redesign.md](api-redesign.md) set that precedent when it replaced v1 in place.

A plugin writing the repositories directly is unaffected: it writes files, not HTTP.

## Open

- **Should `team` default to every team the caller can read**, or require `team=`? The grid takes a set today, and a lead with three teams opens all three.
- **`me` and `personal` are two boards on one screen** (the personal column stands beside the Me day). Two views, two requests — as it is today — or one view with a `personal=true` selector? Two reads better: they are two repositories with two rights, and the column is absent for anyone without one.
