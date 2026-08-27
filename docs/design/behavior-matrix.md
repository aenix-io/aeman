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
| A1 | **Smart remove (grid): in the team's current sprint → demote (start=sprint=previous, end pulled along); otherwise the card is handed back — assignee + sprint cleared, and a card with no Project-board column and no plan band lands in this week's plan. The grid × NEVER deletes: taking a card off a person does not destroy the work, and a card created and assigned the same day used to vanish here** | **frontend** (handleGridDelete) | 🆕 service_test `TestRemoveGrid*` |
| A2 | **Smart remove (grid, plan-taken card): clear assignee + sprint — back to plan-only, keeps plan marker** | **frontend** (handleGridDelete plan branch) | 🆕 `TestRemoveGridPlanTaken` |
| A3 | **Smart remove (plan band): a card filed under a project (either side of the (project, epic) pair) only leaves the plan — never deleted; an assigned-or-worked card keeps working and loses just plan+week; a pure plan card (no column, unassigned, never worked) is deleted for real** | **frontend** (removeFromPlan) | 🆕 `TestRemovePlan*` |
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
| W1 | Weekly history: a worked plan card (has a start date) carried forward keeps showing in every past week it was worked in, mirroring the day grid's sprint history; a pure never-started plan card moves with its week | board.planShowsInWeek + TeamBoard showsInWeek | ✅ TestWeeklyHistoryForWorkedCards |
| W2 | Smart-remove never deletes nor unhooks a worked card: the plan × on an assigned-or-worked card and the grid × on a worked taken card shed only the plan membership (person, dates and sprint history stay); the ONLY × that still deletes is the plan × on a pure plan card — no Project-board column, unassigned, never worked — because the old previous-week demote boomeranged back on the next carry-week. Everything else is handed back: the grid × releases the card to the weekly plan instead of deleting it | boardservice.Remove/ReleaseFromPlan/releaseToPlan | ✅ TestPlanRemoveKeepsWorkedCard / TestGridRemoveOnTakenPlanCard / TestRemoveGridReleasesProjectCardInsteadOfDeleting |
| W4 | A Project-board slot needs no stored band to be on the Weekly panel: its span is its plan (it shows on every week between its boundaries), and its band derives from the end date — by-Wednesday only in the week its end falls in, by-Friday in every earlier covered week. A stored band always outranks the derived one, so deriving never moves a hand-placed card. The Me/Team plan stripe shows the same effective band | board.WeeklyPlanAt + TeamBoard/Card mirror (weekly.ts) | ✅ TestWeeklyPlanDerivesSlotBands / weekly.test.ts |
| W3 | Carried plan cards tighten: a by-Friday card carried into the next week lands in its by-Wednesday band (already overdue); by-Wednesday stays; reseeded recurrents keep their band. Week-history entries render in the past week's by-Friday band (they stayed open through that week's end) | boardservice.CarryWeek + board.WeeklyPlan + TeamBoard mirror | ✅ TestCarryWeekTightensFriToWed / TestWeeklyHistoryLandsInFriBand |
| L5 | Activity log: every aeman mutation (create, stage, progress, assignee, team, zone, review sent/passed/removed/round-reset, plan taken/released, dates, sprint, plan week/band) records an event with the acting user, stored in the card itself (draft body log / a dedicated issue log comment) — capped at 200 events, best-effort (a log failure never fails the mutation), gone with the card on delete. GET /cards/{uid}/log + MCP list_log serve events+notes as one chronological feed; the Me day panel and CardDetail render it. Non-aeman edits (GitHub UI) leave no events. Carries log the same kinds as manual moves (sprint/week/plan-band) and reseeds log created; a plan join/leave logs plan-added/plan-released | board.Event + boardservice.logEvent + ghprojects.AppendEvent | ✅ TestEventBodyRoundTrip / TestPartitionEvents / TestMutationsRecordEvents / TestReviewCycleRecordsEvents / TestAPICardLogUnifiedFeed |
| V3 | Watch (unscoped): ADDED/MODIFIED/DELETED per card; echo suppression by client id | server store | ✅ (live-verified) + 🆕 `TestWatchEvents*` |
| V4 | Watch (view-scoped): entering/leaving the selection delivers ADDED/DELETED for that subscription | 🆕 apiserver `TestScopedWatch*` |
| V5 | Ordering singleton: move emits one MODIFIED Ordering; LIST stays sorted | 🆕 apiserver `TestOrdering*` |

| V+ | Echo suppression is scoped to the ADDRESSED card: the author's watch is spared the echo only for the {uid} their request named — a batch fan-out (epic rename over its cards) or a cascade (a subtask following its parent) echoes even to the author, who holds no optimistic copy of those cards. Unscoped suppression made a renamed column's cards vanish from the very board that renamed it | internal/server echoOrigin + clientIDMiddleware | ✅ TestBatchEchoesReachTheirAuthor / TestAddressedCardStaysSuppressed |

| R+ | A review card's own STAGE drives its original exactly as its progress does: marking the review card done passes the review (the original leaves the review stage, a review-passed event names the reviewer), and lowering it below 100 — Reopen — sends the original back on review. Clearing the stage of a card at 100 changes nothing: it is complete whatever the stage says. Only the progress paths synced this, so "mark as done" on a review card left the original stuck on review with nothing in its log | boardservice.SetStage → syncReviewLink | ✅ TestReviewDoneByStagePassesTheOriginal / TestReopeningAReviewCardReturnsTheOriginal |

| S+ | A subtask left BEHIND in an earlier sprint still renders under its parent: a completed subtask stays in the sprint it was finished in while the parent carries on, and the parent's progress bar is derived from exactly those children. Hiding them (the old `activeSprint > sprintStart` rule) left a card handed to someone else reading 90% with no expand arrow and subtasks its new owner could see named in the log but could not open. Only a subtask whose day has not arrived — deferred ahead, or a sprint that had not started on the viewed day — stays hidden | web/src/subtasks.ts (shared by MeBoard and TeamBoard) | ✅ subtasks.test.ts |

| S2 | Ungrouping hands an OWNERLESS child the parent's person: a subtask usually has no assignee of its own (it rides the parent's), so a pull-out left it in Unassigned and off every personal board — from the person who pulled it, the card vanished. A patch that also names assignees wins, including an explicit empty one: assignees are applied AFTER the parent so a deliberate drop into Unassigned lands | boardservice.SetParent + handlePatchCard ordering | ✅ TestUngroupGivesTheParentsPerson / TestUngroupKeepsItsOwnPerson / TestPatchUngroupRespectsExplicitAssignees |
| W5 | The × on a worked card ASKS instead of deciding: a card in the current sprint with a previous one to fall back to, not created today, and carrying progress opens a two-way choice — keep it in the previous sprint (the old silent behaviour, subtasks riding along) or delete it outright. An untouched card still demotes silently, and everything else still deletes. The browser confirm is suppressed when the board asks, since a prompt reading "Delete?" in front of a demote is how the × came to be read as deletion | web/src/removal.ts + RemoveChoiceDialog, TeamBoard | ✅ removal.test.ts |
| W6 | The demote RECORDS its move (`sprint` event, from → to). It wrote start, sprint and day silently while the subtasks it dragged logged theirs, so a card left the board with nothing in its own history to explain it — twice the cause of a misread incident | boardservice.Remove | ✅ TestDemoteRecordsItsMove |

| S3 | A subtask's PERSON always follows its parent, like its team: grouping hands the child to the parent's assignee, a direct change on a subtask snaps back, and re-assigning (or unassigning) a parent cascades to every subtask. Without it a split family lands on two personal boards, since the Me view admits a card when you own one of its subtasks — one stray child dragged the parent and all its siblings onto a stranger's board | boardservice.SetAssignee + SetParent, mirrored in TeamBoard | ✅ TestSubtaskOwnerFollowsTheParent |

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

## Storage: git backend (2026-08-27)

Companion to [git-backend.md](git-backend.md). These rules are pinned BEFORE the backend exists: each test is written first in the package that owns the rule (`pkg/gitstore`, `pkg/board`, `pkg/boardservice`, `internal/server`, `cmd/aeman`, the migration, `pkg/apiserver`) and must fail before the code lands. Shallow-path tests (G9–G11) run against the real `git-upload-pack`/`git-receive-pack` binaries (go-git's in-process server has no shallow support); they skip locally when the binaries are absent and fail in CI (`AEMAN_TEST_REQUIRE_GIT=1`). G8 is hermetic via `SetShallow`. Rules about dates, sprints, visibility, review and carry-over do not change and keep their rows above.

| # | Rule | Lives in | Test |
| --- | --- | --- | --- |
| G1 | Card path = `cards/<a>/<b>/<id>.md`, `a`,`b` = the id's LAST two chars; the path never changes while the card exists in a domain (rename, move, re-zone, re-team keep it) | gitstore layout | 🆕 gitstore `TestCardPath*` |
| G2 | Empty fields are omitted on write; unknown front-matter keys survive a rewrite | gitstore file format | 🆕 gitstore `TestRewriteKeepsUnknownKeys`, `TestClearedFieldIsOmitted` |
| G3 | Derived states are never written: 100% writes `progress: 100` and `doneFrom` (a review passing 90 → 100 writes `doneFrom: 90`), never `stage: done`; In Progress is absent from every file | gitstore file format | 🆕 gitstore `TestDerivedStateNotStored` |
| G4 | One action = one commit per touched domain sharing `Aeman-Action-Id`; `Aeman-Cards` lists every id; carry-over with zero cards still commits the advanced pointer (team file only); a team already on today's sprint makes no commit | gitstore commit | 🆕 gitstore `TestCarryOverIsOneCommit`, `TestActionAcrossDomainsSharesID`, `TestCarryOverPointerOnlyCommit`, `TestCarryOverAlreadyTodayNoCommit` |
| G5 | Coalesced field writes commit once with the final value, keyed by `(op, card, actor)`; two actors on one slider → two attributed commits; an action on a card flushes its pending coalesced writes first (per-card FIFO) | server write queue | 🆕 server `TestCoalescedWritesCommitOnce`, `TestTwoActorsTwoCommits`, `TestActionFlushesCoalescedFirst` |
| G6 | Author = actor login (email from template), committer = server identity, date = action time; the sweep, title resolution, import and schema migration are authored `aeman` | gitstore commit | 🆕 gitstore `TestCommitIdentity*`, `TestSweepCommitIsUnattributed` |
| G7 | A card's log = commits touching it within the horizon, across `Aeman-Moved-From`; from/to from an `Aeman-Change` trailer first, the front-matter diff second; `truncatedBefore` set when the horizon cuts it | gitstore history | 🆕 gitstore `TestCardLog*`, `TestLogReadsChangeTrailer`, `TestLogFollowsMove`, `TestLogTruncatedBefore` |
| G8 | The history walker visits the shallow boundary commit and does not cross it (no error on a depth-1 clone) | gitstore history | 🆕 gitstore `TestWalkerStopsAtBoundary` |
| G9 | Deepening to a date applies `unshallow` and lands exactly at the horizon; a second deepen goes further; past the root leaves no shallow entry | gitstore sync | 🆕 gitstore `TestDeepenSince*` |
| G10 | Push rejection is detected by fetching, never by error type: nothing new → failed (commits kept, healthz age grows); new commits → re-apply and retry | gitstore sync | 🆕 gitstore `TestRejectedPush*` |
| G11 | Re-apply on a new tip re-runs the queued mutation closures on the card as it now is — field-level: disjoint cards both land; same card, different fields → both land; same field → the re-applied write wins, both commits in history; two replicas spawning the same iteration → one file, the loser's create is a no-op | gitstore sync | 🆕 gitstore `TestReapplyDisjoint`, `TestReapplySameCardDifferentFields`, `TestReapplySameFieldLastWins`, `TestSweepIsReplicaSafe` |
| G12 | A rank insertion touches one file; an exhausted key space rebalances only the run between the nearest roomy neighbours, in the same commit, never crossing into another domain | board rank + gitstore | 🆕 board `TestRankBetween*`, `TestRankRebalanceBounded`; gitstore `TestRebalanceStaysInDomain` |
| G13 | Roster fragments from several domains merge into one order; equal ranks tie-break by id; duplicate names resolve to the oldest `created`, the rest are aliases whose cards still count; healthz names them | gitstore roster | 🆕 gitstore `TestRosterMergeAcrossDomains`, `TestDuplicateTeamNameResolvesToOldest` |
| G14 | Domain follows the inheritance rule, LINKED CARDS FIRST, never a per-card choice: review card → reviewed card's domain (the edge: a closed-project original whose team lives in shared → closed, not shared); subtask → parent's even with its own column; iteration → task's; then project card → project's; then team card → team's; moving a card moves its review card and subtasks in the same action | gitstore + boardservice | 🆕 `TestReviewOfClosedCardStaysClosed`, `TestSubtaskWithOwnColumnFollowsParent`, `TestIterationFollowsTask`, `TestCardInheritsProjectDomain`, `TestTeamCardInheritsTeamDomain`, `TestMoveCascadesToLinkedCards` |
| G15 | (deferred until `feat/card-mirrors` lands — not in this repository yet) Mirror refuses a target column in another domain (`ErrCrossDomain`); the guard order is pinned when the branch lands | boardservice.Mirror | 🆕 `TestMirrorRefusesCrossDomain` |
| G16 | The reviewer picker offers only logins that can read the card's domain | server people roster | 🆕 server `TestReviewersFilteredByDomain` |
| G17 | An unreadable domain is absent from the snapshot AND the watch stream — no teams, projects, cards, placeholders or frames from it; an unreadable PRIMARY → 403, no board; a card whose team file is unreadable is served under its team name with the team's sprint controls unavailable | server board composition + watch hub | 🆕 server `TestUnreadableDomainAbsent`, `TestWatchFilteredByDomain`, `TestUnreadablePrimaryIs403`, `TestCardWithUnreadableTeamStillServed` |
| G18 | A newer `schema` is refused at startup; an older one is migrated in one commit | gitstore schema | 🆕 gitstore `TestSchemaNewerRefused`, `TestSchemaOlderMigrated` |
| G19 | Remote changes reach the cache by tree diff: a pushed commit touching one card updates that card only and is broadcast once, to readers of its domain | server sync | 🆕 server `TestRemoteCommitUpdatesOneCard` |
| G20 | Restart keeps unpushed commits: commit without push, reopen the store → queued and pushed; `aeman mcp` drains on exit | gitstore + cmd | 🆕 gitstore `TestUnpushedSurvivesReopen`, `TestMCPDrainsOnExit` |
| G21 | Repack + prune keep every object reachable | gitstore maintenance | 🆕 gitstore `TestRepackKeepsHistoryReadable` |
| G22 | A cross-domain move is create-THEN-delete with the same id and one action id; the created file carries `movedFrom:`/`movedAt:` so a torn move resolves from the tree alone — a fresh depth-1 clone of both domains shows the card once; the create hits disk before the delete; maintenance removes the ghost after the destination landed | gitstore + boardservice | 🆕 `TestMoveAcrossDomains`, `TestMoveCreatesBeforeDeleting`, `TestTornMoveResolvesAtDepthOne`, `TestMaintenanceRemovesGhost` |
| G23 | `Reopen` restores `doneFrom` regardless of history depth; no `doneFrom` → the in-progress nudge | boardservice.Reopen | 🆕 `TestReopenRestoresDoneFromPastHorizon`, `TestReopenWithoutDoneFromNudges` |
| G24 | `serve` refuses an unborn remote naming `aeman init`; `init` bootstraps `board.yaml` + `teams/_.yaml` in one commit and is idempotent | cmd + gitstore | 🆕 `TestServeRefusesEmptyRemote`, `TestInitBootstraps`, `TestInitTwiceNoop` |
| G25 | Every mutation requires `CanWrite` on its target domain(s); a read-only collaborator reads everything and changes nothing | server auth | 🆕 server `TestReadOnlyCollaboratorGets403`, `TestMoveChecksBothDomains` |
| G26 | Healthz reports the oldest unpushed commit age per domain and turns red past `--unpushed-warn` | server health | 🆕 server `TestHealthzUnpushedAge` |
| M1 | Migration: the final tree equals the snapshot byte for byte, whatever the event log says | migrate | 🆕 migrate `TestFinalTreeEqualsSnapshot` |
| M2 | Migration is idempotent: a second run on the migrated repository is a no-op without `--force` | migrate | 🆕 migrate `TestSecondRunNoop` |
| M3 | Migration ids are deterministic and every id-valued field (`parent`, `reviewOf`, `task`, state links) is remapped; a dangling reference is cleared and reported | migrate | 🆕 migrate `TestDeterministicIDs`, `TestReferencesRemapped`, `TestDanglingReferenceReported` |
| M4 | Migration reports everything it dropped or approximated: the `Status` field, issue cards reduced to `link`, unattributed notes, unapplied events, dangling references, and the id table | migrate | 🆕 migrate `TestReportNamesDrops` |
| M5 | Legacy `PVTI_` ids resolve on the API for one major version; unknown legacy id → 404 | apiserver | 🆕 apiserver `TestLegacyUIDResolves` |
