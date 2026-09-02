package board

// The Backlog board: the weekly plan seen several weeks ahead, with a limit
// on each week (docs/design/backlog.md). A card in the backlog is a
// weekly-plan card placed in a week ahead; nothing here is stored beyond the
// card's own week and lane and the team's capacity.

// PlacedAhead reports whether the card is placed in a week ahead of today's.
// Such a card is on no day board until its Monday (B1): that is what makes
// the backlog a regulator rather than a list.
func PlacedAhead(c Card, today string) bool {
	return c.Week != "" && c.Week > MondayOf(today)
}

// LaneOf is the one reader of a card's lane. It derives the lane where the
// card's links already say it — a column slot and a process turn are the
// plan, a subtask and a review card belong to their card — and reads the
// stored value on every other card (B2).
func LaneOf(b Board, c Card) Lane {
	return laneOf(b, c, 0)
}

func laneOf(b Board, c Card, depth int) Lane {
	if c.Epic != "" || c.Task != "" {
		return LanePlan
	}
	if depth < 2 {
		owner := c.Parent
		if owner == "" {
			owner = c.ReviewOf
		}
		if owner != "" {
			for i := range b.Cards {
				if b.Cards[i].ItemID == owner {
					return laneOf(b, b.Cards[i], depth+1)
				}
			}
		}
	}
	return c.Lane
}

// LaneDerives reports whether the card's lane is decided by its links, so a
// stored lane on it would be ignored — a write of one is refused.
func LaneDerives(c Card) bool {
	return c.Epic != "" || c.Task != "" || c.Parent != "" || c.ReviewOf != ""
}

// NeedsTriage reports whether the card is one nobody placed: an open team
// card of its own (no parent, no original) with no week, not on the team's
// current sprint and not scheduled to a day ahead (B3). Cards on the day
// board are being worked; cards with a week are in the backlog already.
func NeedsTriage(b Board, c Card, today string) bool {
	if IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) {
		return false
	}
	if c.Parent != "" || c.ReviewOf != "" || c.Week != "" {
		return false
	}
	if Complete(c.Stage, c.Progress) {
		return false
	}
	if c.SprintStart != "" && c.SprintStart == CurrentSprint(b, c.Team) {
		return false
	}
	if c.StartDate != "" && c.StartDate > today {
		return false
	}
	return true
}

// BacklogWeekOf is the Monday of the column a card stands in on the Backlog
// board: its week when it has one; the week of the day it is scheduled to
// when that day is ahead; the current week for a card of the current
// sprint; "" for a card that is in no column (needs triage).
func BacklogWeekOf(b Board, c Card, today string) string {
	switch {
	case c.Week != "":
		return c.Week
	case c.StartDate != "" && c.StartDate > today:
		return MondayOf(c.StartDate)
	case c.SprintStart != "" && c.SprintStart == CurrentSprint(b, c.Team):
		return MondayOf(today)
	}
	return ""
}

// Default lane shares, in percent of the week, for a team that set none.
const (
	DefaultClientShare   = 30
	DefaultInternalShare = 10
)

// CapacityOf is the team's capacity: the roster's number when one is set,
// otherwise derived from the cards done in the last four complete weeks —
// read off doneAt, the tree alone (B7). The second result says it was
// derived. A team with no number and no doneAt at all has no limit
// (Week 0): nothing red until the board knows.
func CapacityOf(b Board, team, today string) (Capacity, bool) {
	cap := b.SprintStates[team].Capacity
	if cap.Client == 0 {
		cap.Client = DefaultClientShare
	}
	if cap.Internal == 0 {
		cap.Internal = DefaultInternalShare
	}
	if cap.Week > 0 {
		return cap, false
	}
	monday := MondayOf(today)
	from := AddDays(monday, -28)
	done, earliest := 0, ""
	for _, c := range b.Cards {
		if c.Team != team || c.DoneAt == "" || IsStateTitle(c.Title) {
			continue
		}
		if earliest == "" || c.DoneAt < earliest {
			earliest = c.DoneAt
		}
		if c.DoneAt >= from && c.DoneAt < monday {
			done++
		}
	}
	if earliest == "" {
		return cap, true
	}
	// Fewer than four weeks of doneAt on the board: average over the
	// complete weeks there are, one at least, rather than read a short
	// record as a slow team.
	weeks := 4
	if earliest > from {
		weeks = 0
		for w := MondayOf(earliest); w < monday; w = AddDays(w, 7) {
			weeks++
		}
		if weeks < 1 {
			weeks = 1
		}
	}
	cap.Week = (done + weeks - 1) / weeks
	return cap, true
}
