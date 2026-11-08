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
// place: not the board's bookkeeping, not a planning card (the Strategy board's),
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

// BacklogOrder sorts a backlog's cards the way it is read (Q2,
// docs/design/ordering.md): the reviews nobody has taken (K10) first, in a
// group of their own, then the tasks — both in the BOARD's order (rank,
// then id). A task's rank IS its place in its team's queue: nothing is
// inherited down the tree, so moving a goal or an initiative reorders no
// backlog, and a task may stand between any two others. A mix of teams is
// the same order over their cards.
func BacklogOrder(ix *Index, cards []Card) []Card {
	out := append([]Card(nil), cards...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := NeedsReviewer(out[i]), NeedsReviewer(out[j])
		if ri != rj {
			return ri
		}
		return ix.pos[out[i].ItemID] < ix.pos[out[j].ItemID]
	})
	return out
}

// UnassignTargets is what the Strategy board's UNASSIGNED means on a card (K16): on
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
