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
// on `anchor` comes due — the calendar arithmetic behind process tasks.
// The cycle is counted on the calendar from the anchor, never from when the
// last iteration happened to close, so a late March does not shift April.
// The per-sprint default has no calendar meaning and yields "".
func NextAfter(cycle, anchor, from string) string {
	a, err := time.Parse(isoLayout, anchor)
	if err != nil {
		// A date nobody can read is not a schedule. Better no due date than
		// one derived from garbage — a malformed anchor used to come back
		// verbatim and then be string-compared against the week's end.
		return ""
	}
	f, err := time.Parse(isoLayout, from)
	if err != nil {
		f = time.Time{} // no floor: the anchor itself is the answer
	}
	after := func(d time.Time) bool { return d.After(f) }

	switch cycle {
	case RecurrenceWeek, RecurrenceFortnight:
		days := 7
		if cycle == RecurrenceFortnight {
			days = 14
		}
		d := a
		if !after(d) {
			// Straight to the right turn instead of walking there: an anchor
			// twenty years back is a thousand steps, and a loop bound would
			// have to guess how many are too many.
			elapsed := int(f.Sub(d).Hours() / 24)
			d = d.AddDate(0, 0, (elapsed/days+1)*days)
			for !after(d) {
				d = d.AddDate(0, 0, days)
			}
		}
		return d.Format(isoLayout)

	case RecurrenceMonth, RecurrenceQuarter:
		months := 1
		if cycle == RecurrenceQuarter {
			months = 3
		}
		n := 0
		if !after(a) {
			// Months between the two dates, rounded down to whole turns.
			gap := (f.Year()-a.Year())*12 + int(f.Month()-a.Month())
			if gap > 0 {
				n = (gap / months) * months
			}
		}
		for d := addMonthsClamped(a, n); ; d = addMonthsClamped(a, n) {
			if after(d) {
				return d.Format(isoLayout)
			}
			n += months
		}
	}
	return "" // the per-sprint default has no calendar meaning
}

// addMonthsClamped adds whole months to a date, keeping the day of the month
// and clamping it to the target month's length: the 31st becomes the 28th in
// February and is the 31st again in March. Go's own AddDate rolls the
// overflow forward instead (31 Jan + 1 month = 3 Mar), which walks a monthly
// task off its day and skips February altogether.
func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, t.Location()).AddDate(0, months, 0)
	if last := first.AddDate(0, 1, -1).Day(); d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, t.Location())
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
