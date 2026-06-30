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
		// Team A: a card on its day and one moved (carried) to a later day. The
		// origin (StartDate) is left behind; only the day (SprintStart) places it.
		{ItemID: "A1", Team: "A", StartDate: "2026-06-22", SprintStart: "2026-06-22"},
		{ItemID: "Amoved", Team: "A", StartDate: "2026-06-20", SprintStart: "2026-06-26"},
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
		{"a card shows on its day", "A", "2026-06-22", []string{"A1"}},
		{"the moved card shows only on its new day", "A", "2026-06-26", []string{"Amoved"}},
		{"the moved card's origin day no longer shows it", "A", "2026-06-20", []string{}},
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
	return NewBoard(nil, []Card{
		// Team A's current sprint is 06-26 (previously 06-20).
		{ItemID: "ss-a", Title: SprintStateTitle, Team: "A", SprintStart: "2026-06-26", StartDate: "2026-06-20"},
		// u's card in the current sprint (a carried card: sprintStart == current).
		{ItemID: "u1", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-26", SprintStart: "2026-06-26"},
		// u's future-dated card: hidden until its day (06-28) arrives.
		{ItemID: "ufuture", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-28", SprintStart: "2026-06-28"},
		// u's done/old card from a past sprint (sprintStart < current) — never shows.
		{ItemID: "uold", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-20", SprintStart: "2026-06-20"},
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
		{"current-sprint card shows on its day", "u", "2026-06-26", []string{"u1"}},
		{"current-sprint card still shows on a later day", "u", "2026-06-27", []string{"u1"}},
		{"future-dated card appears once its day arrives", "u", "2026-06-28", []string{"u1", "ufuture"}},
		{"before the current sprint nothing shows", "u", "2026-06-25", []string{}},
		{"empty user sees everyone in the current sprint", "", "2026-06-26", []string{"u1", "u4"}},
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
