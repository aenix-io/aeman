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

// PointsAWeekOf is a team's weekly capacity in POINTS: the capacities of the
// PEOPLE in it, added up. A team is the people in it — so a person away is a
// team short by exactly what that person gets through, and a lead who fixes
// one person's number moves their team's by the same amount, with nothing to
// keep in step by hand. The second result is always true: a team's number is
// derived by construction, there is no team number to set.
//
// A person who works for more than one team is split by their RECORD: their
// capacity is shared out in proportion to the points they closed in each team
// over the same four weeks, so the shares add back up to the person and no
// team counts the whole of somebody it only half has. Somebody with no closed
// record — a newcomer, whose capacity a lead has just set BECAUSE there is no
// record — counts where they are CARRYING work right now, split the same way.
func PointsAWeekOf(b Board, team, today string) (int, bool) {
	total := 0
	for login, share := range teamShares(b, today)[team] {
		capacity, _ := CapacityOfPerson(b, login, today)
		total += int(float64(capacity)*share + 0.5)
	}
	return total, true
}

// teamShares is, per team, how much of each person belongs to it: their
// closed points in that team over the window as a fraction of their closed
// points everywhere, falling back to the open work they are carrying for a
// person the window has nothing on.
func teamShares(b Board, today string) map[string]map[string]float64 {
	byPerson := map[string]map[string]int{}
	add := func(login, team string, pts int) {
		if login == "" || pts == 0 {
			return
		}
		if byPerson[login] == nil {
			byPerson[login] = map[string]int{}
		}
		byPerson[login][team] += pts
	}
	monday := MondayOf(today)
	from := AddDays(monday, -7*capacityWeeks)
	carrying := map[string]map[string]int{}
	for _, c := range b.Cards {
		if c.Parent != "" || c.ReviewOf != "" || IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) {
			continue
		}
		if len(c.Assignees) == 0 {
			continue
		}
		login := c.Assignees[0]
		if c.DoneAt >= from && c.DoneAt < monday && c.DoneAt != "" {
			add(login, c.Team, PointsOf(b, c))
			continue
		}
		if carriedNow(c, today) {
			if carrying[login] == nil {
				carrying[login] = map[string]int{}
			}
			carrying[login][c.Team] += PointsOf(b, c)
		}
	}
	// Somebody the window has nothing closed for is placed by what they hold.
	for login, teams := range carrying {
		if _, closed := byPerson[login]; !closed {
			byPerson[login] = teams
		}
	}
	out := map[string]map[string]float64{}
	for login, teams := range byPerson {
		total := 0
		for _, pts := range teams {
			total += pts
		}
		if total == 0 {
			continue
		}
		for team, pts := range teams {
			if out[team] == nil {
				out[team] = map[string]float64{}
			}
			out[team][login] = float64(pts) / float64(total)
		}
	}
	return out
}
