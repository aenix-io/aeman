package board

import "sort"

// A BACKLOG is one ranked list per team: the tasks it has accepted and has
// not taken into work (docs/design/backlog.md). A task is in exactly one of
// two places (K1):
//
//   - IN WORK: it has a date or a sprint, and stands on the Team and Me
//     boards by the day rules;
//   - IN ITS TEAM'S BACKLOG: it has neither, and it is parked. It stands on
//     no day board.
//
// There is no third place. The Triage strip — open tasks nobody had dated,
// weeked or parked — is gone, and every door that used to leave a task there
// parks it instead (K3). A task with neither is UNPLACED: no door makes one,
// and the schema-3 bump parks the ones an older server left (K8).
//
// `parked` is a stored flag rather than derived from "no date, no sprint":
// a derived membership would put every undated card of history into a
// backlog at once, and the flag is what the doors already write.
//
// EVERY TEAM HAS ONE — the no-team group included, which is a team like any
// other here — and nothing declares it. It cannot be made, renamed or
// removed, so no card can ever be left pointing at a backlog that has gone.

// InBacklog reports whether the card is parked in its team's backlog. A
// parked card that also carries dates (a direct writer can write one) reads
// as parked: the day boards leave it out, so it is drawn once, where it can
// be dealt with.
func InBacklog(c Card) bool { return c.Parked }

// InWork reports whether a card has a date or a sprint — what puts a task on
// the day boards (K1).
func InWork(c Card) bool { return c.StartDate != "" || c.Day != "" || c.SprintStart != "" }

// isTaskOwnPlace reports whether a card is a task that answers for its own
// place: not the board's bookkeeping, not a planning card (the Roadmap's),
// not a review card (it follows
// the card it reviews) and not a subtask (it rides its parent).
func isTaskOwnPlace(c Card, subtask bool) bool {
	return !IsStateTitle(c.Title) && !IsPlanning(c) && c.ReviewOf == "" && !subtask
}

// OnBacklog reports whether a card is listed in its team's backlog (K2): a
// parked task that answers for its own place, and is not finished — the
// backlog never shows work that is over. isSubtask is handed in so an Index
// can answer it for a whole listing without a walk of the board per card.
func OnBacklog(c Card, isSubtask func(Card) bool) bool {
	if !InBacklog(c) || Complete(c.Stage, c.Progress) {
		return false
	}
	return isTaskOwnPlace(c, isSubtask(c)) || NeedsReviewer(c)
}

// NeedsReviewer reports a REVIEW NOBODY HAS TAKEN (K10): an open review card
// with no assignee. Putting a task on review without naming a reviewer makes
// one, parked in the task's team's backlog — the "needs a reviewer" item the
// backlog pins above its ranked tasks — and naming a reviewer takes it into
// work like a task. A review card with a reviewer follows the card it
// reviews, as ever.
func NeedsReviewer(c Card) bool {
	return c.ReviewOf != "" && len(c.Assignees) == 0 && !Complete(c.Stage, c.Progress)
}

// Unplaced reports an OPEN task in neither place: no date, no sprint, not
// parked — the shape the Triage strip used to hold. A review nobody has taken
// (NeedsReviewer) is asked the same: its place is its team's backlog. No door leaves a task
// like this any more (K3); schema 3 parks the ones an older server left
// (K8). Finished work is not asked to be placed: it is over.
func Unplaced(c Card, subtask bool) bool {
	if InWork(c) || InBacklog(c) {
		return false
	}
	if !isTaskOwnPlace(c, subtask) && !NeedsReviewer(c) {
		return false
	}
	return !Complete(c.Stage, c.Progress)
}

// BacklogKey is a backlog card's place in the list, comparable on its own
// (Less). The client mirrors it card by card (web/src/backlogboard.ts).
//
//   - Group: the EFFECTIVE priority (K15) — 1 for P0 … 5 for P4, 6 for none.
//   - Review: a review nobody has taken (K10) stands first within its group,
//     before the tasks; its group is the reviewed task's effective priority.
//   - Loose: the card has no plan item above it; within a group the loose
//     cards come after every planned one.
//   - Path: the places in the board's order from the root of the card's tree
//     down to the card itself (K14) — the Roadmap's draw order, which lists a
//     parent's children in board order. The first step is the root's level
//     ahead of its place, as the Roadmap draws its roots level by level (goals,
//     then initiatives with no goal, then epics with no initiative). A loose
//     card's path is its own place alone; a review's is the reviewed task's,
//     so the reviews of one group read in their tasks' Roadmap order.
type BacklogKey struct {
	Group  int
	Review bool
	Loose  bool
	Path   []int
}

// Less reports whether k stands before o.
func (k BacklogKey) Less(o BacklogKey) bool {
	if k.Group != o.Group {
		return k.Group < o.Group
	}
	if k.Review != o.Review {
		return k.Review
	}
	if k.Loose != o.Loose {
		return !k.Loose
	}
	for i := 0; i < len(k.Path) && i < len(o.Path); i++ {
		if k.Path[i] != o.Path[i] {
			return k.Path[i] < o.Path[i]
		}
	}
	return len(k.Path) < len(o.Path)
}

// BacklogKey is the card's place in its backlog (BacklogKey).
func (ix *Index) BacklogKey(c Card) BacklogKey {
	if NeedsReviewer(c) {
		k := BacklogKey{Loose: true, Path: []int{ix.pos[c.ItemID]}}
		if task, ok := ix.Find(c.ReviewOf); ok && task.ReviewOf == "" {
			k = ix.taskKey(task)
		}
		p, _ := ix.EffectivePriority(c)
		k.Group, k.Review = priorityRank(p), true
		return k
	}
	return ix.taskKey(c)
}

// taskKey is a task's place by its own effective priority and tree.
func (ix *Index) taskKey(c Card) BacklogKey {
	own := ix.pos[c.ItemID]
	p, _ := ix.EffectivePriority(c)
	group := priorityRank(p)
	up := ix.Lineage(c)
	planned := false
	for _, a := range up {
		if IsPlanning(a) {
			planned = true
			break
		}
	}
	if !planned {
		return BacklogKey{Group: group, Loose: true, Path: []int{own}}
	}
	path := make([]int, 0, len(up)+2)
	path = append(path, Level(TypeGoal)-Level(up[0].Type))
	for _, a := range up {
		path = append(path, ix.pos[a.ItemID])
	}
	return BacklogKey{Group: group, Path: append(path, own)}
}

// BacklogOrder sorts a backlog's cards the way it is read: by EFFECTIVE
// priority (K15) — P0, P1, P2, P3, P4, then none — each priority led by the
// reviews nobody has taken of its tasks (K10), then in the ROADMAP's order
// (K14) — see BacklogKey.
//
// One order, decided by the tree: dragging a task in the backlog moves it
// among its siblings under the same parent, in the same priority, which
// moves it on the Roadmap too, and a task's place is never something the
// backlog keeps apart from the Roadmap's. The board's order (rank, then id)
// is where the siblings' order comes from, as on every board.
func BacklogOrder(ix *Index, cards []Card) []Card {
	out := append([]Card(nil), cards...)
	keys := make(map[string]BacklogKey, len(out))
	for _, c := range out {
		keys[c.ItemID] = ix.BacklogKey(c)
	}
	sort.SliceStable(out, func(i, j int) bool { return keys[out[i].ItemID].Less(keys[out[j].ItemID]) })
	return out
}

// UnassignTargets is what the Roadmap's UNASSIGNED means on a card (K16): on
// a task, the task itself when it is open work in hand; on a plan item, every
// such task under it at any depth, in board order. Work in hand is a task
// with a date or a sprint that is not parked; finished work, a review card
// (it follows the card it reviews) and a subtask (it rides its parent) are
// never among them.
func (ix *Index) UnassignTargets(c Card) []Card {
	inHand := func(t Card) bool {
		return isTaskOwnPlace(t, ix.IsSubtask(t)) && InWork(t) && !InBacklog(t) && !Complete(t.Stage, t.Progress)
	}
	if !IsPlanning(c) {
		if inHand(c) {
			return []Card{c}
		}
		return nil
	}
	var out []Card
	for _, t := range ix.LeafTasks(c.ItemID) {
		if inHand(t) {
			out = append(out, t)
		}
	}
	return out
}
