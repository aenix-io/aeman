# Team backlog

The Triage board is retired. Each team has a **backlog**: one ranked list
of the tasks it has accepted and has not taken into work. Decided
2026-11-03 at the "aeman backlog" meeting (Andrei Kvapil, Vadim
Pokrovskyi); this document is the source of truth for the change and the
rules are pinned in [behavior-matrix.md](behavior-matrix.md) rows K1–K16.
[triage.md](triage.md) is kept for history only.

## Why

Triage answered "who does what, and in which week": a grid of weeks per
team, an inbox strip above it and a shelf beside it — three places for
work that is not being done, and a week number on every card in two of
them. The leads did not plan by weeks in practice; they kept an inbox
that never emptied and a shelf nobody ordered. What the teams need is the
Kanban one: a single list per team, ordered by hand, top is next. The
hierarchy (goal ← initiative ← epic ← task, [hierarchy.md](hierarchy.md))
already says *why* a task exists; the backlog says *when*, by position.

The change is visual first ("start with what you have"): the process does
not change with it. Leads take work from the top of the backlog; engineers
pulling from it themselves is a later step.

## Model

A task — a card with no `type`, not a subtask, not a review card — is in exactly one of two places:

- **In work**: it has a date or a sprint. It stands on the Team and Me
  boards by the day rules, unchanged.
- **In its team's backlog**: it has neither, and it is `parked: true`. It
  stands on no day board. The no-team group has a backlog like any team.

A task taken into work leaves the backlog — the backlog never shows work
in hand. Taking a task into work is giving it a day or a sprint: the
`own` gesture with a person (it lands today, in the unplanned band, in
the team's sprint) — the card's owner picker in the SPA — or, over the
API and the MCP, a date, a sprint or a defer. Each of these clears
`parked` in the same write (`takenIntoWork`).

There is **no third place**. The Triage strip — open tasks nobody had
dated, weeked or parked — is gone: every door that used to leave a task
there parks it instead (K3). The **week** of a task is gone: nothing
schedules a task for a week any more. (The `week` field lived on for
Project-board deadline stubs until the Project board was removed,
2026-11-07, `hierarchy.md`.)

`parked` stays the stored flag rather than being derived from "no date,
no sprint", because a derived membership would put every undated card of
history into a backlog at once, and because the flag is what the
existing doors (`SetBacklog`, `own` with a team alone, the shelf drop)
already write.

## The Backlog view

`view=backlog` is a board of its own — it was a pane of Triage — and the
SPA's tabs are **Me, Team, Backlog, Roadmap, Project**.

- **Teams.** The Team filter is the Team board's (the boards' one filter
  dropdown, No team among the teams, `ownership.md` O11), and so is the
  Customer filter beside it. The board is ONE list:
  the backlogs of every selected team MIXED in one order, each card
  wearing a chip of its team; a team that works from several backlogs
  (features reads portal's and cozystack's) selects them all.
- **What is listed.** The team's parked tasks: no planning card, no
  subtask, no review card, nothing finished. Order is
  `BacklogOrder` over the union — the **Roadmap's order** (K14, below),
  the TO DO tasks first (K15).
- **The card.** One wide card per row, a plain card with no coloured
  accent down its edge: the title first; on the RIGHT its parent chain —
  every goal, initiative and epic it sits under, root first (all of
  `status.lineage`), each with its kind icon as the Roadmap draws it, a
  long name cut short with the whole name in its tooltip, and each
  opening that planning card's drawer (the one the Roadmap opens); the
  due date — the server's `status.dueDate`: the nearest planning ancestor's end, when it has one; the
  size when it has one; and whose it is — the team's avatar, the
  person's stacked over it when somebody has it, one label (the shared
  owner badge, the Roadmap's too), with a separate Take button to its
  left while nobody has it.
- **Order.** Dragging a card reorders it **among its siblings** — the
  tasks under the same parent, in the same group (TO DO or not; the tasks
  with no plan item above are one run): the move gesture anchored on the
  sibling just above it (`after`), else the one just below (`before`). A
  place next to none of its siblings is shown as forbidden and writes
  nothing: the order is the tree's (K14), and the move changes the
  Roadmap's order too. A drag does not change the team: the card's team
  chip is its owner picker, and `own` with a team alone hands it to that
  team, still parked; the owner badge is that picker.
- **Adding.** No add box at the foot of the list: every gap — above the
  first card, between two, below the last — shows a round "+" at its left
  edge on hover, and an empty backlog shows one always. Pressed, it opens
  the add form right there, with a team selector when several teams are
  selected — starting on the team of the card above, else the first
  selected team; with one team there is no selector. Enter creates, Esc
  cancels. The task is parked on the chosen team and born EXACTLY there:
  the create names its place — `after` the card above, or `before` the
  card below at the top — and the server places it in the same request
  (MCP `create_card` takes the same two).
- **Taking.** **Take** is the backlog's own gesture (`take`, MCP
  `take_card`): the card's right-most button asks "Take this card into
  work?" and makes it the viewer's, on the card's own team — today, the
  unplanned band, the team's sprint — so it disappears from the backlog
  and appears on the Team and Me boards; a review nobody has taken makes
  the viewer its reviewer. A task already in work, finished work, a
  subtask, a review somebody has and a parked card somebody else is
  named on are refused. The card's avatar is the
  owner picker (`own`), which hands the card to anybody else.
- **Live.** The board watches every card of the teams it shows
  (`view=all&team=<set>`, the Team board's watch): a card parked,
  unparked, re-teamed, retitled, finished or reordered by anybody shows
  in every open backlog without a reload; a reorder arrives as the
  `Ordering` frame the other boards already apply.

## One order with the Roadmap (K14)

Decided 2026-11-07 with the owner. The backlog reads in the **Roadmap's
order**, across the whole hierarchy: goal order first, then each
initiative within its goal, each epic within its initiative, each task
within its epic. Concretely, a task's place is the PATH of places in the
board's order (rank, then id — the order every board reads) from the root
of its tree down to the task, compared step by step; the first step is the
root's level ahead of its place, because the Roadmap draws its roots level
by level (goals, then initiatives with no goal, then epics with no
initiative). Tasks with no plan item above them come after every planned
one, in their own place in the board's order. A parent's children — tasks
and plan items alike — are in board order, so a task directly under an
initiative and the epics beside it interleave as the Roadmap draws them.

There is one order, decided by the tree. Dragging in the backlog moves a
task among its siblings (see **Order** above), which is a move in the
board's order and so moves it on the Roadmap too; moving a plan item on
the Roadmap moves every task under it in the backlog, live — the backlog
listing carries `boardOrder` (the board's order of its cards and the plan
items above them), every `Ordering` frame carries the whole board's, and
the client sorts by the same key the server does (`board.BacklogKey`,
mirrored in `backlogboard.ts`). The listing's `boardOrder` is the whole
board's order — the same list an `Ordering` frame carries — so a card the
watch brings in under a plan item no listed card stands under lands right.

This replaces "hand-placed rank first, then oldest first": an unranked
card (one migrated in schema 3 and never placed) now stands where the
board's order puts it — before the ranked ones of its run, oldest first —
rather than after them.

**Reviews nobody has taken** (K10) stand above every task: the tree orders
tasks, not a review, so the "needs a reviewer" items keep the top, among
themselves in the board's order, and are dragged among themselves only.
K10's "at the top until somebody drags it" becomes "at the top, always".

## TO DO and UNASSIGNED (K15, K16)

The status badge on the Roadmap is a menu, on a task and on a plan item.

- **TO DO** (K15) marks parked work READY to be taken next: a stored flag
  on the task, `todo: true`. In the backlog the TO DO tasks come first, in
  the Roadmap's order among themselves, then the rest in the same order.
  On a task it marks the task itself, which must be waiting in its
  backlog (parked, open, not a review or a subtask); on a plan item it
  marks every such task under it at any depth. Chosen again when every
  one is marked, it clears them. The flag is **cleared when the task is
  taken into work** (any door that gives it a date, a sprint or a person)
  **or finished** — a task reopened later is not marked ready by a flag
  nobody saw. On the Roadmap a parked task's badge reads **TO DO**
  (filled) with the flag and **BACKLOG** without it — decided with the
  owner 2026-11-07: the old "To do" on a parked task said nothing about
  where it was. Work in hand reads as before (to do until its bar moves,
  in progress, the stages, done), and so do a plan item's rolled-up words
  (its "to do" is the server's "nothing under it has started"). `POST /cards/{uid}/actions/todo {todo}`, MCP `set_todo`.
- **UNASSIGNED** (K16) takes work out of hand: after "Take N tasks out of
  work? They leave Team and Me and go back to their team's backlog." every
  open task in work — the task itself, or every open task in work under
  the plan item — goes the way the ×'s unassign sends a card (K3): nobody
  on it, no day, no sprint, its unfinished review withdrawn, parked in the
  backlog of the team it stands under, **where it stands in the board's
  order** — taking work out of hand does not rearrange the plan (the ×'s
  park keeps its place too since K14). Finished work, review cards and
  subtasks are untouched. `POST /cards/{uid}/actions/unassign-work`, MCP
  `unassign_work`.

The plan item above shows both: its roll-up is read off where its open
tasks stand (H5) — IN PROGRESS while any is in work, TO DO when none is and
all are marked, BACKLOG otherwise — so UNASSIGNED on an epic's last task in
work turns the epic to BACKLOG, and TO DO on it turns it to TO DO, exactly
when the menu item would unmark them. Each gesture answers with the tasks
it changed as they now stand, which the acting tab applies (the watch does
not echo a tab's own write); the plan items above re-derive on every
watcher.

Both gestures name the tasks they act on: the client sends the uids it
counted (`uids`), because with a Roadmap filter on the reader sees fewer
tasks than the board holds, and the server acts on exactly those — each
must be one the gesture reaches from the row (422 `notATarget`). Without
`uids` (an agent asking for everything) every task the gesture reaches is
taken. Every task is checked first — reachable, and the caller's to write
— so a gesture is applied whole or not at all (403 naming how many and
which), and it lands in one commit per repository it touches. The status
badge offers the menu only where an answer reaches something: finished
work and subtasks wear a plain badge.

## What is removed

- The Triage view (`view=triage`), its board, its strip, its week grid,
  its per-person load panel, and the `from`/`weeks` selectors.
- The `place` and `untriage` gestures (HTTP actions and the MCP
  `place_card` / `untriage_card` tools).
- `week` on a task: in `CardSpec`, the create request, the card patch and
  the MCP `create_card` / `update_card` parameters. The Problem codes
  `notAMonday` and `subtaskWeek` go with it.
- `status.triage`, `status.triageWeek` and `status.due` (read by the
  Triage board only).
- The rules that read a task's week on the day boards: the week-ahead
  gate and `plannedFor`'s week branch on Team (TeamGrid / `teamgrid.ts`),
  the week gates on Me (MeView / `meview.ts`, D17).

Kept: team **capacity** (points a week) in the team file, the API and the
MCP — nothing reads it on a board until a backlog shows load again. (The
Project board's deadlines, kept here, went with that board on
2026-11-07.)

## Reviews

Decided 2026-11-03 with the owner (K10).

- **Putting a task on review always makes its review card**:
  `review: <title>`, `reviewOf` the task, the task's team. With a
  reviewer named (send-to-review) it is theirs and stands on their Me and
  Team boards, as before. Without one — the stage set to review from any
  door: the drawer, `PATCH stage`, MCP `update_card stage=review` — it is
  made with **nobody** on it and **parked in the task's team's backlog**:
  the "needs a reviewer" item. A task that already has a review card gets
  no second one.
- **Its place is the top, always** (amended with K14, 2026-11-07): the
  reviews nobody has taken are a group of their own above every task —
  the Roadmap's tree orders tasks, not a review — among themselves in the
  board's order (a new one is ranked first, so it shows first), and they
  are dragged among themselves only. It stands in the same list as the tasks, drawn as a
  review card is drawn on the Me and Team boards, with the reviewed task's
  parent chain — goal, initiative and epic only, as a task's; the drawer's breadcrumb, which ends in the task (`status.reviewed`), is the way to it — with the task's owner as the small counterpart avatar over the corner of its own, as on the Team and Me boards, and is dragged like any card.
- **Taking one** is the card's owner picker with a person — `own` on the
  review card, the one owner answer a review card takes while nobody has
  it: it gets the reviewer, today, the reviewed task's sprint and the
  unplanned band, leaves the backlog, and stands on the reviewer's Me and
  Team boards; the original shows `reviewedBy`. send-to-review on the
  original does the same.
- **Back to backlog** — the × on a review somebody has, on the Team and Me
  boards, offers it above taking the card off the board: the reviewer is
  taken off and the review waits again, nobody on it, at the top of the
  reviewed task's team backlog; the task stays on review (K12).
- **Withdrawing the review** — the task leaving the review stage, finished
  or deleted — takes the card away, as it takes an untouched assigned one.
  The × on a review card is that withdrawal — the Backlog's on one nobody
  has taken, the Team and Me boards' on one a reviewer has (beside "Back to
  backlog"): after "Withdraw the review? «task» goes back to In progress."
  the task goes back to In progress and the card goes. No door leaves a
  task on review with no review card: a plain delete of a review card is
  the same withdrawal, and removing the reviewer hands the review back to
  the backlog.
- **K1**: a review nobody has taken is parked (it is in the backlog); one
  with a reviewer follows the card it reviews, as before.
- **Schema 3** gives every open task already on review with no review card
  this card, in the same bump (a fresh ULID, created at the bump, ranked
  at the top of the board's order — the oldest task's first when there are
  several).

## The ×

On a day board the unassign answer of the × used to put a card "back in
its week" and refused (`nowhereToLeaveIt`) when it had none. It now puts
the card back in its team's **backlog**: parked, no person, no day, no
sprint. A card can always go there, so the refusal is gone. It keeps its
place in the board's order (since K14: it used to be moved to the top, and
the backlog reads in the Roadmap's order, so the move rearranged the
plan).

## Migration: schema 2 → 3

One commit per domain at start-up, idempotent, composed into one pass
like schema 2:

1. A task with a `week` and no date and no sprint is placed by where its
   week stands on the day of the bump. A week still **ahead** was the
   weekly plan — work accepted and not yet in hand, exactly the backlog —
   so the task is parked. A week that has **come** stood on the Team and Me
   boards as a debt, so the task stays in work: that week's Monday to
   Friday become its dates (`WeekAsDates`) when it has none, and with no
   sprint it joins its team's current sprint — the week is what kept it on
   its owner's Me board, and an open card of a past day with no sprint is
   on no Me board. A **finished** task only loses the week. In every case
   the `week` is removed.
2. A task with a `week` and a date or a sprint keeps them and loses the
   `week` — it is in work.
3. A task that would have stood in the Triage strip — open, no date, no
   sprint, no week, not parked — is parked.
4. Project-board deadline stubs keep their `week`; project cards and
   planning cards have none already (schema 2). Since 2026-11-07 the
   bump skips project cards and `projects/` altogether.

The rank of a migrated card is untouched. At the bump `BacklogOrder` put
unranked cards after ranked ones, oldest first; since K14 the backlog
reads in the Roadmap's order, and an unranked card stands where the
board's order puts it (before the ranked ones of its run, oldest first).

`board.Reachable` (which the importer's cleanup deletes against) keeps
parked tasks reachable, so nothing the migration parks is lost.

## Rules pinned

| Row | Rule |
| --- | --- |
| K1 | A task is either in work (a date or a sprint) or parked in its team's backlog — the team it stands under, its own else its plan item's (O13); never both, never neither. |
| K2 | The backlog lists the parked open tasks standing under a team (its effective team, O13) only — no planning, project, subtask, review or finished card — in `BacklogOrder`. |
| K3 | Every door that used to leave a task with no place (a create with no day, `own` with nobody, a Roadmap-born task, the × unassign) parks it in its team's backlog instead. |
| K4 | Taking a task into work (a person, a date or a sprint) unparks it; it leaves the backlog at once. |
| K5 | A drag in the backlog moves the card by `after`/`before` anchors of its neighbours in the merged list; handing it to another team is its team chip (`own` with a team alone), and it stays parked. |
| K6 | The Backlog board is live: it watches every card of its teams, applies `Ordering` frames, and drops a card the moment it is taken into work. |
| K7 | A task has no week: the API, the MCP and the codec refuse or drop `week` on a task; deadline stubs keep theirs. |
| K8 | Schema 3 parks the tasks planned for a week ahead and the strip, keeps a task whose week has come in work on that week's dates and its team's current sprint, and strips every task week, in one idempotent pass. |
| K9 | The Triage view, the `place` and `untriage` gestures and the `triage`/`triageWeek`/`due` status fields are gone from the API, the MCP and the SPA. |
| K10 | Putting a task on review always makes its review card; without a reviewer it is made with nobody on it, parked in the team's backlog — always above every task, a group of its own dragged only among its kind (K14) — and a person picked there takes it into work. |
| K11 | Take is the backlog's gesture: the card becomes the actor's, on its own team, and leaves the backlog; refused where there is nothing to take. |
| K12 | The × on a review somebody has offers "back to backlog": the reviewer is taken off and the review waits, nobody on it, at the top of the reviewed task's team backlog; the task stays on review. |
| K13 | The insert "+" in a gap of the list creates a task parked on the chosen team and placed there by the create itself (`after`/`before`), in one request. |
| K14 | The backlog reads in the Roadmap's order — the path of board-order places from the root plan item down; loose tasks after the planned ones; reviews nobody has taken above all — and a drag moves a task among its siblings only, which moves it on the Roadmap too. |
| K15 | TO DO marks a parked task (or every one under a plan item) ready: it stands first in its backlog; the flag clears when the task is taken into work or finished. |
| K16 | UNASSIGNED takes an open task in work (or every one under a plan item) back to its backlog, the ×'s unassign way, after a confirm that counts them; finished work is untouched. |
