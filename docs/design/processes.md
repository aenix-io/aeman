# Processes

A process is recurring work the team wants to keep doing and to see
whether it actually does: publishing articles, collecting payment from
each client, the monthly security review. It is the third board after
Project, and it reuses the board's parts rather than adding new ones.

## Model

A **process** is a group of **tasks**, the way a project is a group
of epic columns. Both are declared by hidden state cards, so their order
is the board's own order and their edits ride the same write queue:

- `aeman:process-state` — one per process. `Process` is its name,
  `Project` the project it belongs to (a process is part of a plan, and
  the Process tab filters by the same project chips as the Project tab).
- `aeman:process-task` — one per task, the thing an iteration is
  copied FROM. `Process` names its process; `Recurrence` is its cycle;
  `Start` is the calendar anchor; `Team` the team whose weekly plan the
  iterations land in; `Assignees` the standing owner, if any. Title and
  description are the iteration's title and description.

An **iteration** is an ordinary weekly-plan card on the `recurrent`
stage, with `Task` naming the task it came from. It is filed in
the team's plan for its week, assigned to the task's owner, and from
then on it is the team's card: renamed, described, reassigned, it stays
as it was made. The NEXT iteration is copied from the task again —
that is the point. (Today's recurrent carry-over copies the previous
card, so a rename propagates forever; a task does not have that
problem.)

## Cycle

The cycle lives on the **task**, not the process: a process of
collecting payment has one task per client, and clients pay on
different schedules. Cycles are the existing ones — every week, every
two weeks, every month, every quarter — counted **on the calendar from
the task's start**, not from when the last iteration closed. A
task started on 3 March with a monthly cycle is due in the weeks of
3 April, 3 May, … whatever happened to the March card.

An iteration is spawned by the server's weekly sweep, for every task
whose cycle puts a due date in the current week — and at once when a
task is created or changed into being due this week. The sweep runs
after each background refresh of the board, with the token of the
session that keeps the board fresh; nobody presses anything, and there
is no endpoint to press: the UI has no such button, so neither do the
API and the MCP (ADR 0002).

## When the previous iteration is still open

Default: **do not spawn** — the open card is the process, and it simply
goes overdue. That keeps a stuck process as one stuck card rather than a
growing pile.

Per task, `Accumulate` flips this: spawn regardless, so unpaid
months pile up as separate cards. Collecting payment wants this; writing
an article does not. The flag belongs on the task for the same
reason the cycle does.

## What the tab shows

Processes, grouped by project (the same chips as Project; the selection
is shared between the two tabs). Each process expands into its
tasks, each task shows its cycle, team, owner, and its
**history**: a row of dots, one per expected iteration in the recent
past — green when that iteration closed inside its cycle, red when it
did not, grey while it is still running. One glance says whether the
process is alive.

The dots come from the iterations themselves (`Task` + the card's
dates and completion), so nothing is stored for the report.

## API and MCP

`add_process` / `delete_process` / `rename_process` (a process is
deletable while it has no tasks), `add_process_task` /
`update_process_task` / `delete_process_task`, and
`list_processes` returning the structure with each task's history.
Iterations are plain cards; nothing new to list them with.

## Overdue, and why nothing moves

A card that came from a plan — a Project slot, a process turn, a
weekly-plan card — and is still open past the day it was owed by is
**overdue**. The day is the card's own: a slot's end date, a turn's
week, a plan card's band (by Wednesday means by Wednesday). It is
derived on every read (`board.Overdue`), never stored, and it shows as
a red line on the card's left edge wherever the card appears: the day
board, the weekly plan, the Project board. Agents read it as
`status.overdue`.

An overdue card is not moved. It stays in the week it was owed in —
that week is the record of what was missed — and shows on the current
week's panel beside that week's own work. Carry-week used to move it
(and, for a slot, stretch its end date to the target week, rewriting
the very date that said it slipped), so the board forgot that anything
had. Now the server's weekly sweep only files process turns and reseeds
finished recurrent cards; debts it counts and leaves be. Closing the
card takes the line away and leaves the card where it was — done, late,
in its own week.
