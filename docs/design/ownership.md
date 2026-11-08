# Owner, team and customers on the plan

Who is answerable for a goal, an initiative or an epic, which team carries
it out, and which customers the work is for. Decided 2026-11-06 with Andrei
Kvapil; this document is the source of truth for the change and the rules
are pinned in [behavior-matrix.md](behavior-matrix.md) rows O1–O13. It
amends H3 of [hierarchy.md](hierarchy.md) (a planning card had no team and
no owner) and left H4 (a project card has neither) as it was — project
cards themselves were removed on 2026-11-07 (`hierarchy.md`), and the
project-card clauses below are kept as history.

## Why

The Roadmap shows what the company plans, but not who is answerable for each
part of the plan, which team is doing it, or which customer it is for. Leads
want to see "the epics I own and have to break down", "what cozystack is
doing", "everything for Hidora" — and the planning cards cannot say any of it.

Two different kinds of answerability are involved, and mixing them is what
makes a single "assignee" on a planning card wrong:

- **Who carries it out** is a TEAM. A goal does not get done by one person,
  and an epic's executors are the people on its tasks.
- **Who answers for the plan item itself** — breaks it down, triages what
  lands under it, keeps its status honest — is ONE PERSON. That person is not
  doing every task under it, so the item must not land on their day boards.

## Model

### Owner (planning cards only)

A goal, an initiative or an epic has an optional **owner**: one person.

- The owner answers for the card itself. It is **not inherited**: each level
  has its own, and an initiative with no owner has none. (The Roadmap may
  hint "goal owner: X" in grey on a card with none; that hint is not the
  card's owner and no filter matches it.)
- An owner puts nothing on any day board: planning cards stay off Me, Team
  and Backlog (H3), and an owner does not inherit the tasks below.
- Tasks keep their **assignees** as today; "owner" on a planning card and
  "assignee" on a task are different fields with different meanings.

### Team (planning cards and tasks)

A planning card has an optional **team**: the team that carries it out.

- The card's **effective team** is its own team, or else the nearest
  ancestor's that has one. It is computed, never written down the tree.
- A task's own team always wins over an inherited one. An epic of `portal`
  can hold tasks of `cozystack`: those tasks simply carry their own team.
- A task with no team of its own (one born on the Roadmap, say) takes the
  effective team of its planning ancestors for filtering and display, and
  — amended 2026-11-06 (O13) — **stands under it on every day board and
  backlog**: the Team board draws it in that team's grid, the Backlog lists
  it in that team's backlog, every team filter and `team` selector counts it
  that team's, and wherever a team's sprint is resolved for it (taken into
  work, dated, made under the plan item, carried over) it is that team's
  sprint. It is still inherited, not written: change the plan item's team
  and the task moves with it. A review card stands under its task's team.
  The no-team group is the cards whose effective team is empty.
  When the task's placed team changes without a write to it — its plan
  item takes another team, it is filed under another plan item, or out
  from under one — a task in work moves into the new team's current
  sprint in the same action. "No owner" on a task under an epic takes its
  own team off and leaves it under the epic's team.
- What stays the card's OWN team: storage (the file's `team:`), the domain
  rule (which repository a card lives in, `DomainOf`), and every write —
  PATCH, `own`, the owner picker, the "+" of a team's row or backlog. The
  team a reader cannot read an ancestor of is not inherited for them: their
  board stops the chain there.
- A planning card still lives in the primary repository (H3): its team
  places nothing.

### Customers (planning cards and tasks)

The board has a **customer roster**, like its teams and projects: add,
rename, reorder, delete. A goal, an initiative, an epic, a task or a subtask
may name **several customers**.

- A card's **effective customers** are its own plus every ancestor's (the
  union): a lower card is added to, never overwritten.
- A review card shows the customers of the task it reviews.
- Project cards had no customers (H4; both removed 2026-11-07).
- Deleting a customer from the roster removes it from every card that names
  it; renaming changes the name everywhere at once (cards refer to the
  customer by id, not by name).
- A card can still come to name an id the roster no longer holds: an edit
  queued before a delete lands after it, or a rebase replays a write the
  other side's delete came before. Such an id names nobody: it is left out
  of the card's customers and effective customers on every read, no filter
  matches it, and the next edit of the card's customers drops it. Only an
  id NEW to a card must be on the roster, so a dangling one never locks the
  card. This is why a delete does not have to wait for the card writes
  queued before it.

## Roadmap filters

The Roadmap has four filters, applied by the server (`view=roadmap`
selectors `team`, `customer`, `owner`, `status`; the same in MCP
`list_cards`):

- **Team** — a task matches when its effective team is one of those chosen; a
  planning card matches when it does itself or any task under it does. So an
  epic of `portal` with some `cozystack` tasks shows under the `cozystack`
  filter, with those tasks only. **No team** is an option like any team: it
  matches what has no effective team, and choosing only real teams leaves
  that work out (O11).
- **Customer** — the same rule over effective customers.
- **Owner** — the Roadmap's one person filter: a planning card matches by
  its own owner (no inheritance, no hint), and a task or a subtask by its
  assignees; a planning card also passes by a task under it that matches.
  It replaced the Roadmap's earlier client-side assignee filter, so the
  toolbar asks "whose" once. Its options are the board's members, the plan
  items' owners, the tasks' assignees and whoever is already selected.
- **Status** (added 2026-11-08, O14) — In progress, Backlog, Locked, No
  items, Done (`in-progress`, `backlog`, `locked`, `no-items`, `done`). A
  task's work status is `done` when it is complete, `backlog` when it is
  parked, `in-progress` when it is in work — a date or a sprint, whatever
  its stage (review, locked and refused are in somebody's hands); a task
  nobody has placed yet reads as `backlog`. A task is also **Locked** while
  a dependency holds it (`status.locked`, its own or an ancestor's — not
  the manual stage) and it is not done. A subtask goes by its parent task,
  a review card by the task it reviews — Locked included: a subtask that
  waits for something itself shows LOCKED on its badge, but the Locked
  filter goes by its parent, and leaves it out while the parent waits for
  nothing. A planning card is **Locked** by
  itself while a dependency holds it and its tasks are not all done, and
  **No items** by itself when nothing is under it; it matches In progress,
  Backlog or Done when a task under it does. "In progress" shows the work
  in hand with the plan items above it. **Hide done** — one press on the
  toolbar — is every value but Done, so it hides exactly the finished work
  and keeps everything else: an epic with no tasks yet, one waiting for
  another, open work (the owner asked for "hide done" in so many words).
  Until Locked and No items were added (amended 2026-11-08) a plan item
  matched nothing by itself, Hide done was In progress + Backlog, and it
  hid every empty epic — the client epics in Operations that wait for the
  product epics among them. A Hide done remembered from then reads back as
  the new one.

Several filters are OR within one and AND across them — judged on ONE task
(amended 2026-11-08): a planning card passes by a task under it that passes
every filter chosen, the planning card's own team, customers or owner
standing in for that filter where they apply; the work status is always the
task's. With no status chosen, or with Locked or No items chosen and the
planning card answering it itself, a planning card whose own fields pass
every filter is listed with no task. An epic is never drawn on a team taken from
one task and a status (or a customer) taken from another.

The Team board and the Backlog have the same Team and Customer filters
(O11); there they narrow the day board's cards, a customer matching by
effective customers as here. The Me board has the Team filter alone (O12).

Several values within one filter are OR; different filters are AND. Every
matching card is drawn with its chain of ancestors above it, so it is clear
where it sits. The choice is remembered per viewer in the browser.

## Setting them

- **The drawer.** A planning card's drawer has an Owner / Team picker (a
  person needs no team here, unlike on a task) and a Customers chip field.
  A task's drawer has the Customers chip field; inherited customers and an
  inherited team are shown in grey with where they come from, and cannot be
  removed there.
- **`own`** (H10) on a planning card sets its owner and/or team instead of
  being refused; on a task it is unchanged. A person alone keeps the card's
  team and a team alone keeps its owner; nobody (neither) takes both off.
  One half comes off alone through the update (`team` or `owner` empty),
  and that is what the owner picker writes on a plan item.
- **API / MCP.** `CardSpec` gains `owner` (planning cards) and `customers`
  (ids); `status` gains the computed `effectiveTeam` and
  `effectiveCustomers`. The customer roster has its own routes and MCP tools
  (`add_customer`, `rename_customer`, `delete_customer`,
  `reorder_customers`), the way teams and projects do (ADR 0002).

## Storage

- A planning card's file may carry `team:` and `owner:` (a login); any card
  may carry `customers:` (a list of customer ids).
- The roster: `customers/<ulid>.yaml` with `name:` and `rank:`, in the
  primary repository.
- No migration: existing planning cards simply have no team, owner or
  customers. The schema stays at 3; readers that do not know the keys keep
  them as unknown keys (the codec round-trips them).
- One existing state changes meaning: a `team:` a planning card's file
  already carries — written by a direct writer, or left from before H3
  read it off — was ignored and now takes effect. That planning card shows
  the team, passes the Roadmap's team filter, and the tasks under it with
  no team of their own take it as their effective team. Nothing moves on a
  day board, since a planning card's team places nothing. A board with
  such files should be checked before the release; this change does not
  rewrite them.
- `git-backend.md` and `plugin-impact.md` describe the new keys and the
  roster for external writers.

## Rules to pin

| Row | Rule |
| --- | --- |
| O1 | A planning card may have one owner (a person) and one team. |
| O2 | An owner is never inherited and puts nothing on any day board. |
| O3 | A card's effective team is its own, else the nearest ancestor's (a review card's, its task's); a task's own team always wins. |
| O4 | A card's effective customers are its own plus all its ancestors'; a lower card is added to, never overwritten. |
| O5 | Customers come from the board's roster, are referred to by id, and a rename or a delete reaches every card. |
| O6 | A review card shows its task's customers (project cards, which carried none, were removed 2026-11-07). |
| O7 | Roadmap team/customer filters match a task by its effective value and a planning card by itself or any task under it; matches are drawn with their ancestors. |
| O8 | The Roadmap owner filter is its one person filter: a planning card by its own owner, a task or subtask by its assignees. |
| O9 | `own` on a planning card sets its owner and/or team; the same fields and the customer roster are in the API and MCP. |
| O10 | Deleting a customer needs the right to write every repository it reaches. |
| O11 | The no-team group is a team in every team filter, and the day boards filter by customer too. |
| O12 | Every board filters through one dropdown, with the roster's Manage inside; the Me board says what its filters hide. |
| O13 | A task stands under its effective team on every day board, backlog, team filter and sprint; its own team is still what is stored and written. |
| O14 | The Roadmap status filter matches a task by its work status (done, backlog when parked, in progress when in work; unplaced reads as backlog), a subtask by its parent task, a review card by its task, and a planning card by itself for Locked and No items, otherwise by a task under it. Across filters the AND is judged on one task. |
