package board

// closedInWindow calls fn for every card of the team closed in the last
// capacityWeeks complete weeks before today's — the window CapacityOf,
// CapacityOfPerson and the reactive share all read. Subtasks ride their
// parent (PointsOf weighs them there), state cards and review cards are
// not the team's work.
func closedInWindow(b Board, team, today string, fn func(Card)) {
	monday := MondayOf(today)
	from := AddDays(monday, -7*capacityWeeks)
	for _, c := range b.Cards {
		if c.Team != team || c.DoneAt == "" || c.DoneAt < from || c.DoneAt >= monday {
			continue
		}
		if c.Parent != "" || c.ReviewOf != "" || IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) {
			continue
		}
		fn(c)
	}
}

// ReactiveShareOf is the percentage of the points a team closed in the last
// four complete weeks that arrived OUTSIDE the plan — the yellow and red
// zones: work that turned up that day, or had to be done by its end. A week
// planned to the team's whole capacity cannot absorb them, so what can be
// planned is the capacity less this share (Plannable). Unknown, and 0, when
// the team closed nothing sized in the window: a client then plans against
// the whole capacity and says so.
func ReactiveShareOf(b Board, team, today string) (int, bool) {
	all, reactive := 0, 0
	closedInWindow(b, team, today, func(c Card) {
		pts := PointsOf(b, c)
		all += pts
		if c.Zone == ZoneYellow || c.Zone == ZoneRed {
			reactive += pts
		}
	})
	if all == 0 {
		return 0, false
	}
	return 100 * reactive / all, true
}

// Plannable is what a team can put into a week: its points a week less the
// share that history says will arrive on its own. With no share known, the
// whole capacity.
func Plannable(pointsAWeek, reactiveShare int, known bool) int {
	if !known {
		return pointsAWeek
	}
	return pointsAWeek * (100 - reactiveShare) / 100
}

// PointsAWeekOf is a team's weekly capacity in POINTS: the roster's number
// when one is set (Capacity.Week — the same field the team's card count
// used; a roster that sets it is now saying points), otherwise the points
// the team closed per complete week over the last four, averaged the way
// CapacityOf averages its cards. The second result says it was derived.
func PointsAWeekOf(b Board, team, today string) (int, bool) {
	if cap := b.SprintStates[team].Capacity; cap.Week > 0 {
		return cap.Week, false
	}
	total := 0
	closedInWindow(b, team, today, func(c Card) { total += PointsOf(b, c) })
	return total / capacityWeeks, true
}
