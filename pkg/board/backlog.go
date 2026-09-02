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

// NeedsTriage reports whether nobody has said WHEN the card's work is due: an
// open card of its own (no parent, no original) with no week (B3). The week
// is the whole of the decision — a card on today's board was put there by the
// day's planning, not by a week's, and until someone gives it a week it is
// work of unknown time. That is the pile the strip exists to show, however
// large it is at first.
func NeedsTriage(_ Board, c Card, _ string) bool {
	if IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) {
		return false
	}
	if c.Parent != "" || c.ReviewOf != "" || c.Week != "" {
		return false
	}
	return !Complete(c.Stage, c.Progress)
}

// BacklogWeekOf is the Monday of the column a card stands in on the Backlog
// board — its week, and nothing else. A card with no week stands in no
// column: it is in the strip, waiting for someone to say when.
func BacklogWeekOf(_ Board, c Card, _ string) string { return c.Week }

// WeeksCovered is every week a card occupies on the Backlog board: the week
// it was placed in, through the week its end date reaches. Stretching a card
// over three weeks says the work takes three weeks, and each of them counts
// it against what the team can do — a stretched card is not one week of work
// filed early.
//
// A card with no week covers none: it is in the strip, waiting for someone to
// say when. An end date inside (or before) its own week reaches nowhere.
func WeeksCovered(c Card) []string {
	if c.Week == "" {
		return nil
	}
	out := []string{c.Week}
	last := MondayOf(c.Day)
	for w := AddDays(c.Week, 7); last != "" && w <= last; w = AddDays(w, 7) {
		out = append(out, w)
	}
	return out
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
