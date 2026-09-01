# Board dates, sprints, and card visibility

Source of truth for how aeman places cards on days, what a "sprint" is, and the
visibility rules for the Team / Me / Weekly views, plus create, Carry Over, and
send-to-next-day. The date logic is subtle — **keep this file in sync with the
code** whenever the rules change.

Code that implements these rules:
- Frontend: `web/src/components/{TeamBoard,MeBoard,Card}.tsx`, `web/src/date.ts`,
  `web/src/sprint.ts`.
- Go: `pkg/board/{filters,date,sprint}.go`, `pkg/boardservice/service.go`.

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
- **Week** (`week`, date) — weekly-plan only (Monday of the plan week).
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

A card filed under a Project-board **column** (an epic) is not on the day grid until it joins a sprint: its multi-week span would smear across every day it covers, and the column is where it is shown meanwhile. A card that merely carries a project NAME has no column — the Project board renders columns by epic — so it is an ordinary dated card and shows like one. The Me view draws the same line, with one exception: a slot someone owns shows on that person's own board, because then it is their work.
- A **materialized** card (`startDate <= today`) shows on its sprint's start day
  (`sprintStart`) **and** on its own scheduled day (`startDate`) — so a card
  created on a later day of the sprint appears both on the sprint day (where the
  team lead sees the whole sprint at once) and on the day it was actually created.
- It **also** shows on every **sprint day it passed through**: a sprint-pointer
  day `S` (the team's current or previous sprint) with
  `activeSprint(team, startDate) <= S < sprintStart` — so navigating back shows
  each sprint complete, carried-over and deferred cards included.
- A **deferred / future-scheduled** card (`startDate > today`) shows on its own
  future day, and its **past sprint day keeps it** (`sprintStart < today`) — so
  deferring never erases where the card came from. It is hidden everywhere else
  until its day arrives, then it materializes back into the rules above.

### A past day is shown as it was (the snapshot)

The rules above place TODAY's cards on a day. That is what a day-lens is: dates decide where a card belongs, and a card finished this morning reads finished on every day you flip to. Going BACK on the Me or Team board answers a different question — what did the board look like then — and gets a different answer: the board **as it stood** when that day ended, read from the repositories' own history (the tree at that day's last commit). Every field is the day's own; a card created since is simply absent.

- **Whether a day is over is each TEAM's own answer**, not the calendar's. A sprint lays itself out on its own day — that is where the lead works it from, and where a card created today lands (its `sprintStart` may be days old) — so a team still inside that sprint keeps the live board, while a team whose sprint has moved past the day shows what it held that evening. One day, one screen, two moments: on a board of several teams the columns of the settled teams are records and the rest is today's work, and each card says which it is (`status.asOf`). A team whose sprint has stood since July therefore keeps every day since July live for itself alone — that sprint IS open, and Carry Over is what closes it.
- Only a **past** day, and only the day boards (Me, Team). Today is still happening, tomorrow has not, and the Project and Process boards are not day boards.
- A record is not a workspace, and it looks like one: flat and grey, with no control on it, and the detail pane opens it read-only. It does not drag; a write to it is refused in the browser AND by the server (409 on `X-Aeman-As-Of`), so a UI path that forgets fails loudly instead of writing today's board from a picture; today's traffic is not applied over it. The live cards beside it stay entirely workable.
- The history has an edge. The server keeps a horizon (`--history`, two weeks by default) and deepens on demand up to `--history-max` (a year); a day behind that is refused (410) rather than answered with the oldest state at hand.

### Me view — a personal day (`selectedDate`)
- A card (assigned to the viewer) shows when its scheduled day has arrived and the
  viewed day falls in a sprint the card **spans** — from the one it started in up
  to the sprint it now belongs to: `startDate <= selectedDate` **AND**
  `activeSprint(team, selectedDate) <= sprintStart`.
- So **today** shows the current sprint's cards. Rolling **back** into the
  previous sprint's days shows that sprint's cards — including the **carried-over**
  ones, which stay visible on the days of the sprint they came from (not only the
  cards that were completed there).
- A **deferred / future-scheduled** card (`startDate > today`) is hidden until
  that day, then shows from it on (the next Carry Over re-syncs its sprint like
  any unfinished card).
- A **sprint-less** day card (a "next sprint" create) shows from its
  `startDate` on — the sprint gate above would otherwise hide it right when its
  day arrives — until a Carry Over adopts it into a sprint. Only cards
  scheduled into the sprint active on the viewed day or later
  (`startDate >= activeSprint(team, day)`) qualify: an old sprint-less stray
  stays on its own past days instead of resurfacing on current boards.
- The team-focus "eye" toggle further narrows to the selected teams (not
  persisted, off by default).

### Carry Over
- No-op if the team's sprint is already today.
- Else: sets sprint-state to (current = today, previous = old current), and for
  every **unfinished** card of the **closing sprint** (`sprintStart == old
  current`), sets `sprintStart = today`. A card that is **not on today's
  sprint** — demoted back, or simply older — stays where it is, so removing a
  card from the current sprint is final and it never boomerangs back. Complete
  cards (done, or 100% with no stage) stay put too. Then the view jumps to
  today.
- A carried card keeps its `startDate`, so it stays visible on the days of the
  sprint it came from (see the Team / Me rules): Carry Over adds it to the new
  sprint without removing it from the previous one.
- **Adoption**: unfinished **sprint-less** day cards ("next sprint" creates,
  not plan cards) with `old sprint < startDate <= today` also get
  `sprintStart = today` — they join the sprint this Carry Over opens, which is
  what "the next sprint" meant when they were created. Ones still ahead of
  today wait for a later Carry Over; ones scheduled **before** the closing
  sprint (old sprint-less strays — report cards, legacy cards) are never
  dragged in.

### Defer ("+1 day" / "+1 week")
- The per-card control pushes **`startDate`** forward — counting from **today**
  (or from the card's already-deferred slot, so presses stack): `+N` sets
  `startDate = max(today, startDate) + N`. The card **stays in its sprint**
  (`sprintStart` untouched), so its past sprint day keeps showing it.
- A card **created today** (0d) has no history worth keeping: deferring it
  relocates it fully — `sprintStart` moves to the new day too (and a stale end
  date is pulled along), so it leaves the current sprint entirely.
- While `startDate > today` the card is hidden between today and that day in Me
  and Team; it shows on its new day, and its past sprint day keeps it in Team.
- Carry Over still sweeps a deferred card's sprint forward (its `sprintStart` is
  in the past), but the future `startDate` keeps hiding it until its day comes.

### Recurrent cards
- The **recurrent** stage marks a repeating task. Unlike review/locked its
  progress spans the full 0–100% (no clamp), and 100% counts as **complete**.
- **Carry Over**: an unfinished recurrent card carries like any other card; a
  **finished** one (100%) stays behind and is **reseeded** — a fresh copy is
  created in the new sprint with the same title and description, no notes,
  at 0%, recurrent again.
- **Carry over week**: the same rule for plan cards — a finished recurrent plan
  card stays in its week and a fresh copy is created in the target week, unless
  a plan card with the same title is already there (re-running is idempotent).
- Recurrent plan cards are **excluded from the weekly progress bar**: it
  describes the week's one-off work only.

### Project-board slots in the Weekly plan

A weekly-plan card with no dates of its own, **attached** to a Project-board column, takes the slot of the week it was taken from: its start becomes that week's Monday and its end its band's day (by-Wednesday → the Wednesday, by-Friday → the Friday), so the new slot's row is the very week the card was planned in — it does not jump elsewhere on the way to the Project board (G55). A plan card that already carries dates keeps them: the attach never rewrites a schedule someone chose.
- A **slot** (an epic-column card: epic + `week` + `day` set) needs **no
  stored plan band** to be on the Weekly panel: its span IS its plan. It
  shows on the panel of **every week between its boundaries**.
- Its band **derives from the end date**: only the week the slot *ends* in
  can be a by-Wednesday week (`day` ≤ that week's Wednesday); every earlier
  covered week holds the slot in the by-Friday band — it stays open through
  that week's end.
- A **stored band always wins** — hand placement outranks derivation, so
  deriving never moves a card someone placed. The row's plan stripe in Me and
  Team uses the same effective band.
- Except for a **debt**. A plan card or slot still open past the day it was
  owed by shows on the CURRENT week's panel as well as on its own (the debt
  rule), and there it stands in the **by-Wednesday** band whatever it
  carries: it is already late, so what it faces is the nearest deadline of
  the week it is standing in, not that week's last one. Its own week and
  band are untouched — on the panel of the week it was owed in it is still
  the card it was. The stripe follows the panel for the same reason: a
  "by Friday" mark on a card sitting under "by Wednesday" is two answers to
  one question.

### Subtasks (grouped cards)
- A card with a **parent** (the `Parent` text field, one level deep) is a
  **subtask**: it never appears as a row of its own in Team/Me — the views
  deliver it alongside its parent and the UI nests it under the parent's
  expandable list. The Me team-focus filter applies to subtask rows too.
- A subtask is a normal card in every other way: own description, own log and
  notes, own stage/progress, own assignee. It can be pulled back out as a
  standalone card at any time (clear the parent).
- **On the Project board** a subtask that carries its own column is drawn as
  an ordinary slot, marked `↳` with its parent's title: grouping work under a
  parent must not take it off the planner, and a parent commonly lives
  elsewhere (the weekly plan, the working area), so hiding the children left a
  whole group visible in no column at all. Its team badge is read-only there —
  a subtask's team follows its parent — and the slot's × only takes the column
  away: the card still rides its parent, so it is never deleted from there. The
  column may be re-attached afterwards; a SECOND column (a mirror) it may not
  have, because its file follows the parent and the mirror would be stranded
  the moment the parent changes repository. The day grid's × on such a card
  takes it OUT OF THE GROUP and leaves it in its column: a subtask's person
  and sprint are its parent's, so releasing it while still grouped would
  break that pair or be undone by the next carry-over. The column must name the repository
  that actually holds the card — the one its parent lives in.
- The project's progress line counts such a card unless its **parent stands in
  the same project** and is drawn there itself, whose own bar already derives
  from its children: counting both would weigh that work twice on one screen. A
  parent nothing draws — a column with no dates is no slot — answers for
  nothing, and the child counts. A column's own bar asks the narrower question:
  it drops the child only when the parent stands in that same column. Both
  read every column the parent is DRAWN in — its home pair and its mirrors
  — since a mirror is the same card standing in a second column: a parent
  mirrored into the child's column answers for the child there, and the bar
  that read home pairs alone counted the two of them. A figure that spans
  columns (the board's header total) asks it by identity instead: the
  parent is either drawn in that figure or it is not.
- **Derived progress**: while a card has subtasks its bar derives from them —
  the average of the subtasks' effective progress (done = 100) scaled into
  0–90%. The final done / 100% is always a human decision on the parent, and a
  card **cannot be done while it has open subtasks**.
- **Grouping** a card under a parent syncs it into the parent's sprint
  (`sprintStart` copied), moves it onto the **parent's team**, and clears its
  own plan slot; a **weekly-plan card dropped onto a grid card** hands its
  plan slot to the parent instead (the parent replaces it in the Weekly
  panel) — subtasks are never plan cards themselves, though an expanded
  weekly parent shows its subtask rows nested under it.
- **A subtask's person always follows its parent**, the way its team does:
  grouping hands the child over to the parent's assignee, a direct change on a
  subtask snaps back to the parent's, and re-assigning a parent hands the whole
  family over (unassigning it unassigns them all). A family that drifts apart
  lands on two personal boards — the Me view admits a card when you own one of
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
- `sprintStart` becomes the sprint that was **active on the start day**
  (`activeSprint(team, start)`) — a start inside the current sprint joins it —
  falling back to the start day itself when no tracked sprint covers it.

## Resolved open questions

1. **Two fields.** `sprintStart` = sprint, `startDate` = scheduled day. (The
   one-field "single-day" model is dropped.)
2. **Me by the sprints a card spans on the viewed day** —
   `activeSprint(team, day) <= sprintStart`, gated by `startDate <= selectedDate`,
   so a carried-over card stays visible in the previous sprint it came from.
3. **Team shows a materialized card on both its days** — its sprint's start day
   (`sprintStart`) and its own scheduled day (`startDate`); a future-deferred card
   only on its own day; plus the previous sprint's start day when the card was
   carried over from there.
4. **Defer moves `startDate` counting from today** and keeps the card in its
   sprint, so the past sprint day never loses it; the **calendar** is a real
   move (`startDate = sprintStart = start`, `day = end`).
5. The existing telemetry card is left as-is (the owner will move it).
