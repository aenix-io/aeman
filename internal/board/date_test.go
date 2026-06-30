package board

import (
	"testing"
	"time"
)

func TestActiveOnDay(t *testing.T) {
	cases := []struct {
		name          string
		start, finish string
		day           string
		want          bool
	}{
		{"neither bound", "", "", "2026-06-15", false},
		{"start only, on day", "2026-06-10", "", "2026-06-10", true},
		{"start only, before", "2026-06-10", "", "2026-06-09", false},
		{"start only, after", "2026-06-10", "", "2026-06-11", false},
		{"finish only, on day", "", "2026-06-10", "2026-06-10", true},
		{"finish only, before", "", "2026-06-10", "2026-06-09", false},
		{"range, on start", "2026-06-10", "2026-06-20", "2026-06-10", true},
		{"range, in middle", "2026-06-10", "2026-06-20", "2026-06-15", true},
		{"range, on finish", "2026-06-10", "2026-06-20", "2026-06-20", true},
		{"range, before", "2026-06-10", "2026-06-20", "2026-06-09", false},
		{"range, after", "2026-06-10", "2026-06-20", "2026-06-21", false},
		// start later than finish (a card added on a day past its sprint): the
		// span collapses to its start day rather than vanishing.
		{"start past finish, off its day", "2026-06-20", "2026-06-10", "2026-06-15", false},
		{"start past finish, on its day", "2026-06-20", "2026-06-10", "2026-06-20", true},
		{"start past finish, after its day", "2026-06-20", "2026-06-10", "2026-06-21", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveOnDay(c.start, c.finish, c.day); got != c.want {
				t.Errorf("ActiveOnDay(%q,%q,%q) = %v, want %v", c.start, c.finish, c.day, got, c.want)
			}
		})
	}
}

func TestMondayOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2026-06-15", "2026-06-15"}, // Monday → itself
		{"2026-06-17", "2026-06-15"}, // Wednesday
		{"2026-06-21", "2026-06-15"}, // Sunday (end of week)
		{"2026-06-22", "2026-06-22"}, // next Monday
		{"2026-07-01", "2026-06-29"}, // Wednesday, Monday is in the prior month
		{"garbage", "garbage"},       // unparseable → unchanged
	}
	for _, c := range cases {
		if got := MondayOf(c.in); got != c.want {
			t.Errorf("MondayOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAddDays(t *testing.T) {
	cases := []struct {
		in    string
		delta int
		want  string
	}{
		{"2026-06-15", 0, "2026-06-15"},
		{"2026-06-30", 1, "2026-07-01"},  // month boundary
		{"2026-06-01", -1, "2026-05-31"}, // month boundary back
		{"2026-12-31", 1, "2027-01-01"},  // year boundary
		{"2026-03-01", -1, "2026-02-28"}, // non-leap February
		{"2026-06-15", 7, "2026-06-22"},  // a week ahead
		{"garbage", 3, "garbage"},        // unparseable → unchanged
	}
	for _, c := range cases {
		if got := AddDays(c.in, c.delta); got != c.want {
			t.Errorf("AddDays(%q,%d) = %q, want %q", c.in, c.delta, got, c.want)
		}
	}
}

func TestDaysSince(t *testing.T) {
	// Build the timestamp from local noon so converting to a local date never
	// crosses a day boundary, keeping the test timezone-independent.
	noon := time.Date(2026, 6, 25, 12, 0, 0, 0, time.Local).Format(time.RFC3339)

	cases := []struct {
		name      string
		iso, asOf string
		want      int
	}{
		{"empty iso", "", "2026-06-30", 0},
		{"unparseable iso", "garbage", "2026-06-30", 0},
		{"five days", noon, "2026-06-30", 5},
		{"same day", noon, "2026-06-25", 0},
		{"asOf before never negative", noon, "2026-06-20", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DaysSince(c.iso, c.asOf); got != c.want {
				t.Errorf("DaysSince(%q,%q) = %d, want %d", c.iso, c.asOf, got, c.want)
			}
		})
	}
}

func TestLocalDateIso(t *testing.T) {
	if got := LocalDateIso("garbage"); got != "" {
		t.Errorf("LocalDateIso(garbage) = %q, want empty", got)
	}
	if got := LocalDateIso(""); got != "" {
		t.Errorf("LocalDateIso(empty) = %q, want empty", got)
	}
	// Local noon stays on the same local date in every timezone.
	tm := time.Date(2026, 6, 25, 12, 0, 0, 0, time.Local)
	if got := LocalDateIso(tm.Format(time.RFC3339)); got != "2026-06-25" {
		t.Errorf("LocalDateIso(local noon) = %q, want 2026-06-25", got)
	}
}

func TestTodayIso(t *testing.T) {
	got := TodayIso()
	if _, err := time.Parse(isoLayout, got); err != nil {
		t.Errorf("TodayIso() = %q, not a yyyy-mm-dd date: %v", got, err)
	}
}
