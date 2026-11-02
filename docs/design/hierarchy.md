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
| `initiative` | a product bet; an owner, a start and an end               | goal                                   | Roadmap                        |
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
  one rule. It may carry `assignees` (the initiative's owner — a product
  owner, a lead) and a `team`; neither puts it on a day board, because
  the day boards draw tasks only.

### What a level is for

A **goal** is the thing the company is trying to reach; it has no parent.
An **initiative** is a product bet made to reach a goal — the unit that
gets an owner, a start and an end, and the row a lead reads on the
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

A card with a `scope` is a plan, not work in hand: it **has no team and
no assignees** (a write that sets either is refused), and it is **not
drawn on Me, Team or Triage**. Its week is no longer derived from its
dates, and nothing syncs it. It stands in its Scope column on the
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

A planning card is "any other card": it lives where its team lives, in
the primary domain when it has none. A hierarchy link does **not** move
the child — a task under an epic keeps the domain its own project or
team gives it — but it may not cross a domain either: `parent` must name
a card of the child's own domain (`ErrCrossDomain`, the rule mirrors
used to be held to), or a visitor who can read the task and not the
epic would see a breadcrumb with a hole in it. The link table in
`git-backend.md` gains the row.

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
5. strip `team`, `assignees` and `parent` from every card that has a
   `scope`, and `parent` from every card grouped under one (decision taken
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

**Roadmap** draws the hierarchy: every planning card, and every card that
has a planning card above it, however many levels up. A task with no
parent is not on the Roadmap — it is on the day boards. The listing is
the card listing of the view (`GET /api/v1/views/roadmap/cards`), with
the same watch; the tree is assembled from `parent` on the client. A
**create** on the Roadmap is a planning card: `type` is required and must
be `goal`, `initiative` or `epic`; `parent` is optional and must obey
the level rule; a create on a day board refuses `type` (G67 — the board
a caller stands on is part of the address).

## The Roadmap tab

Left, a tree: one row per card, indented by level, expandable, with a
glyph for what it is (goal, initiative, epic, task, subtask, project
card), the card's title, its start, its end (`day`) and its status —
derived for a planning card (`hierarchy.status`, with `done/total`
beside it), and for a task its own: done, in progress, to do. Right, a
timeline: one bar per card from `start` to `day`, every type by the same
rule; a card with one date is a point, with none a row without a bar; a
done bar is drawn distinct, and a bar that runs past the window is cut
square at its edge. Rows and lanes share one height, so the two panes
line up by construction. Above: a depth selector (two levels, "from goal
to task", "from initiative to subtask" — a level above the range is
skipped and its rows stand in its place, a level below is cut with its
subtree; initiative to task by default), a zoom (3 months, a year, fit —
three months and a year open two weeks before this Monday, fit spans
every bar with a week of margin), the window's dates, the team chips and
a person filter — a filter keeps the matching cards and every card above
them, so the tree keeps its shape. The board's own gestures are the two
the server offers it: the add box makes a planning card at the top, the
`+` on a goal or an initiative one under it (an epic makes nothing below
itself — a task is born on a day board and filed here from its own pane,
H7), and the `×` on a planning card deletes it after saying where the
rows under it will go (up a level, or to the top). Dates are changed in
the detail pane; nothing is dragged on the timeline in this version. The
depth, the zoom and which rows are folded are the reader's own, kept in
the browser.

A card's detail pane, opened from any board, shows a **breadcrumb** of its
ancestors root-first — a planning crumb leads to the Roadmap with that row
unfolded and marked; a subtask's task is a name — and a picker for what
the card sits under, offering planning cards of a higher level only
(`allowedParentKinds`, the mirror of the level rule) and nothing on a goal
or a subtask (a subtask's task is the group gesture's). The server's
refusal is shown in its own words. On a planning card the pane shows its
level, its derived standing and `done/total`, its start and end, and
what sits under it grouped as planning cards, tasks and project cards,
each row opening that card — in place of the band a planning card cannot
carry. The options and the children come from the roadmap listing,
fetched once per card when the pane needs it: the day boards never
deliver a planning card to look up. The rules live in `web/src/roadmap.ts`
with their cases in `roadmap.test.ts`; the components draw them.

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
card that should be another level is made again.

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
  422 `planningCard`); its dates, owner and team are its own. Pinned by
  board `TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre`; service
  `TestAPlanningCardHasNoDayBoardState`,
  `TestDeletingAPlanningCardReparentsItsChildren`; apiserver
  `TestTheRoadmapListsTheHierarchy`; web `hierarchy.test.ts`.
- H4 — a project card stands in its column alone: no team, no owner, no
  week, never a subtask, no subtasks, never a review; drawn on the Project
  board and the Roadmap only. A planning parent it may have. Pinned by board
  `TestAProjectCardIsOnNoDayBoard`, `TestAProjectCardIsNotOnTheTriageBoard`;
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
- H6 — `parent` never crosses a domain (`ErrCrossDomain`, 422
  `crossDomain`); a hierarchy link does not move the child — a task under
  an epic lives where its own project or team lives, and nothing under a
  planning card follows it between repositories. Pinned by board
  `TestAHierarchyLinkDoesNotPlaceTheChild`,
  `TestSubtaskHelpersStopAtPlanningCards`; service
  `TestAHierarchyLinkStaysInsideOneRepository`.
- H7 — Roadmap create requires a type; a day-board create refuses one
  (`ErrNotOnThisBoard`); the Roadmap draws the ×, which deletes the
  planning card and hands its children to its parent. Pinned by service
  `TestAPlanningCardIsBornOnTheRoadmapAndNowhereElse`,
  `TestDeletingAPlanningCardReparentsItsChildren`,
  `TestAGestureIsOfferedByTheBoardThatDrawsIt`; server
  `TestTheCatalogNamesTheBoardsAndTheirGestures`; web `hierarchy.test.ts`
  (`createView`).
- H8 — schema 2: the five steps, idempotent, and what a pre-migration
  day still shows.

Retired: G28, G29, G38, G56 (processes); G15, G54, G57 (mirrors, and the
subtask-in-a-column rule); B14 and the slot clauses of D2/D6 together
with the week derivation (the slot rule) — each row kept and marked
removed, the way B7 is. (D17, the week-planned card on its owner's Me
board, is not the slot rule and stays.)

## Rollout

One release. Every phase is a commit on one branch; the migration is
rehearsed on `aeman-db-dev`; production is upgraded only after the user
has signed the rehearsal off. There is no downgrade path past the schema
bump: a server older than this one refuses a schema-2 repository (G18).
