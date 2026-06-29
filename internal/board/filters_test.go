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
	return NewBoard(nil, []Card{
		// Team A: a fresh card (one sprint day) and one carried into a later sprint.
		{ItemID: "A1", Team: "A", StartDate: "2026-06-22", SprintStart: "2026-06-22"},
		{ItemID: "Acarry", Team: "A", StartDate: "2026-06-15", SprintStart: "2026-06-22"},
		{ItemID: "B1", Team: "B", StartDate: "2026-06-22", SprintStart: "2026-06-22"},
		{ItemID: "N1", Team: "", StartDate: "2026-06-20", SprintStart: "2026-06-20"},
	})
}

func TestTeamGrid(t *testing.T) {
	b := gridBoard()
	cases := []struct {
		name      string
		team, day string
		want      []string
	}{
		{"new sprint day shows both A cards", "A", "2026-06-22", []string{"A1", "Acarry"}},
		{"original sprint day shows only the carried card", "A", "2026-06-15", []string{"Acarry"}},
		{"a mid span day shows the carried card", "A", "2026-06-18", []string{"Acarry"}},
		{"day past the span is empty", "A", "2026-06-23", []string{}},
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
	return NewBoard(nil, []Card{
		// Sprint pointers: A's current sprint is 06-22; B has moved on to 06-25.
		{ItemID: "ss-a", Title: SprintStateTitle, Team: "A", SprintStart: "2026-06-22"},
		{ItemID: "ss-b", Title: SprintStateTitle, Team: "B", SprintStart: "2026-06-25"},
		// u's current-sprint card for team A.
		{ItemID: "u1", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-22", SprintStart: "2026-06-22"},
		// u's card on a sprint team B has already left behind (no longer current).
		{ItemID: "u2", Assignees: []string{"u"}, Team: "B", StartDate: "2026-06-10", SprintStart: "2026-06-12"},
		// Assigned to u but never placed in a sprint — must never show.
		{ItemID: "u3", Assignees: []string{"u"}, Team: "A"},
		// Someone else's card.
		{ItemID: "u4", Assignees: []string{"v"}, Team: "A", StartDate: "2026-06-22", SprintStart: "2026-06-22"},
	})
}

func TestMeView(t *testing.T) {
	b := meBoard()
	cases := []struct {
		name      string
		user, day string
		want      []string
	}{
		{"on the sprint day", "u", "2026-06-22", []string{"u1"}},
		{"carried: stays while still the team's current sprint", "u", "2026-06-23", []string{"u1"}},
		{"a non-current carried sprint only shows within its range", "u", "2026-06-12", []string{"u2"}},
		{"before start and past a non-current sprint is empty", "u", "2026-06-21", []string{}},
		{"empty user sees everyone with a sprint", "", "2026-06-22", []string{"u1", "u4"}},
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
