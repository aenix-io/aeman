# Ordering: the epic order and the team queue

Status: approved by the owner 2026-11-08, with the decisions recorded below. Implemented on `feat/ordering-model` except the Backlog's Epics mode and its Split indicator in the SPA (`board.Index.Split` exists server-side): an epic is moved from the Strategy board, and every open Backlog takes the gathered order from the move's `Ordering` frame. This document is the source of truth for how cards are ordered; its rules are rows Q1–Q14 of [behavior-matrix.md](behavior-matrix.md). It supersedes K14 and amends K5, K10's ordering clause and K13 of [backlog.md](backlog.md).

## Summary

- A card keeps ONE `rank`. On an epic it is the board-wide **epic order**; on a task, its **team queue** place. Nothing inherits through Goal→Initiative; `BacklogKey` is retired.
- ONE function, `Place`, decides every default position: create, the Backlog "+", a team handover, a Strategy drop into another epic, a return from work whose old key is no longer valid, and the normalisation of unranked tasks.
- Moving an epic gathers its parked tasks into one block per team. The move is ONE action: one commit per repository it touches, applied to the server's cache under one lock, so no reader ever sees half of it.
- Schema 4 preserves every backlog order and every Strategy order exactly; the order of tasks in work inside a Team cell may change where nothing else allows it, and the migration reports those cells.

## Owner decisions (2026-11-08)

1. The seven recommendations of the plan are approved with their refinements: `Place` for every default position; a return to the backlog keeps the old key only inside the epic's area, else after the epic's last task; the Backlog "+" creates exactly in its gap and inherits the surrounding epic by the rule below, off by default for MCP; unranked tasks are normalised by the same `Place`; the epic order is global, team exceptions are only a documented extension point; Strategy drag-and-drop permutes slots within an epic, re-parents and places after the target epic's last task across epics, and gathers the block on an epic move.
2. **The migration may change the order of in-work tasks inside individual Team cells.** It reports every affected cell (`aeman ordering-report` prints the report without writing anything, so it can be read before the production run).
3. **Mixed children (plan items and tasks) of one parent are drawn by key** on the Strategy board.
4. **Atomicity is per repository.** There is no plan journaled in the primary's commit, no push ordering and no start-up recovery of another replica's operation. A mass move is one action through the write queue — one commit per repository it touches — and is applied to the cache under one lock.
5. **Undo is dropped from this change** (owner, 2026-11-08): it will come back later as a generic revert of an action. A move still answers with the `actionId` its keys were written under, which such a revert will name.
6. No P0/P1 priorities and no new workflow statuses.

## Why

Before this change a task's backlog place was the path of board-order places from the root of its tree down to it (K14). Reordering a goal or an initiative reshuffled the operational backlog, and a task could move among its siblings only. The owner wants epics to set the base order, individual tasks to be free exceptions, empty epics to be prioritisable before decomposition, new tasks to land in their epic's area on their own, and Strategy and Backlog to mean the same thing by the same gesture — without categorical priorities (K15 stays removed).

## Model

One `rank`, meaning by kind; keys of different kinds are never compared for meaning, though they share one key space and one board order.

- **Board order** — every card by (rank, id), merged across repositories, as before. An empty or invalid rank sorts last until normalisation gives it a key.
- **Epic order** — the board's epics in board order. Global: independent of goal and initiative, with no per-team ranks.
- **Queue Q(T)** — the open, parked, own-place tasks standing under team T (effective team, O13), in board order. Subtasks ride their parent; reviews nobody has taken keep their pinned group at the top (K10), in board order among themselves. A mixed multi-team view is the same filter over several teams.
- **A task's epic** is its nearest epic ancestor; a task with none is **neutral**. **E's area in Q(T)** is the gaps from "before E's first task" to "after E's last task", inclusive.
- **Strategy**: a parent's children by key, as before — mixed plan items and tasks too (decision 3). A flat range (one that starts at the epic level) draws the epics in the epic order. A display-mode switch writes nothing.
- **Keys**: `RankBetween` as before. Gathered blocks get evenly spaced keys between their neighbours; slot permutations reuse the existing values; a rebalance renumbers a bounded window around the insertion, never the whole list. Keys are computed in `pkg/board` over the MERGED board (`board.PlaceAfter`, `board.Reorder`); the backend writes the keys it is given (`Backend.Reorder`).

**Extension point — team exceptions to the epic order (not built).** Every algorithm takes the epic order as a parameter, `EpicOrder(T)`, which today is the same global order for every team. A later per-team exception would be an optional key in the team file, `epicRanks: {<epicId>: <key>}`, in the same key space; team T's effective key for E is `T.epicRanks[E] ?? E.rank`. An absent key means "global", so today's data needs no migration, and an older server keeps the key as unknown.

## Rules and algorithms

"Right after x" means a key between x and the card that follows x in board order, so the two are adjacent in every filtered view.

### Place — the one default position

```
explicit anchor                          → right after / right before it    (an explicit place always wins)
epic E has parked tasks in Q(T)          → right after the LAST of them
epic E with none in Q(T)                 → bestGap(Q(T), E)                 (Appendix A)
neutral, plan parent P has tasks in Q(T) → right after the last of them
otherwise                                → right after the last task of Q(T); empty Q → end of the board
```

Callers: create (Strategy, REST, MCP, agents), the Backlog "+" (explicit branch), a team handover, a Strategy drop into another epic, a return whose old key is invalid, and normalisation.

### Moving a task in the Backlog

A key between its neighbours in the merged list, any neighbours. The epic's key is unchanged; one file is written (two or more only when the key space between the neighbours is exhausted and a bounded window is renumbered).

### Moving an epic (Strategy and Backlog — one operation)

The epic gets a key between its neighbours in the epic order. Then, in **every** team holding its parked tasks: B = E's tasks in Q(T), in current order; Q′ = Q(T) without B; g = `bestGap(Q′, E)` under the new epic order; B gets keys right after Q′[g−1] (right before Q′[0] when g = 0). A block already contiguous there writes nothing. Tasks in work do not move. Owner §4: `A1 B2 A2 B1`, B moved above A → `B2 B1 A1 A2`.

### Strategy drag-and-drop

- **A task within its epic** — slot permutation among that epic's parked tasks of the task's team: the keys k1<…<km are handed out in the new order. Owner §5: `A1 B2 A2 B1 A3`, A3 first → `A3 B2 A1 B1 A2`. A task in work or done gets an ordinary key between its drawn neighbours.
- **A task into another epic** — a re-parent plus `Place` under the new epic: after that epic's last parked task in the task's team queue, or `bestGap` for an empty area. One action.
- **An epic** — the epic move above. A flat range never re-parents; in a grouped range, a drop under another parent re-parents and moves, as one action.
- **A goal or an initiative** — its own key only.
- The order between different teams' tasks inside one epic is derived and not set by a drag.

### Back to the backlog (unassign, ×, park)

The old key x is kept only if it is **still valid**; otherwise the task is placed by `Place` (after its epic's last parked task). Valid means:

- E has other tasks in Q(T): x falls inside E's area — strictly after the task preceding E's first task and strictly before the task following E's last task;
- E has no other task in Q(T): the gap x falls in has the **minimal** `bestGap` cost, i.e. it contradicts the epic order no more than the best place;
- a neutral task: the same test against its plan parent's area; with no plan parent, `Place` (the end of the queue).

Why this definition: it returns a task to its place when nothing changed (K14's intent); it never leaves the epic's own territory; it needs no history (when the task was taken); it is O(n). A task that was once first in the whole backlog is never put back on top once another epic stands there.

Examples, with A < B < C:

- A1 taken from the top and returned an hour later, queue `A2 A3 B1` unchanged: x is before A2, on A's area boundary, so it is valid → `A1 A2 A3 B1`.
- A1 taken a month ago; B has since been moved above A, and the queue is `B1 B2 A2 A3`. x now falls before B1, outside A's area → `B1 B2 A2 A3 A1`.
- Queue `A2 B1 A3`, x between A2 and B1: inside A's area (A2…A3), valid → `A2 A1 B1 A3`.
- No other A task, queue `B1 C1`, x before B1: cost 0 is the minimum → `A1 B1 C1`. With the epic order since changed to B < A < C → invalid, `bestGap` gives `B1 A1 C1`.

### Handover, re-parent, the "+"

- **Handover to another team** (a parked task's team changed): `Place` in the new team's queue — after its epic's last task there, else `bestGap`.
- **Re-parent through the parent picker**: keeps its queue place. It is a property edit, not an ordering gesture; Split shows the divergence.
- **Backlog "+"**: the place is exactly the gap pressed. The epic comes from context, with neighbours read off the list as drawn and reviews skipped:
  - a card above and below with the same epic → that epic;
  - above and below with different epics (a boundary) → the epic of the card above, which may be none;
  - a card on one side only (the top or bottom of the list) → that card's epic;
  - an empty list → none.

  The form shows the inherited epic as a chip with an ×. The team is the form's team choice. Server side this is `inheritParent: true` on a create with an anchor — the same code for REST and MCP — defaulting to false so an agent never files tasks under an epic silently.

### Normalisation of unranked tasks

Unranked tasks come from direct writers; an invalid key (empty, a character outside the alphabet, a trailing `0`, longer than 32) counts as none. One mechanism: `Normalize` applies `Place` to such open parked tasks in (created, id) order and writes them as one server action, `place`. It runs at start-up, after every fetch that brought changed cards (only those are checked), and at the end of the migration. It is deterministic over a board, so two replicas write the same keys. A writer-supplied valid rank is respected.

### Backlog Epics mode, dependencies

- **Epics mode** lists the selected teams' epics (by effective team, or by an open task standing under the team), empty ones included; epics with no team appear under "No team", and done epics are hidden. Each row shows the epic in the epic order, Goal/Initiative context, and its count of parked tasks. **Split** means another task stands inside the epic's area. Tasks ↔ Epics writes nothing.
- **Dependencies** never change the order. The All / Ready / Blocked filter reads `status.locked`, default All.

## Atomicity of the mass move

**Within one repository the move is atomic.** The service computes the whole plan — the list of `(card, before, after)` keys — over the merged board inside the store's critical section (`e.mu`), applies it to the cache under the same lock and queues it as ONE op of one action. The op writes every file of the plan under one `gitstore.WithScope`, which makes one commit per repository; a write that fails before the flush leaves nothing committed (the staged files are dropped) and the queue's failure path reloads the cache from the branch tip, so neither the cache nor git holds a partial move. Readers of the cache never see half: the planner, the apply and the `Ordering` frame all happen under the one lock.

**Across repositories it is not.** Team T's queue can span two repositories: T's own tasks live in T's repository, inherited-team tasks and the epic itself in the primary. `WithScope`'s flush commits the repositories one after another, and each is pushed on its own. If the second repository's commit fails after the first succeeded, the first half stays: the epic re-keyed and the primary's tasks gathered, the team repository's tasks where they were. The server reloads the cache from the tips, so every reader sees that state, and reports the failure as a sync error. The state is a valid board (every key is a key) — the block is just not contiguous yet; moving the epic again gathers the rest, because a gather is a target state and writes only what is out of place. This limit is accepted (owner decision 4) and pinned by Q12.

**Concurrent changes.** Two requests on one server are serialised by the store lock: each plans over the cache as the other left it. A fetch that brings another replica's commits in between is just another state to plan over. A rejected push replays the commit field by field (`gitstore.Repo.Rebase`), rank lines included; a block that another replica broke meanwhile stays as the replay leaves it until the next gather.

**What the client sees.** The acting tab shows the new order at once (the `queue.ts` mirror); the answer is `{actionId, moved}` — the action the keys were written under, and how many cards took one. Other tabs get ONE `Ordering` frame with the complete result. A refusal rolls the optimistic order back.


**Cost.** 1 + k files (k = the epic's parked tasks out of place); one commit per repository touched, typically one or two; O(n) compute on ~3300 cards; one `Ordering` frame as before.

## Migration: schema 3 → 4

**Goal.** Every backlog (and any mix of them), every Strategy display range and the flat epic order read EXACTLY as before, with as few keys rewritten as possible.

**The limit.** One key per card cannot always preserve Strategy, the backlog AND the Team cells at once (the cycle in Appendix D). Backlog and Strategy are preserved exactly; the order of tasks in work inside a Team cell changes only where it is forced, and those cells are reported (owner decision 2).

1. **Constraints.** A parent's children keep their old order; the roots of each level keep theirs; backlog cards keep the old backlog order (the K14 tree walk, reviews first); epics keep the old flat order (the tree walk). These are compatible, because each is a restriction of one tree walk. A topological sort runs with the old (rank, id) as priority.
2. **Keys.** Cards already in that order with strictly increasing valid keys (the longest increasing subsequence) keep them; the rest get evenly spaced keys between their kept neighbours.
3. **One pass over every repository.** The per-repository bumps stop at schema 3; schema 4 needs the merged board, so it runs once all repositories are open (`gitstore.MigrateOrdering`). The plan is computed from the PRE-bump trees — for a repository already at 4, the parent of its schema commit — so an interrupted run writes the same things when resumed. One commit per repository (marker + rank lines); normalisation follows.
4. **Report.** The migration returns, and `aeman ordering-report` prints without writing, the files that would be rewritten and every Team cell — team, person, zone — whose in-work order changes.
5. **External writers** (`git-backend.md` "Ordering", `plugin-impact.md`): an epic's `rank` is the global epic order and a task's is its queue place; reorder with a key between any neighbours; a new task may omit `rank` (normalisation places it); a multi-file ordering change should be one commit per repository.

## Changes by layer

- **`pkg/board`** — `queue.go`: `Queue`, `EpicOf`, `Place`, `BestGap`, `ValidReturn`, `MoveEpicPlan`, `ReorderInEpicPlan`, `InheritedEpic`, `Split`, `EpicOrder`; `PlaceAfter` with a windowed rebalance; `Normalize`. `BacklogKey` goes; `BacklogOrder` is the reviews first, then board order.
- **`pkg/boardservice`** — ordering operations through `Backend.Reorder` (a planner run over the merged board, keys written as given); `Place` at every door; `Normalize`.
- **`pkg/gitstore`** — `Reorder` on `Backend` and `MultiBackend`; `MigrateOrdering` (schema 4); defects 1–2.
- **`internal/server`** — the planner under the store lock and one queued op per action; normalisation at start-up and after a fetch.
- **API / OpenAPI / MCP** — `move` on an epic gathers and answers 200 `{actionId, moved}`; `reorder-in-epic`; `inheritParent` on create; MCP `reorder_in_epic`, `inheritParent` on `create_card`; the `parked` text fixed.
- **`web`** — `queue.ts` mirror; Backlog drops anywhere, the "+" with an epic chip; Strategy's flat epic list and the drop rules above; the Backlog's Tasks | Epics switch with Split, and Split on the Strategy board's epic rows. Undo is not part of this change (owner decision, 2026-11-08): it will come back as a generic revert of an action, which the `actionId` a move answers with will name.

## Defects found by the analysis (fixed first)

1. An anchor in another repository yielded the key `i` — an arbitrary place.
2. Rebalance renumbered the whole repository list, although G12 promises a bounded run.
3. The Backlog "+" between epic tasks made a loose task that sorted to the end (K13 broken).
4. An empty rank sorted first; the docs said last.
5. MCP `parked` promised "top", but the service kept the place.

## Rules pinned (matrix rows)

| Row | Rule |
| --- | --- |
| Q1 | A card's `rank` orders it within its kind: one global epic order, one queue per team, goals and initiatives among their own; keys of different kinds are never compared for meaning. |
| Q2 | The backlog is the reviews nobody has taken, then the team's queue in board order; the Tasks/Epics switch and every Strategy display range write nothing. |
| Q3 | Every default position is `Place`: after the epic's last parked task in the team's queue, else the least-contradicting gap, else after the plan parent's last parked task, else the end of the queue; an explicit place always wins. |
| Q4 | A task moved in the backlog goes between any two neighbours; its epic's key does not change. |
| Q5 | Moving an epic — from Strategy or Backlog — re-keys it and gathers its parked tasks into one contiguous block per team, internal order kept; tasks in work and other epics' tasks stay. |
| Q6 | In Strategy a task moved within its epic permutes that epic's parked tasks of its team over their slots; a task dropped into another epic is re-parented and placed by Q3 in the new epic's area. |
| Q7 | Moving a goal or an initiative rewrites its own key only; a flat-range epic drop never re-parents. |
| Q8 | A task back from work keeps its key only while the key is inside its epic's area (or, with no other task of the epic there, in a minimal-cost gap); otherwise Q3. |
| Q9 | A task handed to another team is placed by Q3 in that team's queue; a re-parent through the picker keeps its place. |
| Q10 | The Backlog "+" creates exactly in its gap and inherits the epic of its context (the same epic above and below; at a boundary the card above's; at an end the only neighbour's; none in an empty list) when asked to. |
| Q11 | An unranked or invalidly ranked task is placed by Q3 by normalisation at start-up, after a fetch that changed it, and after a migration. |
| Q12 | A multi-file ordering action is planned and applied to the cache under one lock and written as one action, one commit per repository; a failed write leaves no partial move within a repository, in the cache or in git. Across repositories the halves are separate commits (the documented limit). |
| Q13 | ~~Undo~~ — dropped 2026-11-08 (owner decision): a generic revert of an action will take its place. |
| Q14 | Schema 4 preserves every team's backlog order, every Strategy range and the flat epic order exactly, resumably and idempotently, and reports the Team cells whose in-work order changed. |

## Test plan

### Exact order preservation after migration

- **Real-data test** `TestSchema4PreservesEveryOrderOnARealBoard`. Opt-in via `AEMAN_ORDER_FIXTURE=<a bare copy of the board's repositories>`, skipped in CI, never run on a live clone. It clones the copy into a temporary directory and asserts that every team's backlog and all teams mixed are identical before and after; that every parent's children, every level's roots and the flat epic order are identical; and that ids and links are unchanged. A second run writes nothing. It prints the files rewritten and the Team cells changed.
- **Generated property test** `TestSchema4PreservesOrderOnGeneratedBoards`: random forests, teams, domains and states; keys with ties, empty and invalid ones; the same assertions, plus: an interruption after the first repository and a resume give the same keys as an uninterrupted run.
- **Examples**: mixed initiative children, loose tasks last, reviews first, empty epics, and a forced Team-cell change (the Appendix D cycle).

### Atomicity of the mass move

- **Fault injection** within a repository: a write that fails part-way through an epic gather leaves no commit and no partial order in the cache (`TestAFailedGatherLeavesNoPartialMove`).
- **One frame**: an epic move sends one `Ordering` frame holding the whole block.

### Creating tasks in empty epics — every door

One board — empty epics A < B < C; team T's queue holding other epics' and neutral tasks — through every door: the service create with a parent (the Strategy's "+" and REST), the Backlog "+" with `inheritParent` (an explicit place wins; inheritance in all four cases), MCP `create_card`, an MCP agent scenario (create epics, prioritise with `move_card`, create a batch of tasks — creation order kept), and a direct git writer (a file with no `rank` and one with an invalid `rank`, then normalisation; two replicas yield the same key; a writer-supplied rank is respected). The expected queues are identical across doors and equal the TS mirror's (`queue.test.ts`).

## Appendix A — bestGap

For a queue Q′ and epic E, gap g costs `#{q ∈ Q′[0:g] : epic(q) after E} + #{q ∈ Q′[g:] : epic(q) before E}`; neutral tasks count nothing. Adjacent equal-cost gaps form a run. Take the earliest minimal run; within it, its first gap when the run starts right after a task of an earlier epic, otherwise its last (with nothing counted, the end of the queue). With A < B < C: `L1 L2` → `L1 L2 B`; `A1 L1 L2 C1` → `A1 B L1 L2 C1`; `L1 L2 C1` → `L1 L2 B C1`; `C9 A1 A2 C1` → `C9 A1 A2 B C1`; `A1 C1 A9` → `A1 B C1 A9`.

## Appendix B — the conflicts of the brief's §12

- **C1**: one epic order, many queues with exceptions — an area is derived (`bestGap`), never stored.
- **C2**: different teams' tasks of one epic cannot be reordered against each other without moving a queue — permutation happens within a team.
- **C3**: tasks in work have no slot — the permutation excludes them.
- **C4**: an epic move erases only its own tasks' exceptions.
- **C5**: "after the last" follows a sunk exception — accepted; Split shows it.
- **C6**: Strategy grouping and the flat epic order may disagree — accepted.
- **C7**: a flat-range epic drop never re-parents; a grouped one may.
- **C8**: one key cannot preserve Strategy, backlog and Team cells together (Appendix D).

## Appendix D — migration detail

**The cycle that forbids preserving all three orders.** Take x, a parked A task with key 50; y′, an A task in work, 40; x′, a parked B task, 10; y, a B task in work, 20; y and y′ share a Team cell. The backlog needs x < x′; Strategy under B needs x′ < y; the Team cell needs y < y′; Strategy under A needs y′ < x. That is a cycle, so the Team-cell order is the lowest-priority constraint.

**Construction.** Build the constraint graph (children per parent, roots per level, backlog cards, epics), run Kahn's algorithm with the old (rank, id) as priority, keep the longest strictly increasing subsequence of valid old keys, and give the rest keys between their kept neighbours (`RankRebalance`). O(n log n).
