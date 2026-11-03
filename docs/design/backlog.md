# Team backlog

The Triage board is retired. Each team has a **backlog**: one ranked list
of the tasks it has accepted and has not taken into work. Decided
2026-11-03 at the "aeman backlog" meeting (Andrei Kvapil, Vadim
Pokrovskyi); this document is the source of truth for the change and the
rules are pinned in [behavior-matrix.md](behavior-matrix.md) rows K1–K9.
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

A task — a card with no `type`, not in a Scope column, not a subtask, not
a review card — is in exactly one of two places:

- **In work**: it has a date or a sprint. It stands on the Team and Me
  boards by the day rules, unchanged.
- **In its team's backlog**: it has neither, and it is `parked: true`. It
  stands on no day board. The no-team group has a backlog like any team.

A task taken into work leaves the backlog — the backlog never shows work
in hand. Taking a task into work is giving it a day or a sprint: the
`own` gesture with a person (it lands today, in the unplanned band, in
the team's sprint), a date set from the drawer, or a drag onto the Team
board. Each of these clears `parked`, as they already do (`leaveShelf`).

There is **no third place**. The Triage strip — open tasks nobody had
dated, weeked or parked — is gone: every door that used to leave a task
there parks it instead (K3). The **week** of a task is gone: nothing
schedules a task for a week any more, and the `week` field is read and
written for Project-board deadline stubs only.

`parked` stays the stored flag rather than being derived from "no date,
no sprint", because a derived membership would put every undated card of
history into a backlog at once, and because the flag is what the
existing doors (`SetBacklog`, `own` with a team alone, the shelf drop)
already write.

## The Backlog view

`view=backlog` is a board of its own — it was a pane of Triage — and the
SPA's tabs are **Me, Team, Backlog, Roadmap, Project**.

- **Teams.** The team chips are the Team board's. Each selected team is a
  column of its own; a team that works from several backlogs (features
  reads portal's and cozystack's) selects them all.
- **What is listed.** The team's parked tasks: no planning card, no
  project card, no subtask, no review card, nothing finished. Order is
  `BacklogOrder`: hand-placed rank first, then oldest first.
- **The card.** One wide card per row: the title first; beside it a tag
  with the nearest planning card above it (goal, initiative or epic, the
  last entry of `status.lineage`); the size when it has one; the owner
  when it has one. A task whose planning parent has an end date shows it
  as its due date.
- **Order.** Dragging a card within a column reorders it: the move
  gesture with `after`/`before` anchors relative to the cards around the
  drop (rank is one board-wide list; the anchors keep the move local).
  Dragging it to another team's column hands it to that team, still
  parked.
- **Adding.** Each column has an add box at the bottom: a task created
  there is parked on that team, last.
- **Taking.** The card's owner picker (`own` with a person) takes it into
  work: it disappears from the backlog and appears on the Team and Me
  boards today.
- **Live.** The board watches every card of the teams it shows
  (`view=all&team=<set>`, the Team board's watch): a card parked,
  unparked, re-teamed, retitled, finished or reordered by anybody shows
  in every open backlog without a reload; a reorder arrives as the
  `Ordering` frame the other boards already apply.

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
MCP — nothing reads it on a board until a backlog shows load again; the
Project board's deadlines, which are weeks on deadline stubs.

## The ×

On a day board the unassign answer of the × used to put a card "back in
its week" and refused (`nowhereToLeaveIt`) when it had none. It now puts
the card back in its team's **backlog**: parked, no person, no day, no
sprint. A card can always go there, so the refusal is gone.

## Migration: schema 2 → 3

One commit per domain at start-up, idempotent, composed into one pass
like schema 2:

1. A task with a `week` and no date and no sprint is placed by where its
   week stands on the day of the bump. A week still **ahead** was the
   weekly plan — work accepted and not yet in hand, exactly the backlog —
   so the task is parked. A week that has **come** stood on the Team and Me
   boards as a debt, so the task stays in work: that week's Monday to
   Friday become its dates (`WeekAsDates`). A **finished** task only loses
   the week. In every case the `week` is removed.
2. A task with a `week` and a date or a sprint keeps them and loses the
   `week` — it is in work.
3. A task that would have stood in the Triage strip — open, no date, no
   sprint, no week, not parked — is parked.
4. Project-board deadline stubs keep their `week`; project cards and
   planning cards have none already (schema 2).

The rank of a migrated card is untouched; `BacklogOrder` puts unranked
cards after ranked ones, oldest first, so the first view of a backlog is
"what has waited longest" and the lead sorts from there.

`board.Reachable` (which the importer's cleanup deletes against) keeps
parked tasks reachable, so nothing the migration parks is lost.

## Rules pinned

| Row | Rule |
| --- | --- |
| K1 | A task is either in work (a date or a sprint) or parked in its team's backlog; never both, never neither. |
| K2 | The backlog lists a team's parked open tasks only — no planning, project, subtask, review or finished card — in `BacklogOrder`. |
| K3 | Every door that used to leave a task with no place (a create with no day, `own` with nobody, a Roadmap-born task, the × unassign) parks it in its team's backlog instead. |
| K4 | Taking a task into work (a person, a date or a sprint) unparks it; it leaves the backlog at once. |
| K5 | A drag within a backlog column moves the card by `after`/`before` anchors; a drag to another team's column re-teams it, still parked. |
| K6 | The Backlog board is live: it watches every card of its teams, applies `Ordering` frames, and drops a card the moment it is taken into work. |
| K7 | A task has no week: the API, the MCP and the codec refuse or drop `week` on a task; deadline stubs keep theirs. |
| K8 | Schema 3 parks the tasks planned for a week ahead and the strip, keeps a task whose week has come in work on that week's dates, and strips every task week, in one idempotent pass. |
| K9 | The Triage view, the `place` and `untriage` gestures and the `triage`/`triageWeek`/`due` status fields are gone from the API, the MCP and the SPA. |
