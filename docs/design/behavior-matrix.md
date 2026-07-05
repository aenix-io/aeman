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
| S7 | review/locked store the [10, 90] clamp on stage pick (0 → 10, 100 → 90), so the stored value matches the band | board.ApplyStage | ✅ status_test |

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

## Old → new coverage audit

Every surface of the pre-redesign API, checked off against its replacement.
Done after the cutover so nothing silently dropped.

### HTTP endpoints (35 old → new)

| Old | New | Notes |
| --- | --- | --- |
| GET /board | GET /board | Field metadata dropped by design — zones are semantic now, no option-id mapping for clients. |
| GET /snapshot | GET /cards + /sprints | Split per resource; board order preserved in the list. |
| GET /watch | GET /watch | New frames {type, kind, object}; RELOAD replaced by Sprint/Ordering events + scoped re-diffs. |
| GET /team, /me, /weekly | GET /cards?view= | Weekly bands via spec.plan.band; +weekly.progress. |
| POST /cards | POST /cards | Body follows the spec shape. |
| POST /carry-over, /carry-week | POST /sprints/actions/… | +dryRun count reports. |
| POST /sprint-state | PATCH /sprints | |
| DELETE /cards/{id} | DELETE /cards/{uid} | Cascade unchanged. |
| …/stage | PATCH {stage} | + server-side review-cancel cascade (was frontend). |
| …/in-progress | …/actions/in-progress | |
| …/progress | PATCH {progress} | Done-link unchanged. |
| …/zone | PATCH {zone} | Semantic names; colour keys rejected. |
| …/day | PATCH {dates:{end}} | |
| …/start | PATCH {dates:{start}} | Now runs the calendar rule (sprint follows). Granular start-only writes had no standalone use: defer and demote are actions. |
| …/sprint-start | PATCH {dates:{sprint}} | |
| …/plan, …/week | PATCH {plan:{band, week}} | |
| …/assignee | PATCH {assignees} | |
| …/team | PATCH {team} | Sprint join falls back to today (was: the UI's selected day). Deliberate: the server has no view state. |
| …/take-plan, …/release-plan | …/actions/take-into-plan, release-from-plan | |
| …/move | …/actions/move | + Ordering resource/events. |
| …/note, …/notes/{id} | …/notes CRUD | Mutations return the NoteList. |
| …/description | PATCH {description} | |
| …/rename | PATCH {title} | |
| …/review | …/actions/send-to-review | |
| …/review/reassign | (folded) | send-to-review reassigns when a review card exists. |
| …/review/remove | …/actions/remove-reviewer | |
| — | …/actions/remove | New: the frontend's smart × (A1–A3). |
| — | …/actions/defer | New: D9/D10. |
| — | PATCH {reviewOf} | New: the review link is writable (A5 fix). |
| — | GET /ordering | New singleton. |

### MCP tools (21 old → 20 new)

get_board ✓ · team_view/me_view/weekly_plan → list_cards ✓ · create_card ✓ ·
carry_over/carry_week ✓ (+dryRun) · set_stage/set_progress/set_assignee/
set_team/rename_card → update_card ✓ · set_in_progress → **in_progress**
(added by this audit — update_card cannot express the nudge) ·
send_to_review ✓ · reassign_reviewer → folded into send_to_review ✓ ·
remove_reviewer ✓ · take_into_plan/release_from_plan ✓ ·
move_card/delete_card/add_note ✓ · new: get_card, list_notes, edit_note,
delete_note, remove_card, defer_card, update_card(dates/plan/reviewOf).

### Frontend logic moved server-side

Defer (D9/D10) · calendar set-dates (D11) · smart remove (A1–A3) ·
review-cancel cascade incl. the reviewOf break (A5) · first-sprint record on
create (D8, was doubled client-side) · carry confirm counts (D16) · weekly
plan progress (V2). Each is pinned by the 🆕 tests above.

## Links (2026-07-02)

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| L1 | Description links: GitHub issue/PR refs first (resolved to titles via the backend resolver; unresolvable ones stay unresolved, not dropped), plain links after, deduped | board.ExtractLinks + boardservice.CardLinks | ✅ links_test, service_test TestCardLinks, server TestAPICardLinks |
| L2 | Create-by-URL: a title that is only a GitHub issue/PR URL resolves to that item's title, the link moves into the description; unresolvable keeps the URL title | boardservice.CreateCard | ✅ TestCreateCardFromGitHubURL / FromURLUnresolved |
| L3 | UI: hashtag icon (link icon when only plain links) before the stage icon; one menu, refs-with-titles first, plain links as-is; click opens a new tab | Card.tsx + links.ts mirror | manual |
| L4 | Send-to-review copies the description; afterwards it LIVE-SYNCS across the review link both ways (notes stay per-card) | boardservice.SendToReview / SetDescription | ✅ TestSendToReviewCopiesDescription, TestSetDescriptionSyncsAcrossReviewLink |

## Review-card lifecycle (2026-07-02)

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| R1 | A review card the reviewer worked on (progress > 0) is never auto-removed: leaving review / done keeps it untouched, link intact | boardservice.cancelLinkedReview | ✅ TestStageOffReviewKeepsWorkedReviewCard |
| R2 | Reassigning a reviewer who worked keeps their card (released from the link, stays behind on the next carry) and spawns a fresh review card; an untouched card is handed over in place | boardservice.ReassignReviewer | ✅ TestReassignWorkedReviewerSpawnsNewCard / InPlace |
| R3 | Carry-over moves a review card only while the review is still required: original unfinished on the review stage + a reviewer assigned; stale review work stays behind | boardservice.CarryOver | ✅ TestCarryOverReviewCardsOnlyWhileRequired |

## Focus filter (2026-07-03)

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| F1 | Workable = not done, not on-review, not locked (keeps in-progress, not-started, recurrent<100) | board.Workable | ✅ apiserver TestSelectorFocusAndMultiTeam |
| F2 | `focus=true` selector keeps only workable cards; shared by the Me view toggle, the HTTP LIST and MCP list_cards | apiserver.Selector.Focus + FilterCards | ✅ TestSelectorFocusAndMultiTeam |
| F3 | On view=me / default, `team=` filters by team as a comma-separated set (the eye toggle over the selected chips) | apiserver.FilterCards teamInSet | ✅ TestSelectorFocusAndMultiTeam |
| R4 | Re-review reactivates the completed review card whenever a passed original is put back on review — the stage menu (SetStage→review) or re-sending to the SAME reviewer both trigger it: review card progress→0, round counter ticks up (round 1 implicit, first re-review = 2), original on review. Symmetric reverse of the review-done→off-review forward sync; a still-in-progress review or a different reviewer is untouched | boardservice.reactivateReviewCard (SetStage + ReassignReviewer) | ✅ TestReReviewReactivatesReviewCard / ViaStageMenu / DifferentReviewer / EnterReviewNoCompletedCardIsNoop |
| R5 | A review card is created in the same sprint as the card it reviews (original's sprintStart), not the team's current pointer | boardservice.sendToReview | ✅ TestSendToReviewUsesOriginalSprint |
| R6 | Carry-over leaves a completed review card behind, pinned to the closing sprint's day, so a card created today does not linger on the new sprint via its own start-day | boardservice.CarryOver | ✅ TestCarryOverPinsCompletedReviewCard |
| R7 | Re-review pulls the review card into the original's current sprint and onto today (reappears in the new sprint) and bumps the round | boardservice.reactivateReviewCard | ✅ TestReReviewRelocatesToNewSprintWithCounter |
| V1 | The Team board loads its day grid PLUS the weekly plan of the shown teams (view=weekly accepts a comma set); the plan panel renders from the same card set | apiserver weekly multi-team + App dual fetch | ✅ TestWeeklyViewMultiTeam / viewquery.test |
| V2 | GET /board carries the people roster (members: every distinct assignee), so assign/review/view-as pickers work with per-view card loading; Me view-as sends the impersonated user explicitly | apiserver BoardResource + App viewAs | ✅ TestBoardResourceMembers / viewquery.test |
