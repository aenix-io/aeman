# Hierarchy: Goal → Initiative → Epic → Task → Subtask

The day boards answer "what are these people working on". Nothing above
them answered "why", and planning above the sprint happened in meetings
and in people's heads. This adds the levels above the task — a **goal**,
an **initiative**, an **epic** — as cards on the same board, linked by the
one link the board already had, and a **Roadmap** that draws the whole
tree on a timeline. It also retires three things that stood in the way:
the Process board, card mirrors across projects, and the half-life a
project card led on the day boards.

This document is the source of truth for the hierarchy. The layout and
the storage rules it changes are restated in `git-backend.md`; the rows
that pin the behaviour are in `behavior-matrix.md` (H1–Hn); what a tool
writing the repositories directly has to reproduce is in
`plugin-impact.md`.

## Model

**Every object on the board is a card**, and a card's `type` says what it
is:

| `type`       | what it is                                                | may have `parent` of                   | drawn on                       |
| ------------ | --------------------------------------------------------- | -------------------------------------- | ------------------------------ |
| *(absent)*   | a **task** — engineering work                             | epic, initiative, goal, or a task      | Me, Team, Triage, Roadmap      |
| *(absent)* + `scope` | a **project card** — a plan filed in a Scope column | epic, initiative, goal                 | Project, Roadmap               |
| `epic`       | a body of work worth tracking as one thing                | initiative, goal                       | Roadmap                        |
| `initiative` | a product bet; a start and an end                         | goal                                   | Roadmap                        |
| `goal`       | what the company is trying to reach                       | —                                      | Roadmap                        |

Three consequences, each deliberate:

- **One link.** `parent` is the only link between levels, and it is the
  same `parent` subtasks always had: a bare ULID. The kind of the parent
  is read off the parent card, never encoded in the key. A task whose
  parent is a task is a **subtask**, one level deep, exactly as before;
  a task whose parent is an epic is simply a task under an epic — it is
  not hidden from the day boards, does not inherit a team or an owner,
  and does not ride its parent anywhere. Every rule that used to read
  "a card with a parent" now reads "a task whose parent is a task".
- **The link is optional, and points up only.** A task may have no
  parent, an epic no initiative, an initiative no goal. A parent must be
  of a higher level than the child (a task may skip levels and sit under
  a goal directly), so a cycle cannot be written. A card has at most one
  parent; a task is never under two epics.
- **A planning card is still a card.** It has a title, a description, a
  `## Notes` section, an author, a rank, a log, a watch frame and a
  detail pane, because it is stored and served like any other card —
  nothing was added to the storage for it. Its dates are the card's own
  `start` and `day`, which is why the Roadmap draws a goal and a task by
  one rule. It carries **no `assignees` and no `team`**: the people and
  the teams are on the tasks under it, and the Roadmap reads them off the
  tree (an initiative is "in progress" the moment one of its tasks is
  somebody's). A write that sets either is refused; one found in a file
  is read off. With nothing to place it, a planning card lives in the
  **primary** repository, always.

### What a level is for

A **goal** is the thing the company is trying to reach; it has no parent.
An **initiative** is a product bet made to reach a goal — the unit that
gets a start and an end, and the row a lead reads on the
Roadmap. An **epic** is a body of work inside an initiative (or straight
under a goal) that is worth tracking as one thing. A **task** is what the
day boards have always held. The Scope columns of the Project board are
not a level: a Scope is a column, a bucket of project cards in one
project, and a project card may itself sit under an epic.

### Derived status and progress

A planning card records nothing about progress. Its **status** and
**progress** are derived on every read from the tasks under it, through
every level — four words: *no items* when nothing is under it; *done*
when every leaf task is complete; *in progress* when any leaf task is
somebody's (has an assignee), has started or is finished — a person
named on a task is work being done before the first percent is typed;
*to do* otherwise. Progress is the share of leaf tasks complete, served
beside the counts (`3/7`). This is the subtask rule (`DerivedProgress`)
applied up the tree, and like it, it is never stored. Nobody sets a
status by hand at this stage; the Roadmap and the card's own pane show
the derived one.

## Project cards live on the Project board, and nowhere else

A card with a `scope` is a plan, not work in hand: it **has no team, no
assignees, no band and no size** (a write that sets any of them is
refused; a task filed into a column leaves its band and size behind, the
way it leaves its week and sprint), and it is **not drawn on Me, Team or
Triage**. Its week is no longer derived from its dates, and nothing
syncs it. A file that still carries a band or a size on such a card is
read without them and rewritten without them the next time it changes. It stands in its Scope column on the
Project board, may be placed under an epic, initiative or goal (a
planning `parent`, the hierarchy's link), and shows on the Roadmap under
that parent. It is **never a subtask and has none** — a subtask rides its
parent's team, person and sprint onto the day boards, and a project card
has none of those to give or to take; so its `parent`, when it has one,
is always a planning card, and no task is ever grouped under it — and it
is **never sent to review**; the one trace of work it carries is its
progress. Filing a card that carries a team, an owner, a TASK parent or
a review link into a column is refused the same way (`ErrProjectCard`,
422 `projectCard`): the person takes those off first, or makes a card in
the column and links the work under the hierarchy. A card scheduled for
a week and nothing else takes that week's span as its dates on the way
in, and leaves the weekly plan and its sprint.

This reverses the slot rule (the slot clauses of D2 and D6, B14, and the
week derivation in
`board.NewBoardIn`), under which a project card with a week stood in its
owner's column on the Team grid. In practice it put planning
placeholders among the day's work and made the same card answer two
questions. The two boards are now disjoint: the Project board is the
plan, the day boards are the work, and the hierarchy is what connects
them.

## Scope, not Epic

The column of the Project board was called an epic. It is not the new
epic — a column is a bucket in one project, an epic is a level of the
hierarchy that may span projects — so the column is renamed **Scope**
everywhere: the directory (`projects/<id>/scopes/`), the card key
(`scope`), the API (`/api/v1/scopes`, `spec.scope`, `ScopeRef`), the
MCP tools (`add_scope`, `rename_scope`, …) and the UI. The old key is
read as an alias of the new one, so history before the migration still
loads.

## What is retired

**Processes.** The Process board, its processes, tasks and turns, the
sweep that filed turns every tick, the eleven MCP tools, the API routes
and the `process`, `task` and `accumulate` card keys are removed. The
feature had grown to a tenth of the server for a report nobody read, and
its "task" was the one thing the word could not now mean. A turn that
was filed is **orphaned**: it stays a card of its week and its zone with
its title and its `recurrent` stage, and loses only the link to the task
it came from. Card-level recurrence (`recurrence` on an ordinary card,
`UpcomingRecurrences`) is unrelated and stays. `processes.md` is deleted
with the feature.

**Mirrors.** A card no longer stands in a second project's column. The
`mirrors` key, the mirror and unmirror actions and tools, and the
placement logic in the SPA are removed; `remove-from-project` stays and
means what is left of it — take the card out of its Scope column. A card
that was mirrored stays in its home column.

**The slot rule.** See the section above.

## Layout

The shape of the repository does not change; one directory goes, one is
renamed, two card keys go, one arrives:

```
board.yaml                         primary domain only: schema, title
teams/<id>.yaml                    one team
users/<login>.yaml                 one person
projects/<id>/project.yaml         one project
projects/<id>/scopes/<id>.yaml     one Scope column: name, rank   (was epics/)
projects/<id>/deadlines/<id>.yaml  one deadline line: week
cards/<a>/<b>/<id>.md              one card of ANY type: front-matter + body
```

In a card's front-matter: `type` (absent, `goal`, `initiative`, `epic`)
is written right after `title`; `parent` keeps its place and its
meaning widens as above; `epic` becomes `scope`; `process`, `task`,
`accumulate` and `mirrors` are no longer written. A key the reader does
not know is kept verbatim (`Extra`) and written back, as before, which is
what lets a clone that is one schema behind still read the file.

## Domain

The inheritance rule loses one clause and keeps its order:

1. a **review card** (`reviewOf`) lives where the card it reviews lives;
2. a **subtask** — a task whose `parent` is a task — lives where its
   parent lives;
3. an unlinked card filed under a **project** (a Scope column, or a
   `project` without one) lives where that project lives;
4. any other card lives where its **team** lives (the no-team group
   lives in the primary domain).

A planning card has no team and no project, so nothing places it: it
lives in the **primary** repository, always — the one repository every
reader of the board can read (without it there is no board at all,
403). A hierarchy link does **not** move the child — a task under an
epic keeps the domain its own project or team gives it. What the link
must never do is show a reader of the child a hole where the parent is,
so `parent` may point **up to the primary** from any repository (a
closed project's task under the company's goal is the ordinary case), or
to a parent whose file stands in the child's own repository; anything
else is refused (`ErrCrossDomain`). A planning card's derived status
counts every task under it, the server reading every repository as it
does for sprint pointers, so a visitor may see `2/3 done` and two tasks
listed — the third is one they may not read. The link table in
`git-backend.md` carries the row.

## Reading older history

A day before the migration is served from the tree as it was, and that
tree still has `processes/`, `epic:` keys, `task:` keys and `mirrors:`
lists. The loader ignores the `processes/` directory, reads `epic` as
`scope`, and keeps `task`, `process`, `accumulate` and `mirrors` as
unknown keys, so a past-day record opens and shows the cards as they
stood. Nothing is reconstructed.

## Migration: schema 1 → 2

One bump, one commit per domain at start-up (`gitstore.MigrateSchema`,
G18), in this order:

1. delete `processes/`;
2. strip `task`, `process` and `accumulate` from every card;
3. rename `projects/<id>/epics/` to `projects/<id>/scopes/` and rewrite
   `epic:` to `scope:` on every card;
4. strip `mirrors` from every card;
5. strip `team`, `assignees`, `parent`, `zone` and `size` from every card
   that has a `scope`, and `parent` from every card grouped under one
   (decision taken
   2026-11-02: a card in a column was created as a plan and stays one; a
   plan is never a subtask and has none — and before schema 2 every
   `parent` was a task's, so there is no planning parent to keep).

Idempotent — a step that finds nothing to do writes nothing — and
rehearsed against `aeman-db-dev` before the production repositories see
it.

## Views

The server's views are `me`, `team`, `triage`, `backlog`, `project`,
**`roadmap`** and `all`. The SPA shows five tabs — **Me, Team, Triage,
Roadmap, Project** — the Process tab is gone, Backlog is the pane of
Triage it always was, `all` is the escape hatch for tools.

**Me, Team and Triage draw tasks.** A card with a `type` or a `scope` is
not on them, whatever its dates, week or sprint say.

**Project** draws project cards in their Scope columns, as before,
without mirrors.

**Roadmap** draws the hierarchy — every planning card, and every card that
has a planning card above it, however many levels up — and, **below the
tree, the work that is not in it yet**: every open task with no planning
card above it, which is the work in hand on the Team and Me boards, the
cards parked on a team's shelf, and the strip (H9). The loose work is
listed because the Roadmap is where a lead drags it into the tree, and a
board cannot offer a drag of a card it does not draw. A task already
finished outside the hierarchy is not listed (nothing left to plan on
it), nor is a project card in no hierarchy (the Project board's), and a
subtask follows its parent task in or out. The listing is the card
listing of the view (`GET /api/v1/views/roadmap/cards`), with the same
watch; the tree is assembled from `parent` on the client, and the loose
work is grouped by what it is — in hand, parked, unsorted. The
watch keeps the tree LIVE: a planning card is never written when a task
under it changes, so every card change also arrives as a `MODIFIED`
frame for each planning card above it — the chain it stands under and
the chain it left — with the counts the server derived, to everyone the
Roadmap draws them for, the originator included. A
**create** on the Roadmap is a planning card — `type` is `goal`,
`initiative` or `epic`, `parent` optional under the level rule — or a
**task under one**: no `type`, a `parent` naming a goal, an initiative
or an epic (the epic's `+`). Such a task is born with a title, its
parent and, when given, dates — no team, no owner, no day it was not
given, no sprint — so it stands in the Triage strip of the no-team
group and on the Roadmap under its parent until somebody dates or weeks
it; under a task it would be a subtask, which is the parent's own
board's gesture and is refused here. A task with no parent is not the
Roadmap's, and a create on a day board refuses `type` (G67 — the board
a caller stands on is part of the address).

## The Roadmap tab

Left, a tree: one row per card, indented by level with a guide line down
each level and a chevron on a row that has rows under it, one
**pictogram** per kind (`KindIcon`: a target for a goal, a flag for an
initiative, a stack for an epic, a ticked box for a task, a branch for a
subtask, a dashed card for a project card — the same mark wherever the
hierarchy is drawn), the card's title, its start, its end (`day`) and its
status — derived for a planning card (`hierarchy.status`: no items, to
do, in progress, done, with `done/total` beside it), and for a task its
own: done, in progress, to do. Right, a timeline: one bar per card from
`start` to `day`, every type by the same rule; a card with one date is a
point, with none a row without a bar; a done bar is drawn distinct, and a
bar that runs past the window is cut square at its edge. Rows and lanes
share one height, so the two panes line up by construction. Above: a
depth selector (two levels, "from goal to task", "from initiative to
subtask" — a level above the range is skipped and its rows stand in its
place, a level below is cut with its subtree; initiative to task by
default), a zoom (3 months, a year, fit — three months and a year open
two weeks before this Monday, fit spans every bar with a week of margin),
the window's dates and a person filter — a filter keeps the matching
cards and every card above them, so the tree keeps its shape. There is no
team filter: a planning card has no team. The depth, the zoom and which
rows are folded are the reader's own, kept in the browser.

The board's gestures: the add box makes a planning card at the top; the
`+` on a goal or an initiative makes one under it, the `+` on an epic a
TASK under it — undated, nobody's, standing in the Triage strip until it
is placed (H7); the `×` on a planning card asks the Team board's own
question, with the hierarchy's two answers — delete it (the rows under it
move up a level, or to the top, and the dialog says which) or only take
it out from under its parent. A row **drags**: onto another row it goes
under it; into the gap before or after a row it joins that gap's parent
at that place (after an unfolded row with rows under it is the first
place under it, as the eye reads the list). The level rule decides what
may land where, and a card never changes its type by being dragged: a
goal goes nowhere but the top, an initiative under a goal, an epic under
either, a task under any of them — or onto a task, which makes it a
subtask, one level deep — a project card under planning cards only, and
the top of the tree for planning cards only (a task dropped there would
leave the Roadmap, which is the ×'s detach, not a drop). The rows are the
**preview**: while the drag stands over a place it may land, the list
reads as it would after the drop, the moved subtree dimmed; over a place
it may not, the hovered row says so and nothing moves. The drop writes
one parent and one ordering move, applied at once and undone with the
server's own words if it refuses. Dates are changed in the card's pane;
nothing is dragged on the timeline in this version. The Roadmap keeps
itself current from its watch like every board: a change under a
planning card reaches the card's ancestors too (H5), so counts and
standings move without a reload.

The card's pane is a **drawer**: it slides in from the right over the
board the card was met on, laid out like an issue page — the title and
its **breadcrumb** up top (ancestors root first; a planning crumb leads
to the Roadmap with that row unfolded and marked, a subtask's task is a
name), the description and the activity down the main column, the card's
fields in a sidebar of labelled rows, and the actions that act on the
whole card at its foot. Which rows there are is the card's kind's to say,
matching what the server refuses: a task has Status, Assignees, Team,
Zone, Size, its dates read-only, Sprint and Week where set, and Under; a
planning card has Level, Status with `done/total`, its dates (editable),
and Under — no people, no band, no size; a project card has Status, its
dates (editable), Under, Project and Scope — no people, no band, no size.
**Under** is one prominent, searchable picker, grouped by level, offering
planning cards of a higher level only (`allowedParentKinds`, the mirror
of the level rule) and nothing on a goal or a subtask (a subtask's task
is the group gesture's); the same list is what the Project board's "…"
menu attaches a card with. A planning card's pane lists what sits under
it, grouped as planning cards, tasks and project cards, each row opening
that card; it can add under it (a goal an initiative or an epic, an
initiative an epic, an epic a task) and take a child away with the same
choice dialog — delete, or only detach. The actions: mark as done and
reopen (a task or a project card), remove from project (a project card),
delete — or delete-or-unlink on a card that sits under something. The
options and the children come from the roadmap listing, fetched once per
card when the pane needs it: the day boards never deliver a planning card
to look up. The rules live in `web/src/roadmap.ts` with their cases in
`roadmap.test.ts`; the components draw them.

Every board opens the drawer the same way: a click on a card (the Project
board's cards included, where a double click used to be the only way in).
The Triage board draws no deadline markers any more — the project cards
they marked are off it, and the lines stay on the Project board.

## Owner

A task has one owner: a **team**, and a **person** on that team — or
neither. The Roadmap asks for it in one place (the Owner cell of a task
row, the Owner row of the drawer) and answers it with one gesture,
**`own`** (`POST /api/v1/views/{view}/cards/{uid}/actions/own
{team?, person?}`, MCP `own_card`), offered by every board that draws a
task — the Roadmap, Me, Team, Triage, its Backlog pane, and `all` — and
not by the Project board, which draws no task. A person is picked FROM a
team: a person without a team is refused (`ownerNeedsTeam`), a team
without a person is the whole answer, both empty takes the owner off.

What the answer **does** depends on whether the card has a plan — a day,
a week or a sprint — because the Roadmap lists the loose work (H9) and
giving it a home means putting it where a day board draws it:

- a **person** files an **unplanned** card the way typing it into their
  Me board would: on the team, today, in the unplanned band, in the
  team's sprint, off any shelf — so it stands on their Team and Me boards
  at once. A **planned** card changes hands and keeps its plan; moving
  team joins that team's sprint, as the Team board's team menu does.
- a **team alone** parks an **unplanned** card on that team's shelf — its
  Backlog on Triage — with nobody on it and in no sprint; a **planned**
  card moves team, keeps its plan, and loses its person.
- **nobody** takes the team and the person off: a planned card keeps its
  plan in the no-team group; an unplanned one leaves the shelf, if it was
  on one, and is the strip's again.

Subtasks follow the task's team and person, as they do everywhere (S9);
a subtask is not asked, and neither is a review card — its person is
whoever was asked to review (`ownerFollowsLink`). A planning card and a
project card carry no owner at all (H3, H4).

**The reviewer** is the other person a task can have, and it has its own
doors already: a card on review names its reviewer through
`send-to-review {reviewer}` — which **reassigns** when a review card
exists, creating one otherwise — and loses them through
`remove-reviewer`; MCP `send_to_review` / `remove_reviewer` are the same
pair. The drawer's Reviewer row calls those; there is no second door.
The reviewer picker offers the people who can read the card's
repository, as it always has; the server does not refuse a reviewer the
picker would not offer (a direct writer can name anyone).

## API and MCP

Removed: every `/processes*` route and schema, `spec.process`,
`spec.task`, `status.due`, `status.cycle`, `metadata.processes`,
`metadata.processDomains`; `/cards/{uid}/actions/mirror` and `/unmirror`,
`Placement`, `spec.mirrors`; the MCP tools `list_processes`,
`add_process`, `delete_process`, `rename_process`, `set_process_project`,
`set_process_paused`, `reorder_processes`, `reorder_process_tasks`,
`add_process_task`, `update_process_task`, `delete_process_task`,
`mirror_card`, `unmirror_card`.

Renamed: `/epics*` → `/scopes*`, `EpicRef` → `ScopeRef`, `spec.epic` →
`spec.scope`, `add_epic` → `add_scope`, `delete_epic` → `delete_scope`,
`rename_epic` → `rename_scope`, `reorder_epics` → `reorder_scopes`,
`set_epic_project` → `set_scope_project`.

Added: `spec.type` (`goal` | `initiative` | `epic`, absent for a task),
`spec.parent` accepting a card of any type under the level rule,
`status.lineage` (the ancestors root-first, each `{uid, type, title}`),
`status.hierarchy` on a planning card (`{status, done, total}`), the view
`roadmap`; the problem codes `unknownType`, `parentLevel`, `planningCard`
(and `crossDomain`, kept from the mirrors, for a link across a
repository); `create_card` takes `type`, `update_card` takes `parent`,
`list_cards view=roadmap`. No tool per level: a goal is a card. There is
no door that changes a card's type: the level is fixed at create, and a
card that should be another level is made again. The gesture `own`
(`/views/{view}/cards/{uid}/actions/own`, MCP `own_card`) with the
problem codes `ownerNeedsTeam` and `ownerFollowsLink` (the Owner
section above); `GET /api/v1/views` lists it on every board that draws a
task.

## Rules pinned

- H1 — one link, `parent`, kind read off the parent; a task under a task
  is a subtask and the only kind that is one. A DANGLING parent — one not
  on the board — reads as a subtask, as it always did: nothing dangles off
  a planning card, since deleting one re-parents its children. Pinned by
  board `TestASubtaskIsATaskUnderATask`, `TestParentOfAndChildrenOf`,
  `TestSubtaskHelpersStopAtPlanningCards`,
  `TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre`; service
  `TestTheLevelRuleGovernsTheParent`, `TestCarryOverCarriesATaskUnderAnEpic`;
  web `hierarchy.test.ts` (`isSubtask`).
- H2 — the level rule: a parent is of a higher level (`board.Level`: goal >
  initiative > epic > task; an unknown type is a task); one parent;
  optional; a task may skip levels. Pinned by board
  `TestTheLevelsAreStrictlyOrdered`, `TestLineageIsEveryAncestorRootFirst`;
  service `TestTheLevelRuleGovernsTheParent`; apiserver
  `TestACardResourceCarriesItsLineageAndHierarchy`.
- H3 — a planning card is drawn on the Roadmap and on no day board, is
  never carried, weighed or overdue, and has no stage, progress, band,
  week, shelf, sprint, column or review of its own (`ErrPlanningCard`,
  422 `planningCard`); its dates are its own, and it has no owner and no team — those are on the tasks under it — so it lives in the primary repository. Pinned by
  board `TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre`; service
  `TestAPlanningCardHasNoDayBoardState`,
  `TestDeletingAPlanningCardReparentsItsChildren`; apiserver
  `TestTheRoadmapListsTheHierarchy`; web `hierarchy.test.ts`.
- H4 — a project card stands in its column alone: no team, no owner, no
  band, no size, no week, never a subtask, no subtasks, never a review;
  drawn on the Project board and the Roadmap only. A planning parent it may
  have. Pinned by board `TestAProjectCardIsOnNoDayBoard`,
  `TestAProjectCardIsNotOnTheTriageBoard`,
  `TestACardInAColumnOrOfALevelReadsWithNoZoneOrSize`; service
  `TestAProjectCardCarriesNoZoneOrSize`;
  apiserver `TestProjectViewDeliversWhatStandsInAColumnAndNothingElse`;
  service `TestAProjectCardTakesNoTeamNoOwnerNoWeek`,
  `TestAProjectCardIsMadeWithoutATeamOrAnOwner`,
  `TestFilingACardIntoAColumnTakesItOffThePlansItWasOn`,
  `TestAProjectCardIsNobodysSubtaskAndHasNone`,
  `TestReDatingAProjectCardMovesItsRowAndNothingElse`,
  `TestPlacingAProjectCardIsRefused`; gitstore
  `TestMigrateSchema2TakesAProjectCardOutOfEverybodysHands`; web
  `teamgrid.test.ts`, `meview.test.ts`, `triage.test.ts`,
  `placements.test.ts`.
- H5 — derived status and progress, never stored: `no-items`, `todo`,
  `in-progress` (any leaf task assigned, started or done) or `done` with
  the counts, over the leaf tasks (a task whose parent is a planning
  card; a subtask counts through its parent). Pinned by board
  `TestHierarchyStatusIsDerivedFromTheLeafTasks`,
  `TestAnAssignedLeafTaskPutsItsPlanningCardInProgress`; service
  `TestTheLevelRuleGovernsTheParent` (a task's progress writes nothing on
  the epic); apiserver `TestACardResourceCarriesItsLineageAndHierarchy`.
- H6 — `parent` never shows a reader of the child a hole: it may point up
  to the primary (where every planning card lives) or to a parent in the
  child's own repository, and is refused otherwise (`ErrCrossDomain`, 422
  `crossDomain`); a hierarchy link does not move the child — a task under
  an epic lives where its own project or team lives, and nothing under a
  planning card follows it between repositories. Pinned by board
  `TestAHierarchyLinkDoesNotPlaceTheChild`,
  `TestSubtaskHelpersStopAtPlanningCards`; service
  `TestAHierarchyLinkStaysInsideOneRepository`.
- H7 — a Roadmap create is a planning card (`type` required) or a task
  under one (no `type`, `parent` a planning card; born undated, nobody's,
  in no sprint — the Triage strip's until placed; under a task refused);
  a team, an owner, a zone and a size are refused by name; a day-board
  create refuses `type` (`ErrNotOnThisBoard`); the Roadmap draws the ×,
  which deletes the planning card and hands its children to its parent.
  Pinned by service `TestAPlanningCardIsBornOnTheRoadmapAndNowhereElse`,
  `TestDeletingAPlanningCardReparentsItsChildren`,
  `TestAGestureIsOfferedByTheBoardThatDrawsIt`; apiserver
  `TestARoadmapBornTaskStandsInTheStripAndOnTheRoadmap`; server
  `TestTheCatalogNamesTheBoardsAndTheirGestures`; web `hierarchy.test.ts`
  (`createView`).
- H8 — schema 2: the five steps, idempotent, and what a pre-migration
  day still shows.
- H9 — the Roadmap also lists, below the tree, every open task with no
  planning card above it (in hand, parked, in the strip); a finished one
  outside the hierarchy, a project card in none, and a subtask whose
  parent is out stay out; the gate and the watch follow. Pinned by
  apiserver `TestTheRoadmapListsTheHierarchy`; server
  `TestAChangeUnderAPlanningCardReachesItsAncestorsOnTheRoadmap`.
- H10 — `own`: a team and a person, a team alone, or nobody; a person
  files an unplanned card like a Me-board create, a team alone parks it,
  a planned card only changes hands; a person needs a team; a subtask, a
  review card, a planning card and a project card are refused; offered by
  every board that draws a task. Pinned by service
  `TestOwnGivesLooseWorkAHome`, `TestOwnIsOfferedWhereATaskIsDrawn`;
  server `TestOwnIsAnsweredByTheBoardsThatDrawATask`,
  `TestTheCatalogNamesTheBoardsAndTheirGestures`; mcp
  `TestMCPListsTools`, `TestTheToolSetHoldsWhatTheBoardsDo`.
- H11 — the reviewer's doors are `send-to-review` (reassigns when a
  review exists) and `remove-reviewer`, on REST and MCP alike; nothing
  else sets a reviewer. Pinned by service
  `TestReassignReviewerOnExistingReview`,
  `TestReassignReviewerWithoutReviewSendsToReview`,
  `TestRemoveReviewerDeletesReviewCard`.

Retired: G28, G29, G38, G56 (processes); G15, G54, G57 (mirrors, and the
subtask-in-a-column rule); B14 and the slot clauses of D2/D6 together
with the week derivation (the slot rule) — each row kept and marked
removed, the way B7 is. (D17, the week-planned card on its owner's Me
board, is not the slot rule and stays.)

## Open questions

- **TODO — the backlog is per team, and should be per value stream.** A
  shelf belongs to a team today (`board.InBacklog`, keyed by the card's
  `team`; the Triage drawer draws one per team), and `own` with a team
  alone parks a card on that team's shelf. The user's direction
  (2026-11-02) is that the backlog be reworked around the chain that
  delivers value — a product, a customer, a flow — rather than the
  organisational unit, because a team is not what a piece of work is
  waiting FOR. Deferred: the team-keyed shelf stays until the chain is a
  thing the board can name.

## Rollout

One release. Every phase is a commit on one branch; the migration is
rehearsed on `aeman-db-dev`; production is upgraded only after the user
has signed the rehearsal off. There is no downgrade path past the schema
bump: a server older than this one refuses a schema-2 repository (G18).
