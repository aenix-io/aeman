package board

import "time"

// Recurrence cycles: how often a finished recurrent card reseeds a fresh copy
// at carry-over. The empty default keeps the original behaviour — every sprint.
const (
	RecurrenceSprint = ""      // every sprint (default)
	RecurrenceWeek   = "week"  // reseed when the new sprint is ≥1 week past the card's sprint
	RecurrenceMonth  = "month" // reseed when the new sprint is ≥1 calendar month past
)

// ValidRecurrence reports whether cycle is a storable recurrence value.
func ValidRecurrence(cycle string) bool {
	switch cycle {
	case RecurrenceSprint, RecurrenceWeek, RecurrenceMonth:
		return true
	}
	return false
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
	case RecurrenceMonth:
		return c.SprintStart != "" && addMonths(c.SprintStart, 1) <= day
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
