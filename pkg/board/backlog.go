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
// not a project card (the Project board's), not a review card (it follows
// the card it reviews) and not a subtask (it rides its parent).
func isTaskOwnPlace(c Card, subtask bool) bool {
	return !IsStateTitle(c.Title) && !IsPlanning(c) && c.Scope == "" && c.ReviewOf == "" && !subtask
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

// BacklogOrder sorts a backlog's cards the way it is read: the order somebody
// arranged by hand first, and where nobody has, the oldest first.
//
// Both halves are wanted. Dragging is how a backlog gets prioritised, so a
// card placed by hand must stay where it was put. But a shelf nobody has
// sorted should not read as random: the card that has waited longest is the
// one being asked about, so it goes to the top and the shelf answers "what has
// been sitting here" without anybody arranging it.
func BacklogOrder(cards []Card) []Card {
	out := append([]Card(nil), cards...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Rank != "") != (b.Rank != "") {
			// A card placed by hand outranks one that never was.
			return a.Rank != ""
		}
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		return a.CreatedAt < b.CreatedAt
	})
	return out
}
