package board

import (
	"reflect"
	"testing"
)

func ids(cards []Card) []string {
	out := []string{}
	for _, c := range cards {
		out = append(out, c.ItemID)
	}
	return out
}

func gridBoard() Board {
	// TeamGrid places a card on its effective day: sprintStart once materialized
	// (startDate <= TodayIso()), else the future startDate. To stay independent of
	// the wall clock, materialized cards carry a startDate far in the past (always
	// <= today) and the deferred card one far in the future (always > today).
	return NewBoard(nil, []Card{
		// Team A: a materialized card — effective day is its sprint, 06-22.
		{ItemID: "A1", Team: "A", StartDate: "2000-01-01", SprintStart: "2026-06-22"},
		// Team A: a deferred card sharing the same sprint — its effective day is the
		// future startDate (2999-12-31), not the sprint day.
		{ItemID: "Afuture", Team: "A", StartDate: "2999-12-31", SprintStart: "2026-06-22"},
		// Team A: a materialized card on a different sprint day.
		{ItemID: "Aother", Team: "A", StartDate: "2000-01-01", SprintStart: "2026-06-26"},
		{ItemID: "B1", Team: "B", StartDate: "2000-01-01", SprintStart: "2026-06-22"},
		{ItemID: "N1", Team: "", StartDate: "2000-01-01", SprintStart: "2026-06-20"},
	})
}

func TestTeamGrid(t *testing.T) {
	b := gridBoard()
	cases := []struct {
		name      string
		team, day string
		want      []string
	}{
		{"materialized card shows on its sprint day; deferred one is not", "A", "2026-06-22", []string{"A1"}},
		{"deferred card shows on its own future day", "A", "2999-12-31", []string{"Afuture"}},
		{"another sprint day shows its own card", "A", "2026-06-26", []string{"Aother"}},
		{"a day with no card is empty", "A", "2026-06-23", []string{}},
		{"other team is isolated", "B", "2026-06-22", []string{"B1"}},
		{"no-team group", "", "2026-06-20", []string{"N1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ids(TeamGrid(b, c.team, c.day)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("TeamGrid(%q,%q) = %v, want %v", c.team, c.day, got, c.want)
			}
		})
	}
}

func meBoard() Board {
	// MeView groups a day's cards by activeSprint(team, day) and gates on
	// startDate <= day — both deterministic in the viewed day, so these cases do
	// not depend on the wall clock. Team A's current sprint is 06-26, previous 06-20.
	return NewBoard(nil, []Card{
		{ItemID: "ss-a", Title: SprintStateTitle, Team: "A", SprintStart: "2026-06-26", StartDate: "2026-06-20"},
		// u's card in the current sprint, scheduled on its sprint day.
		{ItemID: "u1", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-26", SprintStart: "2026-06-26"},
		// u's current-sprint card deferred to 06-28: hidden until that day arrives.
		{ItemID: "ufuture", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-28", SprintStart: "2026-06-26"},
		// u's card in the previous sprint: shows only on days inside [06-20, 06-26).
		{ItemID: "uprev", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-20", SprintStart: "2026-06-20"},
		// u's card from before the previous sprint: no active sprint matches it.
		{ItemID: "uold", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-10", SprintStart: "2026-06-10"},
		// Assigned to u but never placed in a sprint — must never show.
		{ItemID: "u3", Assignees: []string{"u"}, Team: "A"},
		// Someone else's current-sprint card.
		{ItemID: "u4", Assignees: []string{"v"}, Team: "A", StartDate: "2026-06-26", SprintStart: "2026-06-26"},
	})
}

func TestMeView(t *testing.T) {
	b := meBoard()
	cases := []struct {
		name      string
		user, day string
		want      []string
	}{
		{"current-sprint card shows on its sprint day", "u", "2026-06-26", []string{"u1"}},
		{"deferred current-sprint card appears once its day arrives", "u", "2026-06-28", []string{"u1", "ufuture"}},
		{"rolling back shows the previous sprint, hides the current", "u", "2026-06-22", []string{"uprev"}},
		{"before the previous sprint nothing shows", "u", "2026-06-19", []string{}},
		{"empty user sees everyone in the active sprint", "", "2026-06-26", []string{"u1", "u4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ids(MeView(b, c.user, c.day)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("MeView(%q,%q) = %v, want %v", c.user, c.day, got, c.want)
			}
		})
	}
}

func planBoard() Board {
	return NewBoard(nil, []Card{
		{ItemID: "p1", Team: "A", Plan: PlanWed, Week: "2026-06-15"},
		{ItemID: "p2", Team: "A", Plan: PlanFri, Week: "2026-06-15"},
		{ItemID: "p3", Team: "A", Plan: PlanWed, Week: "2026-06-22"},  // other week
		{ItemID: "p4", Team: "B", Plan: PlanWed, Week: "2026-06-15"},  // other team
		{ItemID: "p5", Team: "A", Plan: PlanNone, Week: "2026-06-15"}, // not a plan card
		{ItemID: "p6", Team: "", Plan: PlanFri, Week: "2026-06-15"},   // no-team group
	})
}

func TestWeeklyPlan(t *testing.T) {
	b := planBoard()
	cases := []struct {
		name    string
		team    string
		week    string
		wantWed []string
		wantFri []string
	}{
		{"team A, current week, split by band", "A", "2026-06-15", []string{"p1"}, []string{"p2"}},
		{"no-team group", "", "2026-06-15", []string{}, []string{"p6"}},
		{"other week keeps only its card", "A", "2026-06-22", []string{"p3"}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bands := WeeklyPlan(b, c.team, c.week)
			if got := ids(bands.Wed); !reflect.DeepEqual(got, c.wantWed) {
				t.Errorf("WeeklyPlan(%q,%q).Wed = %v, want %v", c.team, c.week, got, c.wantWed)
			}
			if got := ids(bands.Fri); !reflect.DeepEqual(got, c.wantFri) {
				t.Errorf("WeeklyPlan(%q,%q).Fri = %v, want %v", c.team, c.week, got, c.wantFri)
			}
		})
	}
}
