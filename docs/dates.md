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
  sprint's start date). Carry Over moves this; the Team board groups by it.
- **Start** (`startDate`) — the card's **scheduled day**: the day it starts
  showing and "materializes" into its sprint. Set to the day the card was created
  (the viewed day); send-to-next-day moves it forward.

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
- `startDate = today` (the viewed day) — its scheduled day is now.
- First sprint: if the team has none yet, creating records today as its first
  sprint on the sprint-state card.

### Team view — a sprint's day (`selectedDate`)
- A card shows on **its sprint's day** and only once it has materialized:
  `sprintStart === selectedDate` **AND** `startDate <= today`.
- The team lead navigates to a sprint's start date to see **all** that sprint's
  cards, including ones created on later days of the sprint.
- A card **deferred to the future** (`startDate > today`) is hidden until its day
  arrives — it is not yet within the sprint's realized dates.

### Me view — a personal day (`selectedDate`)
- A card (assigned to the viewer) shows when its scheduled day has arrived and it
  belongs to the sprint that was current on the viewed day:
  `startDate <= selectedDate` **AND** `sprintStart === activeSprint(team, selectedDate)`.
- So **today** shows the current sprint's cards (everything from the sprint's
  start up to today). Rolling **back** into the previous sprint's days shows that
  sprint's cards — e.g. a card you completed in the previous sprint reappears when
  you scroll to a day that fell inside it.
- A deferred card (`startDate` in the future) is not shown until that day.
- The team-focus "eye" toggle further narrows to the selected teams (not
  persisted, off by default).

### Carry Over
- No-op if the team's sprint is already today.
- Else: sets sprint-state to (current = today, previous = old current), and for
  every **unfinished** card with `sprintStart < today`, sets `sprintStart = today`.
  Future-dated and **done** cards stay put. Then the view jumps to today.

### Send to next day
- The per-card "+1 day" / "-1 day" control moves **`startDate`** by a day
  (the card stays in its sprint, `sprintStart` unchanged). The card disappears
  from Me/Team until its new `startDate` arrives (deferring into the future hides
  it; it reappears when that day comes).

## Resolved open questions

1. **Two fields.** `sprintStart` = sprint, `startDate` = scheduled day. (The
   one-field "single-day" model is dropped.)
2. **Me by the active sprint on the viewed day** (current, or previous when
   scrolled back), gated by `startDate <= selectedDate`.
3. **Team by `sprintStart === selectedDate` and `startDate <= today`** — strictly
   the sprint's day, hiding future-deferred cards.
4. **Send-to-next-day moves `startDate`** (not the sprint).
5. The existing telemetry card is left as-is (the owner will move it).
