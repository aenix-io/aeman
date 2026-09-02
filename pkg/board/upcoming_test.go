package board

import (
	"reflect"
	"testing"
)

// The projection has to answer exactly as the sweep does, or the board draws
// a turn in a week the sweep will not file it in.
func TestDueInWeekAsksWhatTheSweepAsks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		recurrence string
		start      string
		week       string
		want       bool
	}{
		{"weekly, the week it starts in", RecurrenceWeek, "2026-09-02", "2026-08-31", true},
		{"weekly, every week after", RecurrenceWeek, "2026-09-02", "2026-09-07", true},
		{"a fortnight skips one", RecurrenceFortnight, "2026-09-02", "2026-09-07", false},
		{"and comes back the next", RecurrenceFortnight, "2026-09-02", "2026-09-14", true},
		{"monthly lands in the week its day falls in", RecurrenceMonth, "2026-09-02", "2026-09-28", true},
		{"and in none of the other three", RecurrenceMonth, "2026-09-02", "2026-10-05", false},
		{"the week the anchor day itself falls in", RecurrenceWeek, "2026-09-04", "2026-08-31", true},
		{"a week before the task begins", RecurrenceWeek, "2026-09-14", "2026-08-31", false},
		{"per sprint has no calendar at all", RecurrenceSprint, "2026-09-02", "2026-08-31", false},
		{"nor has an unreadable anchor", RecurrenceWeek, "not a date", "2026-08-31", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DueInWeek(tc.recurrence, tc.start, tc.week); got != tc.want {
				t.Fatalf("DueInWeek(%q, %q, %q) = %v, want %v",
					tc.recurrence, tc.start, tc.week, got, tc.want)
			}
		})
	}
}

func TestUpcomingTurnsAreTheOnesNobodyHasFiledYet(t *testing.T) {
	t.Parallel()
	task := Card{ItemID: "t1", Recurrence: RecurrenceWeek, StartDate: "2026-09-02"}

	t.Run("every week of the window", func(t *testing.T) {
		t.Parallel()
		got := UpcomingTurns(Board{Cards: []Card{task}}, task, "2026-08-31", 3)
		want := []string{"2026-08-31", "2026-09-07", "2026-09-14"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UpcomingTurns = %v, want %v", got, want)
		}
	})

	t.Run("but not a week whose turn is already a card", func(t *testing.T) {
		t.Parallel()
		// That turn IS on the board; drawing it twice would count it twice.
		b := Board{Cards: []Card{task, {ItemID: "i1", Task: "t1", Week: "2026-09-07"}}}
		got := UpcomingTurns(b, task, "2026-08-31", 3)
		want := []string{"2026-08-31", "2026-09-14"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UpcomingTurns = %v, want %v", got, want)
		}
	})

	t.Run("a task with no recurrence has nothing coming", func(t *testing.T) {
		t.Parallel()
		plain := Card{ItemID: "t2"}
		if got := UpcomingTurns(Board{Cards: []Card{plain}}, plain, "2026-08-31", 8); got != nil {
			t.Fatalf("UpcomingTurns = %v, want none", got)
		}
	})

	t.Run("a window of no weeks holds nothing", func(t *testing.T) {
		t.Parallel()
		if got := UpcomingTurns(Board{Cards: []Card{task}}, task, "2026-08-31", 0); got != nil {
			t.Fatalf("UpcomingTurns = %v, want none", got)
		}
	})

	t.Run("counts from the Monday of the day it is given", func(t *testing.T) {
		t.Parallel()
		got := UpcomingTurns(Board{Cards: []Card{task}}, task, "2026-09-02", 1)
		if !reflect.DeepEqual(got, []string{"2026-08-31"}) {
			t.Fatalf("UpcomingTurns = %v, want the week holding that day", got)
		}
	})
}
