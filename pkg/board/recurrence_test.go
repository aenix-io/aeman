package board

import "testing"

// The calendar keeps a task on its day. Go's own month arithmetic rolls the
// overflow forward (31 Jan + 1 month = 3 Mar), which walks a monthly task off
// its date for good and skips February; a malformed anchor is no schedule at
// all and used to come back verbatim to be string-compared against a week.
func TestNextAfterHoldsItsDay(t *testing.T) {
	cases := []struct{ name, cycle, anchor, from, want string }{
		{"the 31st in a short month", "month", "2026-01-31", "2026-02-01", "2026-02-28"},
		{"and back to the 31st", "month", "2026-01-31", "2026-03-01", "2026-03-31"},
		{"and the 30th where there is no 31st", "month", "2026-01-31", "2026-04-01", "2026-04-30"},
		{"February in a leap year", "month", "2026-01-31", "2028-02-01", "2028-02-29"},
		{"a leap-day anchor", "month", "2028-02-29", "2028-03-01", "2028-03-29"},
		{"quarters keep the day", "quarter", "2026-01-31", "2026-02-01", "2026-04-30"},
		{"weekly from twenty years back", "week", "2000-01-03", "2026-08-20", "2026-08-24"},
		{"a fortnight lands on the anchor's weekday", "2weeks", "2026-08-17", "2026-08-20", "2026-08-31"},
		{"the anchor itself when nothing has passed", "week", "2026-09-07", "2026-08-20", "2026-09-07"},
		{"a malformed anchor is no schedule", "week", "not-a-date", "2026-08-20", ""},
		{"nor is an empty one", "month", "", "2026-08-20", ""},
		{"the per-sprint default has no calendar", "", "2026-08-17", "2026-08-20", ""},
		{"an unreadable floor falls back to the anchor", "week", "2026-08-17", "nonsense", "2026-08-17"},
	}
	for _, c := range cases {
		if got := NextAfter(c.cycle, c.anchor, c.from); got != c.want {
			t.Errorf("%s: NextAfter(%q, %q, %q) = %q, want %q",
				c.name, c.cycle, c.anchor, c.from, got, c.want)
		}
	}
}

// Whatever the cycle, the answer is strictly after the floor and never walks
// backwards — the spawn logic compares it against the end of a week.
func TestNextAfterIsStrictlyAfter(t *testing.T) {
	for _, cycle := range []string{RecurrenceWeek, RecurrenceFortnight, RecurrenceMonth, RecurrenceQuarter} {
		prev := ""
		from := "2026-08-20"
		for i := 0; i < 40; i++ {
			got := NextAfter(cycle, "2026-01-31", from)
			if got <= from {
				t.Fatalf("%s: NextAfter(from %s) = %s, not after it", cycle, from, got)
			}
			if got <= prev {
				t.Fatalf("%s: went backwards, %s after %s", cycle, got, prev)
			}
			prev, from = got, got
		}
	}
}
