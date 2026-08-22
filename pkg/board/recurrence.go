package board

import "time"

// Recurrence cycles: how often a finished recurrent card reseeds a fresh copy
// at carry-over. The empty default keeps the original behaviour — every sprint.
const (
	RecurrenceSprint    = ""        // every sprint (default)
	RecurrenceWeek      = "week"    // reseed when the new sprint is ≥1 week past the card's sprint
	RecurrenceFortnight = "2weeks"  // ≥2 weeks past
	RecurrenceMonth     = "month"   // ≥1 calendar month past
	RecurrenceQuarter   = "quarter" // ≥3 calendar months past
)

// ValidRecurrence reports whether cycle is a storable recurrence value.
func ValidRecurrence(cycle string) bool {
	switch cycle {
	case RecurrenceSprint, RecurrenceWeek, RecurrenceFortnight, RecurrenceMonth, RecurrenceQuarter:
		return true
	}
	return false
}

// NextAfter is the first date strictly after `from` at which a cycle anchored
// on `anchor` comes due — the calendar arithmetic behind process templates.
// The cycle is counted on the calendar from the anchor, never from when the
// last iteration happened to close, so a late March does not shift April.
// The per-sprint default has no calendar meaning and yields "".
func NextAfter(cycle, anchor, from string) string {
	if anchor == "" {
		return ""
	}
	step := func(d string) string {
		switch cycle {
		case RecurrenceWeek:
			return AddDays(d, 7)
		case RecurrenceFortnight:
			return AddDays(d, 14)
		case RecurrenceMonth:
			return addMonths(d, 1)
		case RecurrenceQuarter:
			return addMonths(d, 3)
		}
		return ""
	}
	d := anchor
	if d > from {
		return d
	}
	for i := 0; i < 2000; i++ { // a bound, not a budget: ~38 years of weeks
		n := step(d)
		if n == "" || n <= d {
			return ""
		}
		d = n
		if d > from {
			return d
		}
	}
	return ""
}

// RecurrenceDue reports whether a recurrent card's next iteration is due on
// day (the sprint being started): always for the per-sprint default, and once
// the interval has elapsed since the sprint the card is bound to otherwise.
// A cycle card without a sprint anchor is never due — there is nothing to
// count the interval from.
func RecurrenceDue(c Card, day string) bool {
	switch c.Recurrence {
	case RecurrenceWeek:
		return c.SprintStart != "" && AddDays(c.SprintStart, 7) <= day
	case RecurrenceFortnight:
		return c.SprintStart != "" && AddDays(c.SprintStart, 14) <= day
	case RecurrenceMonth:
		return c.SprintStart != "" && addMonths(c.SprintStart, 1) <= day
	case RecurrenceQuarter:
		return c.SprintStart != "" && addMonths(c.SprintStart, 3) <= day
	default:
		return true
	}
}

// addMonths shifts an ISO date by whole calendar months (Go-normalised: Jan 31
// +1 month = Mar 2/3). A malformed date comes back unchanged.
func addMonths(iso string, months int) string {
	t, err := time.Parse(isoLayout, iso)
	if err != nil {
		return iso
	}
	return t.AddDate(0, months, 0).Format(isoLayout)
}
