# Triage

Who does what, and in which week. It is the fourth board — Me | Team | **Triage** | Project | Process — and, like the personal board, it is not a new kind of thing: a card in the backlog is a weekly-plan card whose week has not come yet. Decided 2026-09-02 after the leads' meeting; the rules are to be pinned in [behavior-matrix.md](behavior-matrix.md) rows B1–B10 as they land.

## Why

The board has a day (Team, Me), a week (the Weekly panel), and a plan by columns (Project) — and no place for work that is accepted but not for now. So it goes to today's Unplanned and is done next, whatever else was planned: the queue is a stack. August's own history says so: in the three engineering teams 751 cards came in and 425 were closed, 16 were dropped, a third of the closings were cards not a day old, while over a hundred and fifty cards stood open for more than a month. What the leads asked for is a **pressure regulator at the intake**: a queue with a known throughput, where putting something in ahead of something else means moving that something else, and where the work nobody outside asked for keeps a slot of its own.

## The model

Three things, two of them already here.

- **Week.** A card on the board is a card with `week` (a Monday) and a plan band (`plan: wed|fri`, **by Friday** unless someone picks Wednesday) — exactly a weekly-plan card, placed in a week ahead. Nothing about the card says "triage"; the Triage board is the weekly plan's rows laid side by side. When the week arrives the card is on the Weekly panel; it is pulled onto a day at the daily sync the way plan cards are today; if it is still open when its week has passed it is **overdue** and stays in the week it was owed in, shown beside the current week's work (see [processes.md](processes.md), "Overdue, and why nothing moves"). None of that is new. What is new is one rule: **a card whose week lies ahead is on no day board** — not on Team, not on Me, not on the personal column — until its week begins. That rule is what makes the backlog a regulator rather than a list: a card placed three weeks out cannot be picked up on Tuesday because it caught someone's eye.
- **Lane.** Where the work came from — `lane: client | plan | internal`. `client`: someone outside asked (a bug from a client, a request, a UAT finding). `plan`: our own roadmap — a Project-board slot, a process turn, a card planned into a week from the plan. `internal`: work nobody outside asked for and nobody outside sees — the pipeline, API versioning, a watch-cache that will fall over next month. The third lane exists so that this work has a floor; it is the one that starves first, and August shows it did. The lane is **derived** where the card's own links already say it — a card in an epic column and a process iteration are `plan`, a subtask and a review card take the lane of the card they belong to and follow it — and **stored** on every other card. A card with no lane is not in any lane: it needs triage.
- **Capacity.** How many cards a team finishes in a week, in `teams/<id>.yaml`:

  ```yaml
  capacity:
    week: 80        # cards a week; absent = derived from the last four full weeks
    client: 30      # at most, percent of the week
    internal: 10    # at least, percent of the week
  ```

  Absent, `week` is **derived**: the team's cards with `doneAt` in the last four complete weeks, divided by four — read from the tree alone, no history needed. `doneAt` has been written since 2026-08-28, so the derived number is unreliable for four weeks after that; until a team has four weeks of it, or a number of its own, the board counts and shows nothing red. Capacity is the team's, never a person's: the meeting decided that most work can be done by most of the team, at a spread the average absorbs, and a per-person figure would be read as a rating. The unit is the card. That makes a **size norm** part of the process, not the tool: a card is at most two days of work, a bigger one is split into subtasks. Subtasks count on both sides — in the throughput and in the load — so the norm does not change the arithmetic, only what a card means.

August calibrates the defaults: about 80 closings a week in portal, about 20 in cozystack.

## Triage

**Needs triage** is a strip above the columns, one per team: every open card of the team that has **no lane or no week** and is not already being worked — not in the team's current sprint, not a subtask, not a review card. Triage is one action, **lane + week**; a card gets both on the Triage board by being dragged into a lane of a column. Cards created on the Project board or by a process are born triaged (their lane derives, their week is their slot's or their turn's); a card created on the Triage board in a lane is too; a card created on the Team board is on a day — being worked — and needs none; a card created through the API or MCP with neither lane nor week lands in triage. So the Jira sync's bugs land in triage, and the PM places them, or the sync sets `lane: client` itself and they land in a week.

The strip has an owner — the team's PM — and it ages in public: a card older than three working days in triage is marked, and the strip's header counts them. Cards in triage count toward no week and no lane; they are counted separately, as **unaccounted** work, in the team's load figure below.

## The board

- **Columns are weeks**, the current week leftmost, the horizon to the right — six weeks by default, more by scrolling; a card placed beyond the horizon is placed all the same and shows when the horizon reaches it. Time reads left to right; cards travel left as the weeks pass. Rows are teams: the team chips are the Team board's, and each selected team is a row of its own, because capacity and lanes are per team.
- **Each column has three lanes**, `client` above, `plan`, `internal` below, in that order because that is the order they are fought over. The lane header carries its count against its share; the column header carries the week's total against the capacity.
- **The current week's column** is the Weekly panel's content for this week — this week's plan cards, the debts owed from earlier weeks — plus the team's **current sprint as the Team board shows it today**, each card under its lane. A sprint card with no lane sits under no lane and counts toward the week's total; it is being worked, so it is not triage. Done cards of the week stay in the column, greyed: they used the week's capacity and the count must say so.
- **A future week's `plan` lane is partly projected.** Project-board slots whose span covers the week are there by their `week` and `day`. Process turns that will fall due in the week are drawn as **ghosts** from the task's cycle — computed, never stored, the way the process history dots are — so the week shows the load it will carry before the sweep files it. A ghost cannot be dragged; the task's start can.
- **Deadlines** (`projects/<pid>/deadlines/*.yaml`) are drawn at the boundary after the week they fall in, labelled with the project, for every project that has a card of the team on the board. A card dragged past a deadline of its own project is not refused — it is the PM's call — but the marker turns red and stays red while a card of that project stands beyond it.
- **Dragging.** Between columns sets `week` (a slot's week moves the slot, as on the Project board — same card, same field); between lanes sets `lane`; into the current week sets this Monday and the card is on the Weekly panel at once; into the triage strip clears `week`. A drop keeps the band unless the target column's Wednesday has passed; a debt dragged into the current week is a debt no more — it was owed, and it is re-owed.
- **The Me perspective** is a filter, not a board: the assignee chips narrow every column to one person, which is the engineer's own week ahead that the PM asked for.

## Limits and signals

The first cut warns; a later one blocks (below).

- **The week is over capacity** — total of the column (open and done, sprint cards included) above `capacity.week`: the column header turns red with the excess.
- **Inbound above its share** — the `client` lane above `capacity.client` percent of the week: the lane header turns red. This is the 30% rule made checkable: a client card may enter the current week only if the lane has room — or by the PM's explicit choice to exceed it, which the red then records.
- **Internal starved** — the `internal` lane below its floor for the current week and the two before: the lane header turns amber. An empty internal lane is a signal the same as a full client lane, and, by August, the more frequent one.
- **Triage ageing** — a card in the strip older than three working days: marked on the card, counted in the strip header.
- **The side door.** A card created straight on the Team board has no lane and so escapes the client share; the week's total still counts it, and the lane chip on the card is where it is given one. Closing that door is a process rule (the meeting's own: P2 does not enter the current week), not a refusal in the tool.
- **Load** — in the team's row header: `booked N weeks · M untriaged` — every placed open card of the team, in whatever week, divided by the capacity, plus the strip's count. It is the figure the PM asked for — "how far ahead are we booked" — and the one to name when a date is promised.

None of these is stored. They are read off the cards and the roster on every render, and the API returns them with the listing so an agent sees the same red a person does.

## Rules to pin

Rows for [behavior-matrix.md](behavior-matrix.md), to be added with the tests that pin them; the tests are written first, one failing case per row, in `pkg/board` (`backlog_test.go`) and `pkg/boardservice` (`triage_test.go`), with the vitest mirror beside `weekly.test.ts`.

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| B1 | A card with `week` ahead of today's week is on no day board — Team, Me, personal — until that week begins; from its Monday it is on the Weekly panel and shows on the day boards by the rules they have today. A card with `week` set and no band is placed by Friday | board.TeamGrid / MeView / WeeklyPlan; boardservice.place | board `TestACardPlacedInAWeekAheadIsOnNoDayBoardUntilItsWeek` |
| B1a | Placed back in the CURRENT week, a card standing on no day joins the sprint being worked (the team's active sprint, else its current one, else the week itself) — the round trip out to a week ahead and back must not leave the card holding a week and standing on no board. A card that already has a day keeps it; a slot and a personal board's card are left alone | boardservice.Place | boardservice `TestPlacingBackInThisWeekPutsTheCardOnTheDayBoardAgain` |
| B2 | `lane` derives for a card in an epic column and for a process iteration (`plan`), and for a subtask and a review card (the card they belong to, followed on change); it is the stored value on every other card; a card has at most one lane and the derived one wins over a stored one | board.LaneOf | board `TestALaneIsDerivedWhereTheLinksSayItAndStoredElsewhere` |
| B3 | Needs triage = an open card of the team with no lane or no week that is not in the team's current sprint, not a subtask, not a review card, not sent to review (its work waits on a reviewer, not on a week); a card created on a day board, on the Project board, in a process, or on the Triage board in a lane never is; a create through the API or MCP with neither lane nor week is | board.NeedsTriage; boardservice.CreateCard | board `TestNeedsTriageIsACardNobodyPlaced`; boardservice `TestACreateWithNeitherLaneNorWeekLandsInTriage` |
| B4 | Triage is one action: placing a card in a lane of a week sets both and takes it out of the strip; dropping it back into the strip clears the week and keeps the lane | boardservice.Place / Untriage | boardservice `TestPlacingACardTriagesItAndTheStripLetsItGo` |
| B5 | The current week's column is the Weekly panel's content for the week plus the team's current sprint as the Team board shows it; a sprint card without a lane is under no lane and counts toward the total; done cards of the week stay in the column and count | board.BacklogWeek | board `TestTheCurrentWeekIsThePanelPlusTheSprint` |
| B6 | A future week's plan lane holds the slots whose span covers it and a ghost for every process turn due in it, computed from the task's cycle and never stored; a ghost is not a card (no id, no drag) | board.BacklogWeek + board.ProcessTurnsDue | board `TestAFutureWeekProjectsTheTurnsItWillCarry` |
| B7 | Capacity is the team's: `capacity.week` if set, else the team's cards with `doneAt` in the last four complete weeks over four; with fewer than four such weeks and no number set there is no limit and nothing red; a person has none | board.Capacity | board `TestCapacityIsSetOrDerivedFromFourWeeksOfDoneAt` |
| B8 | Signals are derived on every read, never stored: the week over capacity (open and done, sprint cards included); the client lane over its share; the internal lane under its floor for three weeks running; a triage card older than three working days; the team's load = placed open cards over capacity, with the untriaged counted apart | board.TriageSignals | board `TestSignalsReadOffTheCardsAndTheRoster` |
| B9 | Dragging between columns sets `week` (a slot's week moves the slot); between lanes sets `lane`; a card dragged past a deadline of its project is placed, and the deadline reads red while a card of the project stands beyond it | boardservice.Place; board.DeadlineBreached | boardservice `TestAWeekDropMovesTheSlotAndAPastDeadlineReadsRed` |
| B10 | A moved card keeps its band unless the target week's Wednesday has passed; a debt placed into the current week is owed there | boardservice.Place | boardservice `TestAPlacedDebtIsOwedAgain` |

What [dates.md](../dates.md) gains, under a heading of its own between the Weekly and the Subtasks sections:

> ### Triage — a card's week
>
> A card with a `week` ahead of the current one is **placed**, not scheduled: it is on no day board (Team, Me, the personal column) until its Monday, whatever its dates say, and on its Monday it is on the Weekly panel like any plan card — pulled onto a day from there. A placed card with no band is owed by its week's Friday. A placed card still open when its week has passed is a debt and is not moved (see Overdue). The Triage board shows the weeks side by side; the current week's column is the Weekly panel plus the current sprint, and a future week's plan lane is partly projected — Project-board slots by their span, process turns by their cycle, as ghosts.

## Surface

- `GET /api/v1/cards?view=triage&team=<name>[,<name>]&from=<monday>&weeks=<n>` — every placed card of the teams with `week` in `[from, from + weeks)`, the current week's sprint cards, the triage strip (`status.triage: true`), the ghosts (`kind: Ghost`, no uid), each card with `status.lane` (derived or stored) and `status.debt`; and per team a `capacity` block — `{week, client, internal, derived}` — and a `signals` block with the five above. The watch takes the same selector.
- `POST /api/v1/cards` and `update_card` accept `lane` and `week`; `take_into_plan` is unchanged (it places into the current week with a band, as before). A `lane` on a card whose lane derives is refused (422): the links say what it is.
- `PATCH /api/v1/teams/{id}` with `capacity`; MCP `set_team_capacity`. `GET /api/v1/board` carries each team's `capacity` with `derived: true` when it is.
- MCP: `list_cards view=triage` with the same selector; `place_card uid week [lane] [band]`; `untriage_card uid`.
- UI: the Triage tab, the columns, lanes and strip above; the same team chips as Team; assignee chips as a filter; the deadline markers; the row header with the load figure. Card detail is the existing one — the lane is one more chip on it, editable where it is stored.

## Later, and deliberately not now

- **Blocking.** A drop into a full week asks which card moves right, and the tool cascades — week by week, never past a deadline of the moved card's project: the cascade stops there and the deadline is what turns red. The first cut warns, so that the limits are trusted before they refuse.
- **Client partitions.** Inside the `client` lane, grouping by project (a client is an epic, or a project), each group with a share of its own from the contract's hours. The lane is built to group; the shares wait until the lane limit has held for a month.
- **A client's read.** The same board, filtered to one project, read-only, shared: the frozen provider of the day records already does this for a past day. After the internal backlog has held its weeks for a month, and one client at a time.

## What was decided against

- **A board entity of its own.** A file, a state card, a status: every one would be a second place for a card to be, beside its week. The week is the place; the board is a view over it. The one new roster fact is capacity.
- **Days instead of weeks.** A date per card, cascading on every slip, is the routine nobody keeps; a week with a limit shifts once a week and the daily sync picks the day — which is the planning the leads described.
- **Deriving the lane from the project name.** Freedom is a project (their features, in columns) and a client (their bugs, from Jira); the project name says nothing. The epic column does — a card someone filed under a column is planned — and everything else is stored.
- **A project's default lane** (`inbound: client` for lane-less API creates). Wanted for the Jira sync, which can set the lane itself; a default that fills in silently is one more thing a writer following the spec would have to know.
- **A migration that lanes existing cards.** Blank lanes go to triage; the strip is full on the first day and the PM grooms it once — which is what the strip is for. A schema bump would make every replica refuse the repository until upgraded, for a one-time convenience.
- **Reading priority off the title** (`[P1]`, `UAT`). A title is not a field. The rule "P2 never enters the current week" is a person's rule, and the lane cap is how the board holds them to it.
- **Per-person capacity.** Decided at the meeting: the team's throughput is enough for planning, and a per-person figure reads as a rating. The assignee filter answers "what is on me this week" without it.

## Writing the repositories directly

A tool writing the repositories directly learns three things: `lane:` on a card (`client`, `plan`, `internal`; absent = needs triage; ignored on a card in an epic or with a task, where it derives), `capacity:` in `teams/<id>.yaml`, and that `week:` on a card may name a Monday ahead — with `plan: wed|fri`, by Friday if absent — and such a card is on no day board until then. Schema stays 1: older servers keep the keys they do not read. See [plugin-impact.md](plugin-impact.md).
