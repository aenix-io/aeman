package board

import "sort"

// A backlog is a shelf a card waits on. It is the THIRD place a card can be,
// and the one the board had no room for.
//
// Until now a card was either scheduled for a week — drawn on the boards,
// counted against the week — or in neither, which put it in the Triage strip.
// The strip therefore held two unlike things at once: work that arrived this
// morning and nobody has looked at, and an idea somebody read months ago and
// decided to keep. The first is a queue and should be short; the second is a
// shelf and may be long. Measuring them together makes the queue meaningless.
//
// So a card on a shelf is neither. It is not in the strip, because somebody
// HAS looked at it — putting it there is that act. And it is on no day board,
// because it is not planned: the same answer a week ahead gets, for the same
// reason. What a shelf keeps is the decision "not now".
//
// EVERY TEAM HAS ONE — the no-team group included, which is a team like any
// other here — and nothing declares it. A team that plans has work it is not
// planning; that is a fact about the team rather than something somebody sets
// up, and a backlog nobody had to create is one that is there the first time
// it is needed. It cannot be made, renamed or removed, so no card can ever be
// left pointing at a shelf that has gone.

// InBacklog reports whether the card is on its team's shelf.
func InBacklog(c Card) bool { return c.Parked }

// BacklogOrder sorts a shelf's cards the way it is read: the order somebody
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
