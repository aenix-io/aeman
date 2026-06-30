# Board dates, sprints, and card visibility

This is the source of truth for how aeman places cards on days, what a "sprint"
is, and the visibility rules for the Team / Me / Weekly views, plus create,
Carry Over, and send-to-next-day. The date logic is subtle — **keep this file in
sync with the code** whenever the rules change.

Code that implements these rules:
- Frontend: `web/src/components/{TeamBoard,MeBoard,Card}.tsx`, `web/src/date.ts`,
  `web/src/sprint.ts`.
- Go: `internal/board/{filters,date,sprint}.go`, `internal/boardservice/service.go`.

## Fields

GitHub Project fields on a card:

- **Sprint Start** (`sprintStart`, date) — the sprint the card belongs to. Carry
  Over moves this and groups by it.
- **Start** (`startDate`, date) — the card's display day (the day it appears on
  the day board). *Naming is historical; see Open question 1.*
- **Day** (`day`, date) — set on create, currently not used by any filter.
- **Week** (`week`, date) — weekly-plan only (Monday of the plan week).

The hidden per-team **`aeman:sprint-state`** card carries the team's sprint
pointer: its `Sprint Start` = the team's **current** sprint date, its `Start` =
the team's **previous** sprint date. Read via `currentSprint(team)` /
`previousSprint(team)`.

## Concepts

- A **sprint** is a team's batch of work anchored to a start date. Each team's
  current and previous sprint live on its sprint-state card.
- **Carry Over** (button) advances the team's sprint to today (current →
  previous), and pulls its **unfinished** cards forward to today. **Done cards
  are left on their day** (they do not crawl to the new sprint). After it runs,
  the board jumps the viewed day to today.

## Rules (current intent)

### Create
- A new card **joins the team's current sprint**: `sprintStart = currentSprint(team)`
  — which may be an earlier day (e.g. the sprint is "yesterday" and you add a
  card today).
- Its **display date = today** (the viewed day): `startDate = today`.
- So a card created today while the team's sprint is yesterday belongs to
  yesterday's sprint but is shown on today.
- First sprint: if the team has no sprint yet, creating records today as its
  first sprint on the sprint-state card.

### Team view — one day at a time (`selectedDate`)
- A card shows **only on its display day**: `startDate === selectedDate`.
- It does not appear on earlier or later days. A card sent to a future day does
  not show today; a card on a past day is not pulled onto today (Carry Over does
  that).

### Me view — one day at a time (`selectedDate`)
- A card (assigned to the viewer) shows from the moment its day arrives, and
  keeps showing **until it is completed *and* a new sprint has started**.
- ⇒ visible while `previousSprint(team) <= sprintStart <= selectedDate`: a done
  card from the just-ended sprint stays visible until the **next** sprint starts;
  an unfinished card is carried forward by Carry Over and keeps showing.
- The team-focus "eye" toggle further narrows to the selected teams (not
  persisted, off by default).

### Carry Over
- No-op if the team's sprint is already today.
- Otherwise: sets sprint-state to (current = today, previous = old current), and
  for every **unfinished** card with `sprintStart < today`, sets `sprintStart =
  today` (sweeps in-between and externally-created cards too). Future-dated cards
  and **done** cards stay put.

### Send to next day
- The per-card "+1 day" / "-1 day" control shifts the card forward / back by a
  day, relative to the day you are viewing.
- **Which field it moves is Open question 4.**

## Open questions (need a decision)

> These are the points where the rules above are under-specified or where earlier
> statements conflict. Resolve each, then the doc and code get finalized.

1. **One date or two?**
   - The "single-day" model (confirmed earlier, and implemented but **not
     deployed**): a card lives on exactly one day = `sprintStart`; its sprint
     membership *is* its display day — one field.
   - The latest create rule ("join yesterday's sprint, but end date is today")
     needs the sprint membership (yesterday) and the display day (today) to
     **differ** — two fields (`sprintStart` = sprint, `startDate` = display day).
   - **Decide:** one field (day = sprint) or two fields (sprint + display day)?
     The "Rules" above are written for the two-field reading.

2. **Me lower bound: current or previous sprint?**
   - Earlier: "completed cards drop from Me when a new sprint starts" → `currentSprint`.
   - Latest (telemetry should stay visible; "show until completed *and* new
     sprint") → `previousSprint`.
   - **Proposed:** `previousSprint` (recent done work lingers one sprint, then
     drops). The "Rules" above assume this. Confirm.

3. **Team "its day".** With two fields, Team filters by the **display day**
   (`startDate === selectedDate`). Confirm Team is strictly one day (not a span).

4. **Send to next day moves which field?** The display day (`startDate`), the
   sprint (`sprintStart`), or both? (Moving only the display day keeps the card
   in its sprint but shifts where it shows; moving the sprint re-buckets it.)

5. **Existing telemetry card.** It is done with `sprintStart = startDate =
   2026-06-26` (marketing's previous sprint), so it landed on the 26th from the
   old "create joins the sprint day" behavior. Once the rules above ship it shows
   in Me again (previous-sprint bound). Want it left as-is, or its dates bumped
   to today?
