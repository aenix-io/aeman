# Board dates, sprints, and card visibility

Source of truth for how aeman places cards on days, what a "sprint" is, and the
visibility rules for the Team / Me views and what a team's backlog holds, plus
create, Carry Over, and send-to-next-day. The date logic is subtle — **keep this file in sync with the
code** whenever the rules change.

Code that implements these rules:
- Frontend: `web/src/teamgrid.ts` (the Team board's own rule), plus
  `web/src/components/{TeamBoard,MeBoard,Card}.tsx`, `web/src/date.ts`,
  `web/src/sprint.ts`, `web/src/stages.ts` (`finishedOn`, `deferredPast`,
  `doneAtAfter`).
- Go: `pkg/board/{filters,date,sprint}.go`, `pkg/boardservice/service.go`,
  `pkg/gitstore/asof.go` (the day a snapshot says its work was finished on).

## Two dates per card

A card carries **two independent dates**:

- **Sprint Start** (`sprintStart`) — the **sprint** the card belongs to (the
  sprint's start date). Carry Over moves this; the Team board groups a materialized
  card by it.
- **Start** (`startDate`) — the card's **scheduled day**: the day it starts
  showing and "materializes" into its sprint. Set to the viewed day (`selectedDate`)
  the card was created on; defer ("+1 day" / "+1 week") pushes it forward,
  counting from today. The card's age badge uses `createdAt`, not this field.

Also:
- **Day** (`day`, date) — the card's **end** date. With a start it bounds a
  visible **range**: the card shows on every day of `[startDate, day]` in both
  views. Set on create to the same day as `startDate` (a one-day range); the
  calendar's end field edits it.
- **Parked** (`parked`, bool) — the task is in its team's **backlog** rather
  than in work (below). A task has **no week** (`docs/design/backlog.md`,
  K7): the Triage board that scheduled tasks into weeks is retired, a week a
  file still carries is read as nothing.
- Hidden per-team **`aeman:sprint-state`** card: `Sprint Start` = the team's
  **current** sprint date, `Start` = its **previous** sprint date. Read via
  `currentSprint(team)` / `previousSprint(team)`.

`startDate` and `sprintStart` differ whenever a card is created on a later day of
its sprint, or deferred with "+1 day" / "+1 week" (which pushes `startDate` past
today while the card stays in its sprint). A "**next sprint**" create has **no
`sprintStart` at all** until a Carry Over adopts it (see Create / Carry Over).

## Concepts

- A **sprint** is a team's batch of work anchored to a start date; a team's
  current/previous sprint live on its sprint-state card.
- **`activeSprint(team, day)`** = which sprint was current on a given day:
  `current` if `day >= current`, else `previous` if `day >= previous`, else none
  (we only track the last two sprints).
- **Carry Over** advances the team's sprint to today (current → previous) and
  pulls its **unfinished** cards forward (sets their `sprintStart = today`). Done
  cards stay on their sprint. After it runs, the board jumps the viewed day to today.
- **Carry Over pressed again** the same day moves no pointer and carries
  nothing: it hands back to the previous sprint the finished cards of today's
  sprint that were in work before today — the leftovers a planning meeting
  closes after the carry-over (D20). A card created today, or taken out of the
  backlog today, stays: it has no earlier sprint.

## Rules

### Create
- `sprintStart = currentSprint(team)` — the card joins the team's **current
  sprint** (which may be an earlier day than today).
- `startDate = selectedDate` (the viewed day) — its scheduled day is the day in
  view.
- First sprint: if the team has none yet, creating records the viewed day as its
  first sprint on the sprint-state card.
- **Ahead of the sprint** (Team board, `selectedDate > currentSprint(team)`):
  the day is ambiguous — a later day of the running sprint (two-day sprints) or
  the next one (daily sprints) — so the board **asks**. "Current sprint" keeps
  the rules above; "**next sprint**" creates the card with **no `sprintStart`**
  (`noSprint` on the create API): it lives on its own day only and the first
  Carry Over whose day reaches its `startDate` adopts it (see Carry Over). It
  never appears on the old sprint's window.
- **Me board creates never ask**: an engineer's own card always joins the
  current sprint, so a lead reviewing the day sees what came up mid-sprint.

### Team view — a card's days (`selectedDate`)

The Team board answers ONE question: what are these people working on that day? A card is in hand on a day when it is that team's — the team it stands under, its own else its plan item's (`ownership.md` O13) — is a **task** (a goal, an initiative or an epic is the Strategy board's and on no day board — `docs/design/hierarchy.md`), is **not a subtask** (a subtask — a task whose parent is a TASK — rides with its parent; a task under an epic is not one and stands here like any task), is **not parked** in a team's backlog, is **in work** — it has a date or a sprint, since a task with neither is parked in its team's backlog, which is not a day's work — is **open or was finished on that very day** (`doneAt`; a card finished yesterday belongs to yesterday) **while its sprint was the team's current one**, and was **not deferred past that day**.

That is the whole rule, and every card meets it the same way. It replaced seven layered ones — the week's own work, the sprint's start day, the card's own scheduled day, the range between its dates, the days of sprints it had passed through, and two special cases for deferral — which between them managed both halves of being wrong: work nobody was doing appeared on the day, and a card scheduled for last Tuesday and never finished fell off the board entirely, because no rule reached it any more.

Four consequences are worth saying out loud:

- **Open work does not stop when its plan does.** A card's end date says when the work was due to finish, not when it leaves the board: past that day it goes on standing there until somebody finishes it or puts it off. Work that ran over is the work most in need of being looked at.
- **Today is a state, a day ahead is a plan.** The rule above holds up to today. A day still to COME shows only what somebody actually put on it: a card whose own dates reach that day. Today's unfinished work is today's problem, not tomorrow's plan — without that bound every open card stood on every future day, and a month out was simply the team's whole backlog. The Me view is the same rule asked of one person's cards (below).
- **The CURRENT sprint accumulates all its work on every one of its days.** A lead opens the sprint with the team from whichever day they are on — the sprint's own day (the "current sprint" jump), a day in the middle, or today — and each of those days holds everything the sprint has been: the work it opened with, the work typed into it since (most of a sprint is created inside it), and the work already **closed**. A day that showed only what is still open answers "what is left", which is not the question that review asks; and a lead who opened a middle day of the running sprint before carrying it over used to see the closed work missing, which is the bug this rule fixes. **Carry Over is what clears the closed work**: it leaves a finished card on the closing sprint (its `sprintStart` is not moved), so the moment a new sprint opens, that done work belongs to the now-PREVIOUS sprint and drops off today — even a card finished **today** is left behind on the sprint it was carried past, and a previous sprint keeps its closed work only on the days it ran (a record). (A finished card stands on its own `doneAt` day only while its sprint is current — next item.) One other thing leaves the current sprint too, the act of taking a card OUT of it: work **deferred past today** — sent to tomorrow, a card leaves today's sprint at once and arrives tomorrow; sent three days out, the sprint that opens tomorrow starts without it. Deferring is a hold, not a removal: the card is out of the sprint only while its day has not come, and the morning it arrives it is in the sprint again.
- **Finished work belongs to the day it recorded** (`doneAt`) and to no other — **and only while its sprint is the team's current one** (D21). A done card whose sprint the team has carried past is that sprint's record, not the day's: the leftovers a planning meeting closes **before** pressing Carry over were worked in the old sprint, and the meeting day — read back later, its pointers say the new sprint was current — does not hold them, though they were closed on it; the old sprint's days do (open on each evening's record until they were closed). The same goes for a card left out of a carry-over and closed days later. A card with no sprint has none to belong to and stands on its `doneAt` day as before. A card whose writer recorded nothing stands on no day here — nothing is guessed out of its dates, which are a plan rather than a record. Nothing is lost by that, because a day already gone is not answered by this rule at all: it is served as a snapshot, and there the day's own commits say what they finished (below).

The Project board and its **project cards** (cards filed under a column, a `scope`) were removed on 2026-11-07 (`docs/design/hierarchy.md`). A repository may still hold such files; no board draws them and no listing serves them.

### In work or in the backlog

A **task** — a card with no `type`, not a subtask, not a review card — is in exactly one of two places (`docs/design/backlog.md`, K1):

- **In work**: it has a date or a sprint. It stands on the Team and Me boards by the rules above.
- **In its team's backlog** (the team it stands under: its own, else its plan item's, O13): it has neither, and it is `parked`. It stands on **no day board**. The no-team group has a backlog like any team, and nothing declares one.

There is no third place. The Triage strip — open tasks nobody had dated, weeked or parked — is gone: every door that used to leave a task there parks it instead (K3) — a Strategy-born task given no dates, `own` with nobody, the × that unassigns a card (parked, no person, no day, no sprint). Taking a task into work clears `parked` in the same write (K4), and the task leaves the backlog at once. In the SPA that is the card's owner picker — `own` with a person: it lands today, in the unplanned band, in the team's sprint; over the API and the MCP a date, a sprint or a defer does the same (the drawer shows a task's dates read-only, and the Backlog and Team boards are separate tabs). An open task that a write leaves with no date and no sprint is parked in the same request (K1). Clearing a date takes nothing into work. A parked card that carries a date anyway (only a direct write can make one) is drawn in its backlog and on no day board.

The **Backlog** board lists each selected team's parked open tasks — no planning card, no subtask on its own, no review card, nothing finished — the reviews nobody has taken first, then in the Strategy board's order, the tasks with no plan item last (K2, K10, K14). A task's **week** is gone (K7): nothing schedules a task for a week, and the week gates the day boards used to apply (a week ahead held a card back; the Me board drew a week-only card once its Monday came) are gone with it. Schema 3 parked the tasks an older server had left with only a week, or with nothing at all (K8).

### A past day is shown as it was (the snapshot)

The rules above place TODAY's cards on a day. That is what a day-lens is: dates decide where a card belongs, and a card finished this morning reads finished on every day you flip to. Going BACK on the Me or Team board answers a different question — what did the board look like then — and gets a different answer: the board **as it stood** when that day ended, read from the repositories' own history (the tree at that day's last commit). Every field is the day's own; a card created since is simply absent.

- **Whether a day is over is each TEAM's own answer**, not the calendar's. A sprint lays itself out on its own day — that is where the lead works it from, and where a card created today lands (its `sprintStart` may be days old) — so a team still inside that sprint keeps the live board, while a team whose sprint has moved past the day shows what it held that evening. One day, one screen, two moments: on a board of several teams the columns of the settled teams are records and the rest is today's work, and each card says which it is (`status.asOf`). A team whose sprint has stood since July therefore keeps every day since July live for itself alone — that sprint IS open, and Carry Over is what closes it.
- Only a **past** day, and only the day boards (Me, Team). Today is still happening, tomorrow has not, and the Backlog and the Strategy board are not day boards.
- A record is not a workspace, and it looks like one: flat and grey, with no control on it, and the detail pane opens it read-only. It does not drag; a write to it is refused in the browser AND by the server (409 on `X-Aeman-As-Of`), so a UI path that forgets never writes today's board from a picture; today's traffic is not applied over it. The live cards beside it stay entirely workable, and so is ADDING to a team whose sprint is still open: every day of an open sprint is that team's to add to — the day it began included — and only a team the day is over for loses its add box. **Reaching for it anyway takes you off it**: the day jumps to today and the team whose card was touched is offered its carry-over (the same confirm the button shows, quiet when that sprint is already today) — a lead reaching into a day that is over usually has a sprint that was never closed. The write is not replayed onto today: it was aimed at a picture of the past.
- **A day gives back what the × took off it.** The × on a team card DELETES it — the file goes — and the day it stood on holds it all the same, in the state it went in: the record of a day is the tree that day ended with **plus every card the day itself removed**, read from the commit that removed it. Today's board is unchanged: taking the card off today is exactly what the × is for. (The × used to DEMOTE a worked card into the previous sprint instead, and the record then gave it back by `leftAt`, the day it was taken off. Cards demoted before this shipped are still where that left them — alive in an old sprint — and are still given back by that mark; nothing writes it on a team card any more.)
- The record carries that day's **sprint pointers** with its cards — the view rules place a card by its team's pointer. The **roster** (teams, customers) stays today's: a team created since shows on the record of an older day.
- **A day says which of its cards were finished on it.** A day board draws finished work on the day it recorded (`doneAt`) and no other — and the record of a day can supply that day where the card's own writer did not: the day **opened** with the card unfinished and **closed** with it done, so the work was finished inside it. It is filled in on the way out of the history, never stored, and what the card says itself always wins. The evidence is that change and not the day's list of names — a commit names what it touched, and a rank rebalance or a carry-over touches cards by the hundred without finishing any of them. A day whose own morning cannot be seen — the boundary day of a clone whose history is cut — claims nothing: "I do not know when this was closed" is a true answer where an invented day is not.
- The history has an edge. The server keeps a horizon (`--history`, two weeks by default) and deepens on demand up to `--history-max` (a year); a day behind that is refused (410) rather than answered with the oldest state at hand.

### Me view — one person's day (`selectedDate`)
- **The Me board is the Team board for one person** (D6): a card of the
  viewer's (or a parent whose subtask is theirs) is on their day exactly when
  the Team board draws it in the column of the team it stands under — the
  rule above, every clause of it, by that team's sprint pointer. The same
  debt (open work that ran over, a card left out of a carry-over, an open
  sprint-less card), the same closed work (every day of the current sprint;
  the `doneAt` day while its sprint is current), the same plan for a day
  ahead, and a past day served as the same record.
- It used to be a rule of its own — a card on the days of the sprints it
  spanned, across its date range, and on today if open — and the two boards
  drifted: a card closed on a day of a later sprint stood on the Team board
  that day and on no day of its owner's board, and an open card a carry-over
  left behind was debt in the Team board's past days and nowhere on the Me
  board's.
- The board's Team filter (the dropdown the Team board, the Backlog and the
  Me board share, `ownership.md` O12) narrows it to the chosen teams — the
  no-team group being one. When it hides any of the day's cards, a yellow
  "N more tasks" button beside it says how many, counted over what the day
  would draw with no filter — rows and the subtasks nested under them — and
  clears it. The Me board has no Customer
  filter: a customer chosen on the Team board or the Backlog hides nothing
  here.

### Carry Over
- No-op if the team's sprint is already today.
- Else: sets sprint-state to (current = today, previous = old current), and for
  every **unfinished** card of the **closing sprint** (`sprintStart == old
  current`), sets `sprintStart = today`. A card that is **not on today's
  sprint** — older, or demoted back by the × as it used to be — stays where it
  is, so removing a card from the current sprint is final and it never
  boomerangs back. Complete
  cards (done, or 100% with no stage) stay put too. Then the view jumps to
  today.
- A carried card keeps its `startDate`, so it stays visible on the days of the
  sprint it came from (see the Team / Me rules): Carry Over adds it to the new
  sprint without removing it from the previous one.
- **Adoption**: unfinished **sprint-less** day cards ("next sprint" creates)
  with `old sprint < startDate <= today` also get
  `sprintStart = today` — they join the sprint this Carry Over opens, which is
  what "the next sprint" meant when they were created. Ones still ahead of
  today wait for a later Carry Over; ones scheduled **before** the closing
  sprint (old sprint-less strays — report cards, legacy cards) are never
  dragged in.

### Defer ("+1 day" / "+1 week")
- The per-card control pushes **`startDate`** forward — counting from **today**
  (or from the card's already-deferred slot, so presses stack): `+N` sets
  `startDate = max(today, startDate) + N`. The card **stays in its sprint**
  (`sprintStart` untouched), so the day its own day comes it is back in that
  sprint — deferring is a hold, not a removal.
- A card **created today** (0d) has no history worth keeping: deferring it
  relocates it fully — `sprintStart` moves to the new day too (and a stale end
  date is pulled along), so it leaves the current sprint entirely.
- While `startDate > today` the card is hidden between today and that day in Me
  and Team — **its sprint's own day included**, because deferring is the act of
  taking a card out of the sprint in progress. It shows again from its new day
  on, and the sprint's day has it back then too. A day already gone still holds
  it as it stood: that day is served from the history (the snapshot), which is
  where "where the card came from" now lives.
- Carry Over still sweeps a deferred card's sprint forward (its `sprintStart` is
  in the past), but the future `startDate` keeps hiding it until its day comes.

### Recurrent cards
- The **refuse** stage is a person's answer on their own board: "I am not
  doing this". The card is not taken off the board and not hidden — it goes
  back to the team's grid wearing the darkest bar there is, for the lead to
  answer (put it back to work, or take it off). Only the person the card is
  on may set it. Its progress holds the 10–90 band, like review and locked.
- The **recurrent** stage marks a repeating task. Unlike review/locked its
  progress spans the full 0–100% (no clamp), and 100% counts as **complete**.
- **Carry Over**: an unfinished recurrent card carries like any other card; a
  **finished** one (100%) stays behind and is **reseeded** — a fresh copy is
  created in the new sprint with the same title and description, no notes,
  at 0%, recurrent again.

### When work is owed

A card still open past its end date is a **debt**. A debt is never moved, and
the day boards go on drawing the work until it is finished. Nothing marks a
task late: its own dates are the day board's planning, and planning is what
the next sync redoes — marking a task late for still being open paints most of
a normal board red, and a mark that is everywhere says nothing where it
matters. (The overdue mark, `status.overdue`, was worn by Project-board slots
alone and went with the Project board on 2026-11-07. The Backlog shows the end
date of a task's nearest planning card as a due date — a reading of the
hierarchy, not a mark.)

### Subtasks (grouped cards)
- A task whose **parent is a task** (the `parent` field, one level deep) is a
  **subtask**: it never appears as a row of its own in Team/Me — the views
  deliver it alongside its parent and the UI nests it under the parent's
  expandable list. The Me board's Team filter applies to subtask rows too.
  `parent` is also the hierarchy's link (`docs/design/hierarchy.md`): a task
  whose parent is a goal, an initiative or an epic is NOT a subtask — it is
  on the day boards like any task, inherits nothing and rides nowhere; the
  web reads which of the two it is off `status.lineage`, whose last entry
  is the parent.
- A subtask is a normal card in every other way: own description, own log and
  notes, own stage/progress, own assignee. It can be pulled back out as a
  standalone card at any time (clear the parent).
- **Derived progress**: while a card has subtasks its bar derives from them —
  the average of the subtasks' effective progress (done = 100) scaled into
  0–90%. The final done / 100% is always a human decision on the parent, and a
  card **cannot be done while it has open subtasks**.
- **Grouping** a card under a parent syncs it into the parent's sprint
  (`sprintStart` copied) and moves it onto the **parent's team**.
- **A subtask's person always follows its parent**, the way its team does:
  grouping hands the child over to the parent's assignee, a direct change on a
  subtask snaps back to the parent's, and re-assigning a parent hands the whole
  family over (unassigning it unassigns them all). A family that drifts apart
  lands on two people's Me boards — the Me view admits a card when you own one of
  its subtasks, so one stray child drags the parent and every sibling onto a
  board they are not part of.
- A subtask's team always follows its parent: changing the parent's team
  cascades to its subtasks (sprint pointer included), and a direct team change
  on a subtask snaps back to the parent's team.
- **Deleting a parent releases its subtasks**: they are work items in their
  own right, so they return to the board as standalone cards (team, sprint and
  dates kept) instead of being deleted or orphaned with the parent.
- **Carry Over orients by the parent**: an unfinished parent that carries
  drags its **open** subtasks into the new sprint (even ones whose own
  team/dates would not qualify — they ride along). A **completed** subtask
  stays in the sprint it was finished in — it keeps showing on that sprint's
  days under the parent, and the parent's derived bar still counts it
  (DerivedProgress scans all children regardless of sprint). Subtasks whose
  parent does not carry stay put; subtasks are never selected on their own.
- A subtask **scheduled for the future** (startDate past today — the calendar
  or defer) is hidden under its parent until its day arrives, like any
  deferred card; the next Carry Over drags it (it is open) but the future
  startDate keeps hiding it until the day comes. The hiding is the CLIENT's
  rendering rule — the views deliver ALL of a parent's subtasks, so the
  client's derived-progress math always matches the server's.

### Calendar (explicit dates)
- The date picker on a card moves its **real dates**: `startDate = start` and
  `day = end` — a genuine relocation (no history kept, unlike defer). The card
  then shows on **every day of the range** `[start, end]`.
- `sprintStart` becomes the team's **current** sprint for any start in the
  past, whatever sprint was active back then. The dates are the person's to
  choose and go back exactly as asked; the sprint is where the work **stands**.
  A closed sprint is one the process cannot reach: a carry-over moves the
  closing sprint's own cards and nothing older, so a card parked in a closed
  sprint is never picked up again — it goes on being drawn while it stays
  **open**, and leaves the team's board the moment somebody finishes it, with
  the work done and no trace on the sprint they are working. This was already
  the rule for a start older than any tracked sprint (three cards went that way
  in one working day); it used to stop one sprint short, so a start falling
  inside the **previous** sprint joined that one, which had closed just the
  same. Only a team with no sprint pointer at all falls back to the start day
  itself, which seeds its first. A **future** start parks the card sprint-less
  until the carry-over that reaches its day adopts it.

## Resolved open questions

1. **Two fields.** `sprintStart` = sprint, `startDate` = scheduled day. (The
   one-field "single-day" model is dropped.)
2. **Me by the sprints a card spans on the viewed day** —
   `activeSprint(team, day) <= sprintStart`, gated by `startDate <= selectedDate`,
   so a carried-over card stays visible in the previous sprint it came from.
3. **Team shows what is in hand on the day** — one rule, applied to every card
   (see Team view), with two deliberate asymmetries: the CURRENT sprint is the
   whole sprint on the day it began AND on today, closed work included (Carry
   Over clears it onto the previous sprint), and a day AHEAD of today is a plan
   rather than a state. The seven layered rules this replaced, `sprintStart`
   and `startDate` days among them, are gone.
4. **Defer moves `startDate` counting from today** and keeps the card in its
   sprint, so the card is back in it the day it arrives; while it waits it is
   off every live day board, its sprint's own day included. The **calendar** is
   a real move (`startDate = sprintStart = start`, `day = end`).
5. The existing telemetry card is left as-is (the owner will move it).
