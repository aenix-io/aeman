# Board dates, sprints, and card visibility

Source of truth for how aeman places cards on days, what a "sprint" is, and the
visibility rules for the Team / Me / Weekly views, plus create, Carry Over, and
send-to-next-day. The date logic is subtle — **keep this file in sync with the
code** whenever the rules change.

Code that implements these rules:
- Frontend: `web/src/components/{TeamBoard,MeBoard,Card}.tsx`, `web/src/date.ts`,
  `web/src/sprint.ts`.
- Go: `internal/board/{filters,date,sprint}.go`, `internal/boardservice/service.go`.

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
today while the card stays in its sprint).

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

### Team view — a card's days (`selectedDate`)
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
