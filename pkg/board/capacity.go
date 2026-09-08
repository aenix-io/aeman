package board

import "sort"

// Person is what the roster says about one login: for now, the points a
// week they get through, when a lead has set it (0 = not set; the board
// derives one). It is the primary repository's users/<login>.yaml.
type Person struct {
	// Capacity is the roster's number — points a week — or 0 for "derive
	// it" (see CapacityOfPerson).
	Capacity int `json:"capacity,omitempty"`
}

// capacityWeeks is how many complete weeks the derived capacity looks back
// over — the same four the team's own capacity uses (CapacityOf, B7).
const capacityWeeks = 4

// CapacityOfPerson is how many points a week a person gets through: the
// roster's number when somebody set one — a lead knows a person is on a half
// week, or new, or covering for two — and otherwise what the board has seen
// them close, read off doneAt and size from the tree alone. The second result
// says it was derived.
//
// The derived number is the MEDIAN of the points closed in each of the last
// four complete weeks in which the person closed anything. This week never
// counts: it is not over, and counting it would read a Monday as a slow week.
// Weeks with nothing closed are skipped rather than counted as zero — a week
// off is not a slow week, and the number beside a person on Monday must not
// be the memory of their holiday. A person with no record at all is 0 and
// derived: nothing to know, which a client shows as such rather than as a
// limit of none.
func CapacityOfPerson(b Board, login, today string) (int, bool) {
	if p, ok := b.People[login]; ok && p.Capacity > 0 {
		return p.Capacity, false
	}
	monday := MondayOf(today)
	from := AddDays(monday, -7*capacityWeeks)
	byWeek := map[string]int{}
	for _, c := range b.Cards {
		if c.DoneAt == "" || c.DoneAt < from || c.DoneAt >= monday {
			continue
		}
		if c.Parent != "" || IsStateTitle(c.Title) || IsPersonalDomain(c.Domain) || c.ReviewOf != "" {
			continue
		}
		if len(c.Assignees) == 0 || c.Assignees[0] != login {
			continue
		}
		// A week the person closed something in is a week that counts, even
		// if nothing in it was sized: the entry exists at 0 and is medianed.
		byWeek[MondayOf(c.DoneAt)] += PointsOf(b, c)
	}
	if len(byWeek) == 0 {
		return 0, true
	}
	weeks := make([]int, 0, len(byWeek))
	for _, pts := range byWeek {
		weeks = append(weeks, pts)
	}
	sort.Ints(weeks)
	n := len(weeks)
	if n%2 == 1 {
		return weeks[n/2], true
	}
	return (weeks[n/2-1] + weeks[n/2]) / 2, true
}
