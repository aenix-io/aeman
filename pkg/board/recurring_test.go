package board

import (
	"reflect"
	"testing"
)

// A recurrent card that came from no process comes round again all the same:
// carry-over reseeds it, and the weeks it will land in are as spoken for as a
// process turn's. The board drew projections for turns alone, so a team's own
// repeating work — the weekly report, the monthly invoice — showed as one
// card this week and empty weeks after it.
func TestARecurrentCardOfNoProcessIsProjectedToo(t *testing.T) {
	t.Parallel()
	card := Card{
		ItemID: "c1", Title: "Weekly report", Team: "portal",
		Stage: StageRecurrent, Recurrence: RecurrenceWeek,
		StartDate: "2026-09-02", Week: "2026-08-31",
	}
	b := Board{Cards: []Card{card}}

	got := UpcomingRecurrences(b, card, "2026-08-31", 4)
	want := []string{"2026-09-07", "2026-09-14", "2026-09-21"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpcomingRecurrences = %v, want %v", got, want)
	}

	// Its OWN week is never projected: the card is standing in it.
	for _, w := range got {
		if w == "2026-08-31" {
			t.Fatal("the card's own week is the card, not a projection")
		}
	}
}

// A week already holding a copy is not projected: reseeding is what the
// projection foretells, and a week that has had it is a card. Copies are
// known by what reseeding itself matches on — the title, within the team.
func TestARecurrentCardSkipsTheWeeksItsCopiesStandIn(t *testing.T) {
	t.Parallel()
	card := Card{
		ItemID: "c1", Title: "Weekly report", Team: "portal",
		Stage: StageRecurrent, Recurrence: RecurrenceWeek,
		StartDate: "2026-09-02", Week: "2026-08-31",
	}
	copy := Card{
		ItemID: "c2", Title: "Weekly report", Team: "portal",
		Stage: StageRecurrent, Recurrence: RecurrenceWeek, Week: "2026-09-07",
	}
	// Another team's card of the same name is another team's business.
	other := Card{
		ItemID: "c3", Title: "Weekly report", Team: "cozystack",
		Stage: StageRecurrent, Recurrence: RecurrenceWeek, Week: "2026-09-14",
	}
	b := Board{Cards: []Card{card, copy, other}}

	got := UpcomingRecurrences(b, card, "2026-08-31", 4)
	want := []string{"2026-09-14", "2026-09-21"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpcomingRecurrences = %v, want %v", got, want)
	}
}

// Nothing is projected where there is no calendar to project from.
func TestOnlyACalendarRecurrenceIsProjected(t *testing.T) {
	t.Parallel()
	base := Card{
		ItemID: "c1", Title: "x", Team: "portal", Stage: StageRecurrent,
		StartDate: "2026-09-02", Week: "2026-08-31",
	}
	for _, tc := range []struct {
		name string
		card Card
	}{
		// Per-sprint recurrence turns with the sprint, which is not a date.
		{"per sprint", base},
		{"not recurrent at all", Card{
			ItemID: "c1", Title: "x", Team: "portal",
			Recurrence: RecurrenceWeek, StartDate: "2026-09-02", Week: "2026-08-31",
		}},
		// A process turn is projected by its TASK's calendar (UpcomingTurns);
		// answering here as well would draw every one of them twice.
		{"a process turn", Card{
			ItemID: "c1", Title: "x", Team: "portal", Stage: StageRecurrent,
			Task: "t1", Recurrence: RecurrenceWeek, StartDate: "2026-09-02", Week: "2026-08-31",
		}},
		{"nobody placed it in a week", Card{
			ItemID: "c1", Title: "x", Team: "portal", Stage: StageRecurrent,
			Recurrence: RecurrenceWeek, StartDate: "2026-09-02",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UpcomingRecurrences(Board{Cards: []Card{tc.card}}, tc.card, "2026-08-31", 4); got != nil {
				t.Fatalf("UpcomingRecurrences = %v, want none", got)
			}
		})
	}
}

// The copy a sprint turn-over makes is the one that matters, and it has no
// week.
//
// Reseeding builds its copy from a title, a team, a zone and the day the
// sprint turned — a start and a day, never a week (Service.CarryOver). The
// dedup looked only at cards with a stored week, so it never matched the
// copies the board actually produces: the projection went on drawing a ghost
// in a week where the real reseeded card was already standing, which is the
// very double-counting the process projection was written to avoid.
func TestARecurrentCardSkipsTheWeekASeededCopyStandsIn(t *testing.T) {
	t.Parallel()
	card := Card{
		ItemID: "c1", Title: "Weekly report", Team: "portal",
		Stage: StageRecurrent, Recurrence: RecurrenceWeek,
		StartDate: "2026-09-02", Week: "2026-08-31",
	}
	seeded := Card{
		ItemID: "c2", Title: "Weekly report", Team: "portal",
		Stage: StageRecurrent, Recurrence: RecurrenceWeek,
		StartDate: "2026-09-07", Day: "2026-09-07", SprintStart: "2026-09-07",
	}
	b := Board{Cards: []Card{card, seeded}}

	got := UpcomingRecurrences(b, card, "2026-08-31", 4)
	want := []string{"2026-09-14", "2026-09-21"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UpcomingRecurrences = %v, want %v", got, want)
	}
}
