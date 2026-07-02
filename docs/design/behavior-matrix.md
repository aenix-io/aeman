# Behavior test matrix — current logic, pinned before the API redesign

Companion to [api-redesign.md](api-redesign.md). Every rule the board runs on
today, where it lives, and the test that pins it. Rules marked **frontend** are
being moved server-side by the redesign — their tests are written FIRST against
the new service methods and must encode today's frontend behaviour exactly.
The date logic is the fragile part; it gets the densest coverage.

Legend: ✅ existing Go test · 🆕 new test written for the redesign ·
(file names are relative to `internal/`)

## Dates & visibility

| # | Rule (as shipped today) | Lives in | Test |
| --- | --- | --- | --- |
| D1 | Two dates: `startDate` (scheduled day) + `sprintStart` (sprint membership); `day` = end of the visible range | board model | ✅ board/filters_test, board_test |
| D2 | Team grid: a materialized card shows on its sprint day AND its own start day | board.TeamGrid | ✅ filters_test `a sprint day keeps all its cards…`, `a later-created card…` |
| D3 | Team grid: a card with `end >= start` shows on **every** day of `[start, end]` | board.TeamGrid | ✅ filters_test `a ranged card…` (4 cases) |
| D4 | Team grid: deferred card (`start > today`) shows on its own day (or range) + its past sprint day; hidden elsewhere | board.TeamGrid | ✅ filters_test `deferred…` (3 cases) |
| D5 | Team grid: passed-through history — pointer day S (current/previous) with `origin ≤ S < sprintStart` keeps carried/demoted cards | board.TeamGrid | ✅ filters_test `a sprint day keeps all its cards…` |
| D6 | Me view: `activeSprint(day) ≤ sprintStart`, gated by `start ≤ day`; deferred hidden until its day; ranges span | board.MeView | ✅ filters_test TestMeView |
| D7 | activeSprint = current if `day ≥ current`, else previous if `day ≥ previous`, else "" | board.ActiveSprint | ✅ sprint_test |
| D8 | Create defaults: start/day default to each other (one-day range), never today for backdated creates; sprint = team current; first sprint recorded; plan cards get no dates | boardservice.CreateCard | ✅ service_test (create suite) |
| D9 | **Defer +N: target = `max(today, start) + N`; only `start` moves; sprint keeps the card's history day** | **frontend** (Card.moveStart + handleDefer) | 🆕 service_test `TestDefer*` |
| D10 | **Defer of a same-day card (createdAt today): full relocation — sprint and a stale end date move along** | **frontend** (handleDefer) | 🆕 service_test `TestDeferSameDay*` |
| D11 | **Calendar set-dates: `start` → startDate, sprint = `activeSprint(team, start)` else start, `end` → day** | **frontend** (handleSetDates) | 🆕 service_test `TestSetDates*` |
| D12 | Carry over: only the closing sprint's unfinished cards move; demoted/old cards stay (removal is final) | boardservice.CarryOver | ✅ service_test carry suite |
| D13 | Carry over: finished recurrent reseeds a fresh copy (title/description/assignee/zone, 0%, no notes) | boardservice.CarryOver | ✅ `TestCarryOverReseedsFinishedRecurrent` |
| D14 | Carry week: same for plan cards + same-title dedup (idempotent) | boardservice.CarryWeek | ✅ `TestCarryWeekReseedsFinishedRecurrent` |
| D15 | Carry over is idempotent for today; advances pointer even with nothing to carry | boardservice.CarryOver | ✅ service_test |
| D16 | **Carry over / carry week dry run: report would-be counts, mutate nothing** (backs the UI confirm) | **frontend** (confirm-count filters) | 🆕 service_test `TestCarryOverDryRun`, `TestCarryWeekDryRun` |

## Status & progress

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| S1 | review/locked clamp progress to [10, 90] | board.ClampProgress | ✅ status_test |
| S2 | Done is derived: 100% + no stage; never stored; picking Done clears stage + fills 100 | board.ApplyProgress/ApplyStage | ✅ status_test, service_test, server/api_test |
| S3 | Complete = done ∨ (100% ∧ (none ∨ recurrent)); review/locked@100 unfinished | board.Complete | ✅ status_test TestComplete |
| S4 | Recurrent: unclamped 0–100; excluded from plan progress | board + view | ✅ status via S3; 🆕 view test `TestWeeklyViewProgressExcludesRecurrent` |
| S5 | In Progress: clears stage, nudges into [10,90] at the edges | board.ApplyInProgress | ✅ status_test |
| S6 | Review card progress drives the original's review stage (100 → off review, <100 → back on) | boardservice.syncReviewLink | ✅ service_test review suite |
| S7 | review/locked knock a full (100%) card to 90 on stage pick | board.ApplyStage | ✅ status_test |

## Actions (the frontend logic moving server-side)

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| A1 | **Smart remove (grid): in the team's current sprint → demote (start=sprint=previous, end pulled along); otherwise real delete** | **frontend** (handleGridDelete) | 🆕 service_test `TestRemoveGrid*` |
| A2 | **Smart remove (grid, plan-taken card): clear assignee + sprint — back to plan-only, keeps plan marker** | **frontend** (handleGridDelete plan branch) | 🆕 `TestRemoveGridPlanTaken` |
| A3 | **Smart remove (plan band): assigned card keeps working, only plan+week cleared; pure plan card demotes to its previous week, else deletes** | **frontend** (removeFromPlan) | 🆕 `TestRemovePlan*` |
| A4 | Real delete cascades to the linked review card | boardservice.DeleteCard | ✅ service_test |
| A5 | **Leaving review (stage change / in-progress) cancels the unfinished linked review card: demote + break the reviewOf link, or delete; a finished review card stays** | **frontend** (cancelLinkedReview) | 🆕 `TestStageOffReviewCancelsLinked*` |
| A6 | Send-to-review: creates the linked review card (reviewer, yellow, viewed day), original → review stage | boardservice.SendToReview | ✅ service_test |
| A7 | Reassign / remove reviewer | boardservice | ✅ service_test |
| A8 | Take-into-plan / release-from-plan | boardservice | ✅ service_test |
| A9 | Move: reorder after a card / to the top; order is board-level | backend MoveCard + store | 🆕 apiserver `TestOrdering*` (uid list + move) |

## Views & watch (resource layer)

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| V1 | Team/Me/Weekly LIST selectors return exactly what the UI renders, sorted by board order | board filters (✅) + 🆕 apiserver `TestListSelectors*` |
| V2 | Weekly view response carries wed/fri bands + plan progress (recurrent excluded, done→100, average) | **frontend** (planProgress) | 🆕 `TestWeeklyViewProgress*` |
| V3 | Watch (unscoped): ADDED/MODIFIED/DELETED per card; echo suppression by client id | server store | ✅ (live-verified) + 🆕 `TestWatchEvents*` |
| V4 | Watch (view-scoped): entering/leaving the selection delivers ADDED/DELETED for that subscription | 🆕 apiserver `TestScopedWatch*` |
| V5 | Ordering singleton: move emits one MODIFIED Ordering; LIST stays sorted | 🆕 apiserver `TestOrdering*` |

## Card object mapping

| # | Rule | Test |
| --- | --- | --- |
| M1 | Domain card ↔ resource Card (metadata/spec/status) round-trips every field; zones map to semantic names (urgent/unplanned/planned/niceToHave ↔ red/yellow/gray/green) | 🆕 apiserver `TestCardResource*` |
| M2 | `status.complete/inProgress/reviewedBy` derived correctly | 🆕 apiserver `TestCardStatus*` |
| M3 | Notes as a separate collection; draft-log parsing unchanged | ✅ ghprojects load/notes tests |

The 🆕 tests are written and committed **red-first** (or against thin stubs)
before the implementation lands, so the port from the frontend cannot silently
drift from today's behaviour.
