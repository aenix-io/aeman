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
  the card was created on; send-to-next-day moves it forward.

Also:
- **Day** (`day`, date) — set on create, not used by any filter.
- **Week** (`week`, date) — weekly-plan only (Monday of the plan week).
- Hidden per-team **`aeman:sprint-state`** card: `Sprint Start` = the team's
  **current** sprint date, `Start` = its **previous** sprint date. Read via
  `currentSprint(team)` / `previousSprint(team)`.

`startDate` and `sprintStart` differ whenever a card is created on a later day of
its sprint, or deferred with send-to-next-day.

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

### Team view — a card's effective day (`selectedDate`)
- A card shows on its **effective day**: its sprint (`sprintStart`) once it has
  materialized (`startDate <= today`), but its scheduled day (`startDate`) while
  that is still in the future. Show iff `effectiveDay === selectedDate`.
- It **also** shows on the **previous** sprint's start day when it was carried
  over from there (`sprintStart > previousSprint` and its origin sprint,
  `activeSprint(team, startDate)`, is on or before it) — so navigating back to the
  previous sprint shows it complete, carried-over cards included.
- A **materialized** card therefore sits on its sprint's start date, so the team
  lead navigates there to see **all** that sprint's cards, including ones created
  on later days of the sprint.
- A card **deferred to the future** (`startDate > today`) shows on its own future
  day instead of the sprint day, and rejoins the sprint day once today catches up
  to its `startDate`.

### Me view — a personal day (`selectedDate`)
- A card (assigned to the viewer) shows when its scheduled day has arrived and the
  viewed day falls in a sprint the card **spans** — from the one it started in up
  to the sprint it now belongs to: `startDate <= selectedDate` **AND**
  `activeSprint(team, selectedDate) <= sprintStart`.
- So **today** shows the current sprint's cards. Rolling **back** into the
  previous sprint's days shows that sprint's cards — including the **carried-over**
  ones, which stay visible on the days of the sprint they came from (not only the
  cards that were completed there).
- A deferred card (`startDate` in the future) is not shown until that day.
- The team-focus "eye" toggle further narrows to the selected teams (not
  persisted, off by default).

### Carry Over
- No-op if the team's sprint is already today.
- Else: sets sprint-state to (current = today, previous = old current), and for
  every **unfinished** card with `sprintStart < today`, sets `sprintStart = today`.
  Future-dated and **done** cards stay put. Then the view jumps to today.
- A carried card keeps its `startDate`, so it stays visible on the days of the
  sprint it came from (see the Team / Me rules): Carry Over adds it to the new
  sprint without removing it from the previous one.

### Send to next day
- The per-card "+1 day" / "-1 day" control moves **`startDate`** by a day
  (the card stays in its sprint, `sprintStart` unchanged). The card disappears
  from Me/Team until its new `startDate` arrives (deferring into the future hides
  it; it reappears when that day comes).

## Resolved open questions

1. **Two fields.** `sprintStart` = sprint, `startDate` = scheduled day. (The
   one-field "single-day" model is dropped.)
2. **Me by the sprints a card spans on the viewed day** —
   `activeSprint(team, day) <= sprintStart`, gated by `startDate <= selectedDate`,
   so a carried-over card stays visible in the previous sprint it came from.
3. **Team by a card's effective day** — `sprintStart` once materialized, else the
   future `startDate`; plus the previous sprint's start day when the card was
   carried over from there.
4. **Send-to-next-day moves `startDate`** (not the sprint).
5. The existing telemetry card is left as-is (the owner will move it).
