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

## Removed: the Project board (2026-11-07)

The Project board is gone, and with it every **project card** (a card
with a `scope`, or `epic` before schema 2), the projects roster
(`projects/<id>/project.yaml`, its `scopes/` — `epics/` before schema 2 —
and `deadlines/`), the Scope columns, the deadline lines, "Attach to…",
the Roadmap's "Project cards" toggle (`projectCards`), `view=project`,
`spec.scope`/`spec.project`, `status.overdue`, the board's
`metadata.projects`/`scopes`/`deadlines`/`projectDomains`, the
`/scopes`, `/projects` and `/deadlines` routes, their MCP tools, the
problem codes that only they answered (`projectCard`,
`projectCardProgress`, `scope*`, `project*`, `notYoursToDestroy`,
`notYoursToPark`, `domainConflict`), the project step of the domain rule
and the H4 rules below. The owner's decision was to drop project cards
as they stand — **not** to turn them into tasks — and to leave their
files in the repositories: no conversion, no migration, no schema bump
(the schema stays 3).

What remains is a storage rule (H15): every reader skips a project-card
file and everything under `projects/` — not loaded, listed, counted or
served, and not reported as unknown or broken — and no writer touches
them: a write addressed at a project card's id is refused as not found
and the file stays byte for byte; maintenance, the rebase replay,
cross-domain moves and the `Aeman-Cards` trailers only ever see the cards
the board loaded. A `parent` naming one is dangling and dropped on read,
so a planning card's counts exclude them. A task's stray `project:` key
is read and dropped. The importer leaves a Projects v2 board's
Project-board items out and says how many.

The sections below that describe project cards, Scope columns and the
Project board are kept as the record of how the hierarchy was built;
read them with this section in mind.

## Model

**Every object on the board is a card**, and a card's `type` says what it
is:

| `type`       | what it is                                                | may have `parent` of                   | drawn on                       |
| ------------ | --------------------------------------------------------- | -------------------------------------- | ------------------------------ |
| *(absent)*   | a **task** — engineering work                             | epic, initiative, goal, or a task      | Me, Team, Backlog, Roadmap     |
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
  one rule. It carries **no `assignees`**: the people are on the tasks
  under it, and the Roadmap reads them off the tree (an initiative is "in
  progress" the moment one of its tasks is somebody's). A write that sets
  one is refused; one found in a file is read off. It MAY carry a `team` —
  the team that carries it out — an `owner` — the one person who answers
  for it — and `customers` ([ownership.md](ownership.md), O1–O10, amended
  2026-11-06): none of them places it or puts it on any day board, and the
  team and customers are inherited by the cards below as their effective
  ones. With nothing to place it, a planning card lives in the **primary**
  repository, always.

### What a level is for

A **goal** is the thing the company is trying to reach; it has no parent.
An **initiative** is a product bet made to reach a goal — the unit that
gets a start and an end, and the row a lead reads on the
Roadmap. An **epic** is a body of work inside an initiative (or straight
under a goal) that is worth tracking as one thing. A **task** is what the
day boards have always held. (The Scope columns of the Project board
were never a level; they went with the board, 2026-11-07.)

### Derived status and progress

A planning card records nothing about progress. Its **status** and
**progress** are derived on every read from the tasks under it, through
every level — four words: *no items* when nothing is under it; *done*
when every leaf task is complete; *in progress* when any leaf task is
somebody's (has an assignee), has started — any progress above 0, or
held in review or locked, since a held task is not in the band — or is
finished — a person
named on a task is work being done before the first percent is typed;
*to do* otherwise. Progress is the share of leaf tasks complete, served
beside the counts (`3/7`). This is the subtask rule (`DerivedProgress`)
applied up the tree, and like it, it is never stored. It is a walk over the
board (the leaf tasks), so a listing builds one `board.Index` — the cards
by id and by parent — and asks it of every card, and a change announces
its ancestors with resources built once for every watcher; asked card by
card it was a walk of the board per card, per watcher. Nobody sets a
status by hand at this stage; the Roadmap and the card's own pane show
the derived one.

## Project cards lived on the Project board, and nowhere else (removed)

Until 2026-11-07 a card with a `scope` was a PROJECT CARD (H4): a plan,
not work in hand — no team, no assignees, no band, no size, done or not
done, never a subtask and never on review, drawn on the Project board and
under its planning parent on the Roadmap, and living and dying in its
column. It reversed the earlier slot rule, under which a project card
with a week stood in its owner's column on the Team grid. The whole kind
is gone now (see "Removed: the Project board" above); its files are left
in the repositories and read by nobody.

## Scope, not Epic

(Historical: the Scope column itself was removed on 2026-11-07.) The
column of the Project board was called an epic. It is not the new
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
placement logic in the SPA are removed, and so is `remove-from-project`.

**The slot rule.** See the section above.

**The Project board** followed on 2026-11-07 (see the top of this
document).

## Layout

The shape of the repository does not change; one directory goes, one is
renamed, two card keys go, one arrives:

```
board.yaml                         primary domain only: schema, title
teams/<id>.yaml                    one team
users/<login>.yaml                 one person
cards/<a>/<b>/<id>.md              one card of ANY type: front-matter + body
```

(`projects/` — projects, Scope columns and deadline lines — was part of
this layout until 2026-11-07; a repository may still hold it, and every
reader skips it.)

In a card's front-matter: `type` (absent, `goal`, `initiative`, `epic`)
is written right after `title`; `parent` keeps its place and its
meaning widens as above; `epic` becomes `scope`; `process`, `task`,
`accumulate` and `mirrors` are no longer written. A key the reader does
not know is kept verbatim (`Extra`) and written back, as before, which is
what lets a clone that is one schema behind still read the file.

## Domain

The inheritance rule loses one clause and keeps its order (and lost a
second one, the project's, on 2026-11-07):

1. a **review card** (`reviewOf`) lives where the card it reviews lives;
2. a **subtask** — a task whose `parent` is a task — lives where its
   parent lives;
3. any other card lives where its **team** lives (the no-team group
   lives in the primary domain).

A planning card's team places nothing, so nothing places it: it
lives in the **primary** repository, always — the one repository every
reader of the board can read (without it there is no board at all,
403). A hierarchy link does **not** move the child — a task under an
epic keeps the domain its own team gives it. What the link
must never do is show a reader of the child a hole where the parent is,
so `parent` may point **up to the primary** from any repository (a
closed team's task under the company's goal is the ordinary case), or
to a parent whose file stands in the child's own repository; anything
else is refused (`ErrCrossDomain`). "The child's own repository" is the
one it will live in AFTER the link: a subtask leaving its task for a
planning card is no subtask then, and its own team places it. A planning card's derived status
counts every task under it, the server reading every repository as it
does for sprint pointers, so a visitor may see `2/3 done` and two tasks
listed — the third is one they may not read. The link table in
`git-backend.md` carries the row.

## Reading older history

A day before the migration is served from the tree as it was, and that
tree still has `processes/`, `epic:` keys, `task:` keys and `mirrors:`
lists. The loader ignores the `processes/` directory, reads `epic` as
`scope` and the column stubs under `projects/<id>/epics/` as the
project's scopes (the card alone would otherwise stand in a column the
board does not have), and keeps `task`, `process`, `accumulate` and
`mirrors` as unknown keys, so a past-day record opens and shows the cards
as they stood, columns included. Nothing is reconstructed. (Since
2026-11-07 a card in a column — under either key — is a retired project
card, and a past day is read without it like the live board.)

## Migration: schema 1 → 2

One bump, one commit per domain at start-up (`gitstore.MigrateSchema`,
G18), in this order:

1. delete `processes/`;
2. strip `task`, `process` and `accumulate` from every card;
3. rename `projects/<id>/epics/` to `projects/<id>/scopes/` and rewrite
   `epic:` to `scope:` on every card;
4. strip `mirrors` from every card;
5. strip `team`, `assignees`, `parent`, `zone`, `size`, `sprint` and `week`
   from every card that has a `scope` (the door that files a card into a
   column clears the sprint and the week as well, and a sprint on a card no
   day board draws is one a carry-over would move for nothing) — a card
   whose `week` was its only date takes that week as its dates first,
   Monday to Friday, the conversion the filing door makes
   (`board.WeekAsDates`; a card the Project board draws by its dates would
   otherwise have no row on any board; dates the card has win) — and
   `parent` from every card grouped under one
   (decision taken
   2026-11-02: a card in a column was created as a plan and stays one; a
   plan is never a subtask and has none — and before schema 2 every
   `parent` was a task's, so there is no planning parent to keep).

Steps 3 and 5 were cut back on 2026-11-07, when project cards were
removed: the bump now leaves `projects/` and every card carrying `scope`
or `epic` byte for byte as they are, and only ungroups a card whose
`parent` is such a card.

Idempotent — a step that finds nothing to do writes nothing — and
rehearsed against `aeman-db-dev` before the production repositories see
it.

## Views

The server's views are `me`, `team`, `backlog`, **`roadmap`** and
`all`. The SPA shows four tabs — **Me, Team, Backlog, Roadmap** — the
Process tab is gone, the Triage tab with it went on 2026-11-03, and the
Project tab on 2026-11-07: the Backlog, which used to be its drawer, is a board
of its own ([backlog.md](backlog.md)). `all` is the escape hatch for
tools.

**Me, Team and Backlog draw tasks.** A card with a `type` is not on
them, whatever its dates or sprint say.

**Roadmap** draws the hierarchy — every planning card, and every card that
has a planning card above it, however many levels up — and, **below the
tree, the work that is not in it yet**: every open task with no planning
card above it, which is the work in hand on the Team and Me boards and the
tasks parked in a team's backlog (H9). The loose work is
listed because the Roadmap is where a lead drags it into the tree, and a
board cannot offer a drag of a card it does not draw. A task already
finished outside the hierarchy is not listed (nothing left to plan on
it). Nor is a
review card (it follows the card it reviews, which is the work — the
owner picker refuses it and so does the link), and a subtask follows its
parent task in or out. The listing is the card
listing of the view (`GET /api/v1/views/roadmap/cards`), with the same
watch; the tree is assembled from `parent` on the client, and the loose
work is grouped by what it is — in hand or parked. The
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
given, no sprint — so, given no dates, it is parked in the no-team
group's backlog (K3) and stands on the Roadmap under its parent until
somebody takes it into work; under a task it would be a subtask, which is the parent's own
board's gesture and is refused here. A task with no parent is not the
Roadmap's, and a create on a day board refuses `type` (G67 — the board
a caller stands on is part of the address).

## The Roadmap tab

Left, a tree: one row per card, indented by level with a guide line down
each level and a chevron on a row that has rows under it, one
**pictogram** per kind (`KindIcon`: a target for a goal, a flag for an
initiative, a stack for an epic, a ticked box for a task, a branch for a
subtask — the same mark wherever the
hierarchy is drawn), the card's title, its **owner** (a task's team and
the person on it — the cell opens the owner picker, below; a planning
card has none and an empty cell), its start, its end
(`day`) and its status — derived for a planning card (`hierarchy.status`:
no items, to do, in progress, done, with `done/total` beside it), and for
a task its own, in this order: done when complete; else the stage that
holds it — review, locked, refused — which says who it waits on; else to
do while nothing has started (its bar at zero); else in progress. The
drawer's Status row reads the same answer. Right, a timeline: one bar per card from
`start` to `day`, every type by the same rule; a card with one date is a
point, with none a row without a bar; a done bar is drawn distinct, and a
bar that runs past the window is cut square at its edge. Rows and lanes
share one height, so the two panes line up by construction. Above: a
depth selector (two levels, "from goal to task", "from initiative to
subtask" — a level above the range is skipped and its rows stand in its
place, a level below is cut with its subtree; initiative to task by
default), a zoom (3 months, a year, fit — three months and a year open
two weeks before this Monday, fit spans every bar with a week of margin),
the window's dates, and the team, customer and owner filters
([ownership.md](ownership.md), O7, O8: team and customer match a task by
its effective value and a planning card by itself or any task under it;
owner — the one person filter — matches a planning card by its own owner
and a task or subtask by its assignees), applied by the server — a filter
keeps the matching cards and every card above them, so the tree keeps its
shape. The table's column widths are the reader's to size too — a drag
on any boundary of the header, the last one being the split with the
timeline, a double-click to put one back (H12). The depth, the zoom, the
filters, the column widths and which rows are folded are the reader's own,
kept in the browser.

**Below the tree, after everything else, the loose work** (H9): the open
cards with no planning card above them, in sections — **In work**
(what the day boards draw: a card with a day or a sprint) and
**Backlog** (one per team, the no-team backlog among them). The
**Unsorted** section of the strip went with it (`backlog.md`, K3): no
door leaves a task with no place any more. A section is a header line and its rows, drawn on both
panes so the lanes stay in step; its rows show the task's owner and
subtasks like the tree's, and a row there **drags into the tree** under
a planning card by the same rule and with the same landing mark — dragging
within the sections does nothing, and nothing is dropped onto a section
row. The filters apply to them too. (`sectionOf` / `sectionsOf`
in `roadmap.ts`: a root with a planning crumb in its lineage stays in the
tree even when its parent is not in the listing — a hole is worse than a
flat row.)

The board's gestures: the add box makes a planning card at the top; the
`+` on a goal or an initiative makes one under it, the `+` on an epic a
TASK under it — undated, nobody's, parked in the no-team backlog until it
is taken into work (H7, K3); the `×` on a planning card asks the Team board's own
question, with the hierarchy's two answers — delete it (the rows under it
move up a level, or to the top, and the dialog says which) or only take
it out from under its parent. A row **drags**: onto another row it goes
under it; into the gap before or after a row it joins that gap's parent
at that place (after an unfolded row with rows under it is the first
place under it, as the eye reads the list). The level rule decides what
may land where, and a card never changes its type by being dragged: a
goal goes nowhere but the top, an initiative under a goal, an epic under
either, a task under any of them — or onto a task, which makes it a
subtask, one level deep — and the top of the tree for planning cards only (a task dropped there would
leave the Roadmap, which is the ×'s detach, not a drop). **Nothing moves
while a row is dragged** (H13): the dragged row and its subtree are
dimmed where they stand, and where the card would land is a **mark drawn
over the rows** that takes no room — a line in the gap, indented to the
depth the card would land at, or the row it would go under, outlined;
over a place it may not land, the cursor says so and there is no mark.
The target is read off the rows as they stood when the drag began, and
the pointer has to go a little past a boundary — between two rows, or
between a row's before / under / after parts — before it changes, so a
slow aim holds still. (An earlier version previewed the drop by moving
the rows to where the card would land; that put other rows under the
pointer, which read as another place, and the list jumped under a slow
aim.) A leaf row keeps a spacer the
chevron's width and no button, so nothing lights up where there is
nothing to unfold. The drop writes
one parent and one ordering move, applied at once — with a filter on as
without, since the watch does not echo the tab's own move — and undone with the
server's own words if it refuses. Dates are changed in the card's pane;
nothing is dragged on the timeline in this version. The Roadmap keeps
itself current from its watch like every board: a change under a
planning card reaches the card's ancestors too (H5), so counts and
standings move without a reload.

The card's pane is a **drawer**: it slides in from the right over the
board the card was met on, as wide as an issue page's panel (the whole
width on a phone), laid out like one — the title and its **breadcrumb**
up top, the tabs (Details, Activity) at the top of the main column and
as wide as it, the description and the activity down that column, which
takes two thirds, the card's fields in a sidebar of labelled rows that
takes the third and never cuts a value short, and the actions that act on
the whole card at its foot. Every crumb **opens that card** in the same
drawer — ancestors root first, a subtask's task among them — with a way
back to the card it was opened from (the pane keeps a stack); a planning
crumb also offers the Roadmap, with that row unfolded and marked. A child
row and the "Subtask of" row open their card the same way.

The **description** is Markdown and is edited in place: the text reads as
the page it renders to, a click anywhere in it puts the caret there, the
syntax (`**bold**`, lists, links, code, headings) renders as it is typed,
the clipboard carries Markdown both ways, and leaving the editor or
Ctrl/Cmd+Enter saves the draft beside the Save button (Milkdown over
ProseMirror, `MarkdownEditor`; a chunk of its own, fetched when a pane
first opens, with a plain box editing the same draft until it is there
or if it never comes). The box fills the column's height.

Which rows there are is the card's kind's to say, matching what the
server refuses: a task has Status, **Owner** (its team and the person on
it, in the owner picker — a subtask's reads its task's), **Reviewer**
when it is on review (the person on the linked review card, chosen from
those who can read the card's repository, through the review link's own
doors), Zone, Size, its dates read-only, Sprint where set, and
Under; a planning card has Level, Status with `done/total`, its dates
(editable), and Under — no owner, no band, no size. **Under** is one prominent, searchable picker, grouped by
level, offering planning cards of a higher level only
(`allowedParentKinds`, the mirror of the level rule) and nothing on a
goal or a subtask (a subtask's task is the group gesture's). A task's pane
lists its **subtasks** and takes one more, grouped under it the way the
Team and Me boards group a card (the parent's team, band and person come
along; the day is the parent's or today), from whichever board the pane
was opened on — Team, Me, Backlog or the Roadmap, where the new row stands
under its task at once. A planning card's pane draws what sits under it
as the Roadmap's tree rooted one level below the card ("Under it",
`PlanTree`, `roadmap.subtreeRows`): every level under it — plan items,
tasks and subtasks, each marked by its kind — with the Roadmap's rows (fold, title, owner with an inherited team,
status) and no timeline or dates. A row opens that card in the pane, by a
click or from the keyboard; a plan item's + adds under that row (the levels
it may hold, the next one first) and stays open for the next one; the add
row at the foot adds a direct child of the card (a goal an initiative or an
epic, an initiative an epic, an epic a task), the next level down first —
a task made there has no team of its own and stands under the plan item's
team, if it has one. A big tree starts folded below its first level. The
Under picker shows each
option's place — its ancestors, root first, small and muted with their
kinds, cut when long and in full in the tooltip — and finds a plan item by
its ancestors' titles too (`roadmap.parentOptions`). One × on every row
of the tree, and on the card itself,
asks the Team board's own question in the Team board's own dialog, with
the hierarchy's answers: delete, or only detach (ungroup, for a subtask).
The actions: mark as done and reopen (a task), delete — or
delete-or-unlink on a card that sits under something. The options and the children come from the
roadmap listing, fetched once per card when the pane needs it: the day
boards never deliver a planning card to look up. The rules live in
`web/src/roadmap.ts` and `web/src/owner.ts` with their cases beside
them; the components draw them.

The Roadmap opens the drawer with a click on a card. The Me, Team and Backlog boards SELECT a card on the first
click and open it on a click on the selected card (#290): the selection is
shared live as presence, so a lead selecting a card on the Team board is
seen on the person's Me board and the other way round, and each of the
three draws the others' marks on their cards.

## Owner

A task has one owner: a **team**, and a **person** on that team — or
neither. The Roadmap asks for it in one place (the Owner cell of a task
row, the Owner row of the drawer) and answers it with one gesture,
**`own`** (`POST /api/v1/views/{view}/cards/{uid}/actions/own
{team?, person?}`, MCP `own_card`), offered by every board that draws a
task — the Roadmap, Me, Team, Backlog, and `all`. A person is picked FROM a
team: a person without a team is refused (`ownerNeedsTeam`), a team
without a person is the whole answer, both empty takes the owner off.

What the answer **does** depends on whether the card is in work — a date
or a sprint — because the Roadmap lists the loose work (H9) and giving it
a home means putting it where a board draws it:

- a **person** files an **unplanned** card the way typing it into their
  Me board would: on the team, today, in the unplanned band, in the
  team's sprint, out of its backlog — so it stands on their Team and Me boards
  at once. A **planned** card changes hands and keeps its plan, its sprint
  included: a card in a sprint joins the new team's current one, as the
  Team board's team menu does; a card in **no** sprint — dated for a day
  still to come, where no sprint covers that day yet — stays in none, or
  next week's plan would be this sprint's unfinished work and the next
  carry-over would carry it. This is how a task is taken from the top of
  a backlog into work (`backlog.md`, K4).
- a **team alone** parks an **unplanned** card in that team's backlog
  with nobody on it and in no sprint (a parked card handed to another
  team stays parked, K5); a **planned** card moves team, keeps its plan,
  and loses its person.
- **nobody** takes the team and the person off: a planned card keeps its
  plan in the no-team group; an unplanned one is parked in the no-team
  group's backlog (K3) — the strip it used to go back to is gone.

Subtasks follow the task's team and person, as they do everywhere (S9);
a subtask is not asked, and neither is a review card — its person is
whoever was asked to review (`ownerFollowsLink`). A planning card's
answer is its own team and owner (H3, `ownership.md`).

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
`set_epic_project` → `set_scope_project`. (All of these, and the rest
of the Project board's surface, were removed on 2026-11-07 — see the top
of this document.)

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
  on the board — is dropped when the board is read (`DroppedParents` names
  it): the server's own deletes re-parent a planning card's children, but
  an epic deleted by a direct writer, or a torn move, would otherwise hide
  its whole subtree as subtasks of nothing; read as a root, the card stands
  where its own dates and team put it. A subtask
  that leaves its task for a planning card is pulled out of the group the
  way the ungroup gesture pulls it: one with no person of its own takes
  its task's. Pinned by
  board `TestASubtaskIsATaskUnderATask`, `TestADanglingParentIsDroppedOnLoad`, `TestParentOfAndChildrenOf`,
  `TestSubtaskHelpersStopAtPlanningCards`,
  `TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre`; service
  `TestTheLevelRuleGovernsTheParent`, `TestCarryOverCarriesATaskUnderAnEpic`,
  `TestASubtaskLeavingForAnEpicTakesItsTasksPerson`;
  web `hierarchy.test.ts` (`isSubtask`).
- H2 — the level rule: a parent is of a higher level (`board.Level`: goal >
  initiative > epic > task; an unknown type is a task); one parent;
  optional; a task may skip levels; a review card sits under nothing of
  its own (`ErrReviewCardLink`). Pinned by board
  `TestTheLevelsAreStrictlyOrdered`, `TestLineageIsEveryAncestorRootFirst`;
  service `TestTheLevelRuleGovernsTheParent`,
  `TestAReviewCardSitsUnderNothingOfItsOwn`; apiserver
  `TestACardResourceCarriesItsLineageAndHierarchy`.
- H3 — a planning card is drawn on the Roadmap and on no day board, is
  never carried or weighed, and has no stage, progress, band,
  backlog, sprint or review of its own (`ErrPlanningCard`,
  422 `planningCard`); its dates are its own, and it has no assignee — the people doing the work are on the tasks under it; it may carry a team and an owner (who carries it out, who answers for it, `ownership.md`), which place nothing — so it lives in the primary repository. Pinned by
  board `TestPlanningCardsAreOnNoDayBoardAndTasksUnderThemAre`; service
  `TestAPlanningCardHasNoDayBoardState`,
  `TestDeletingAPlanningCardReparentsItsChildren`; apiserver
  `TestTheRoadmapListsTheHierarchy`; web `hierarchy.test.ts`.
- H4 — ~~a project card stands in its column alone: no team, no owner,
  no band, no size, never a subtask, no subtasks, never a review; drawn on
  the Project board and the Roadmap only~~ **Removed** 2026-11-07 with the
  Project board. What remains is H15: every reader skips a project-card
  file and `projects/`, and nothing writes them. Pinned by gitstore
  `TestTheReaderSkipsTheProjectBoardsFiles`,
  `TestNothingWritesTheProjectBoardsFiles`,
  `TestAProjectCardsIDReachesNoWrite`,
  `TestMigrateSchema2LeavesAProjectCardAsItIs`; mcp
  `TestMCPHasNoProjectBoard`.
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
  an epic lives where its own team lives, and nothing under a
  planning card follows it between repositories. Pinned by board
  `TestAHierarchyLinkDoesNotPlaceTheChild`,
  `TestSubtaskHelpersStopAtPlanningCards`; service
  `TestAHierarchyLinkStaysInsideOneRepository`.
- H7 — a Roadmap create is a planning card (`type` required) or a task
  under one (no `type`, `parent` a planning card; born undated, nobody's,
  in no sprint — parked in the no-team backlog until taken into work, K3; under a task refused);
  a team, an owner, a zone and a size are refused by name; a day-board
  create refuses `type` (`ErrNotOnThisBoard`); the Roadmap draws the ×,
  which deletes the planning card and hands its children to its parent.
  Pinned by service `TestAPlanningCardIsBornOnTheRoadmapAndNowhereElse`,
  `TestDeletingAPlanningCardReparentsItsChildren`,
  `TestAGestureIsOfferedByTheBoardThatDrawsIt`; apiserver
  `TestARoadmapBornTaskStandsInTheNoTeamBacklogAndOnTheRoadmap`; server
  `TestTheCatalogNamesTheBoardsAndTheirGestures`; web `hierarchy.test.ts`
  (`createView`).
- H8 — schema 2: the five steps, idempotent, and what a pre-migration
  day still shows (steps 3 and 5 cut back to leave project cards and
  `projects/` untouched, 2026-11-07).
- H9 — the Roadmap also lists, below the tree, every open task with no
  planning card above it (in hand or parked); a finished one
  outside the hierarchy and a subtask whose
  parent is out stay out; the gate and the watch follow. Pinned by
  apiserver `TestTheRoadmapListsTheHierarchy`; server
  `TestAChangeUnderAPlanningCardReachesItsAncestorsOnTheRoadmap`.
- H10 — `own`: a team and a person, a team alone, or nobody; a person
  files an unplanned card like a Me-board create, a team alone parks it,
  a planned card only changes hands; a person needs a team; a subtask and a
  review card are refused (a planning card's answer is its own team and
  owner, O9); offered by
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
removed, the way B7 is. H4, G46 and the Project board's rows went on
2026-11-07, the same way. (D17, the week-planned card on its owner's Me
board, was not the slot rule and stayed — until the team backlog retired
the week of a task, 2026-11-03, `backlog.md`.)

## Open questions

- **TODO — the backlog is per team, and should be per value stream.** A
  shelf belongs to a team today (`board.InBacklog`, keyed by the card's
  `team`; the Backlog board draws the selected teams' backlogs as one list, `backlog.md`), and `own` with a team
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
