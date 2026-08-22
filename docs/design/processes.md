# Processes

A process is recurring work the team wants to keep doing and to see
whether it actually does: publishing articles, collecting payment from
each client, the monthly security review. It is the third board after
Project, and it reuses the board's parts rather than adding new ones.

## Model

A **process** is a group of **templates**, the way a project is a group
of epic columns. Both are declared by hidden state cards, so their order
is the board's own order and their edits ride the same write queue:

- `aeman:process-state` — one per process. `Process` is its name,
  `Project` the project it belongs to (a process is part of a plan, and
  the Process tab filters by the same project chips as the Project tab).
- `aeman:process-template` — one per template, the thing an iteration is
  copied FROM. `Process` names its process; `Recurrence` is its cycle;
  `Start` is the calendar anchor; `Team` the team whose weekly plan the
  iterations land in; `Assignees` the standing owner, if any. Title and
  description are the iteration's title and description.

An **iteration** is an ordinary weekly-plan card on the `recurrent`
stage, with `Template` naming the template it came from. It is filed in
the team's plan for its week, assigned to the template's owner, and from
then on it is the team's card: renamed, described, reassigned, it stays
as it was made. The NEXT iteration is copied from the template again —
that is the point. (Today's recurrent carry-over copies the previous
card, so a rename propagates forever; a template does not have that
problem.)

## Cycle

The cycle lives on the **template**, not the process: a process of
collecting payment has one template per client, and clients pay on
different schedules. Cycles are the existing ones — every week, every
two weeks, every month, every quarter — counted **on the calendar from
the template's start**, not from when the last iteration closed. A
template started on 3 March with a monthly cycle is due in the weeks of
3 April, 3 May, … whatever happened to the March card.

An iteration is spawned by `carry_week` (and by the same background
sweep that runs it), for every template whose cycle puts a due date in
the target week. So the weekly plan fills itself; nobody presses
anything per template.

## When the previous iteration is still open

Default: **do not spawn** — the open card is the process, and it simply
goes overdue. That keeps a stuck process as one stuck card rather than a
growing pile.

Per template, `Accumulate` flips this: spawn regardless, so unpaid
months pile up as separate cards. Collecting payment wants this; writing
an article does not. The flag belongs on the template for the same
reason the cycle does.

## What the tab shows

Processes, grouped by project (the same chips as Project; the selection
is shared between the two tabs). Each process expands into its
templates, each template shows its cycle, team, owner, and its
**history**: a row of dots, one per expected iteration in the recent
past — green when that iteration closed inside its cycle, red when it
did not, grey while it is still running. One glance says whether the
process is alive.

The dots come from the iterations themselves (`Template` + the card's
dates and completion), so nothing is stored for the report.

## API and MCP

`add_process` / `delete_process` / `rename_process` (a process is
deletable while it has no templates), `add_process_template` /
`update_process_template` / `delete_process_template`, and
`list_processes` returning the structure with each template's history.
Iterations are plain cards; nothing new to list them with.
