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
	return NewBoard([]Card{
		// Team A: a materialized card — effective day is its sprint, 06-22.
		{ItemID: "A1", Team: "A", StartDate: "2000-01-01", SprintStart: "2026-06-22"},
		// Team A: a deferred card sharing the same sprint — its effective day is the
		// future startDate (2999-12-31), not the sprint day.
		{ItemID: "Afuture", Team: "A", StartDate: "2999-12-31", SprintStart: "2026-06-22"},
		// Team A: a materialized card on a different sprint day.
		{ItemID: "Aother", Team: "A", StartDate: "2000-01-01", SprintStart: "2026-06-26"},
		// Team A: created on a later day of the 06-22 sprint — shows on both days.
		{ItemID: "Alater", Team: "A", StartDate: "2026-06-24", SprintStart: "2026-06-22"},
		// Team A: deferred — its scheduled day pushed into the far future while it
		// stays in the 06-22 sprint. Hidden between today and that day; its past
		// sprint day and its future slot stay visible.
		{ItemID: "Adeferred", Team: "A", StartDate: "2999-12-30", SprintStart: "2026-06-22"},
		// Team A: a ranged card (start…end) — shows on every day of 06-27..06-29.
		{ItemID: "Aspan", Team: "A", StartDate: "2026-06-27", Day: "2026-06-29", SprintStart: "2026-06-22"},
		{ItemID: "B1", Team: "B", StartDate: "2000-01-01", SprintStart: "2026-06-22"},
		{ItemID: "N1", Team: "", StartDate: "2000-01-01", SprintStart: "2026-06-20"},
		// Team A's sprint pointers: current = 06-26, previous = 06-22. Cards also
		// show on pointer days their sprint passed through (carried / deferred).
		{ItemID: "Astate", Team: "A", Title: SprintStateTitle, SprintStart: "2026-06-26", StartDate: "2026-06-22"},
	})
}

func TestTeamGrid(t *testing.T) {
	b := gridBoard()
	cases := []struct {
		name      string
		team, day string
		want      []string
	}{
		{"a sprint day keeps all its cards, deferred ones included", "A", "2026-06-22", []string{"A1", "Afuture", "Aother", "Alater", "Adeferred", "Aspan"}},
		{"future-scheduled card shows on its own future day", "A", "2999-12-31", []string{"Afuture"}},
		{"another sprint day shows its own card", "A", "2026-06-26", []string{"Aother"}},
		{"a later-created card also shows on its scheduled day", "A", "2026-06-24", []string{"Alater"}},
		{"a deferred card is hidden between today and its scheduled day", "A", "2998-01-01", []string{}},
		{"a deferred card shows on its new scheduled day", "A", "2999-12-30", []string{"Adeferred"}},
		{"a ranged card shows on its start day", "A", "2026-06-27", []string{"Aspan"}},
		{"a ranged card shows mid-range", "A", "2026-06-28", []string{"Aspan"}},
		{"a ranged card shows on its end day", "A", "2026-06-29", []string{"Aspan"}},
		{"a ranged card is gone after its end day", "A", "2026-06-30", []string{}},
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
	return NewBoard([]Card{
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
		// A "next sprint" create: sprint-less but scheduled — shows from its
		// day on until a carry-over adopts it into a sprint.
		{ItemID: "unext", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-27"},
		// An old sprint-less stray, scheduled before the tracked sprints: it
		// stays on its own past days and never resurfaces on current ones.
		{ItemID: "ustray", Assignees: []string{"u"}, Team: "A", StartDate: "2026-06-15"},
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
		{"sprint-less scheduled card shows from its day on", "u", "2026-06-27", []string{"u1", "unext"}},
		{"deferred current-sprint card appears once its day arrives", "u", "2026-06-28", []string{"u1", "ufuture", "unext"}},
		{"rolling back shows the previous sprint, hides the current", "u", "2026-06-22", []string{"uprev"}},
		{"an old sprint-less stray lives on its own past days only", "u", "2026-06-19", []string{"ustray"}},
		{"before the stray's day nothing shows", "u", "2026-06-14", []string{}},
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
	return NewBoard([]Card{
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

// Deferring a card takes it out of the sprint in progress at once — even when
// that sprint opened days ago (no carry-over since), so its start day is in
// the past. A genuinely CLOSED sprint's day still keeps the card as history.
func TestTeamGridDeferredLeavesCurrentSprint(t *testing.T) {
	today := TodayIso()
	current := AddDays(today, -3) // sprint opened three days ago, still current
	previous := AddDays(today, -6)
	deferred := Card{
		ItemID: "c1", Team: "alpha",
		StartDate:   AddDays(today, 7), // deferred a week out
		Day:         current,           // stale end date, as Defer leaves it
		SprintStart: current,
	}
	b := Board{
		Cards: []Card{deferred},
		SprintStates: map[string]SprintState{
			"alpha": {Current: current, Previous: previous},
		},
	}
	if got := TeamGrid(b, "alpha", current); len(got) != 0 {
		t.Fatalf("deferred card must leave the current sprint day, got %+v", got)
	}
	if got := TeamGrid(b, "alpha", today); len(got) != 0 {
		t.Fatalf("deferred card must not show on today, got %+v", got)
	}
	if got := TeamGrid(b, "alpha", deferred.StartDate); len(got) != 1 {
		t.Fatalf("deferred card must show on its own future day, got %+v", got)
	}

	// The same card bound to a CLOSED sprint keeps that day as history.
	past := deferred
	past.SprintStart = previous
	hist := Board{
		Cards: []Card{past},
		SprintStates: map[string]SprintState{
			"alpha": {Current: current, Previous: previous},
		},
	}
	if got := TeamGrid(hist, "alpha", previous); len(got) != 1 {
		t.Fatalf("a closed sprint's day must keep the card as history, got %+v", got)
	}
}

// A Project-board slot has no stored plan band, yet it IS the week's work for
// every week its span covers: the panel derives its band from the end date
// instead of dropping it. The stored band, when present, always wins — deriving
// must never move a card someone placed by hand.
func TestWeeklyPlanDerivesSlotBands(t *testing.T) {
	const week, today = "2026-08-24", "2026-08-24"
	mk := func(id, wk, day string, plan PlanBand) Card {
		return Card{ItemID: id, Title: id, Team: "t", Epic: "E", Week: wk, Day: day, Plan: plan}
	}
	cases := []struct {
		name string
		card Card
		wed  bool // expected in the Wed band
		fri  bool // expected in the Fri band
	}{
		{"ends by Wednesday -> Wed", mk("a", week, "2026-08-26", PlanNone), true, false},
		{"ends exactly Wednesday -> Wed", mk("aw", week, AddDays(week, 2), PlanNone), true, false},
		{"ends Thursday -> Fri", mk("at", week, AddDays(week, 3), PlanNone), false, true},
		{"ends Friday -> Fri", mk("b", week, "2026-08-28", PlanNone), false, true},
		{"a middle week of a long span -> Fri", mk("c", "2026-08-17", "2026-09-04", PlanNone), false, true},
		{"ends by Wednesday of a later covered week -> Wed there", mk("cw", "2026-08-17", "2026-08-26", PlanNone), true, false},
		{"stored band outranks the derived one", mk("d", week, "2026-08-28", PlanWed), true, false},
		{"band-less non-slot stays off the panel", Card{ItemID: "e", Title: "e", Team: "t", Week: week}, false, false},
		{"slot without an end date stays off", mk("f", week, "", PlanNone), false, false},
		{"slot of another week stays off", mk("g", "2026-09-07", "2026-09-11", PlanNone), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bands := WeeklyPlanAt(Board{Cards: []Card{tc.card}}, "t", week, today)
			if got := len(bands.Wed) == 1; got != tc.wed {
				t.Errorf("in Wed band = %v, want %v", got, tc.wed)
			}
			if got := len(bands.Fri) == 1; got != tc.fri {
				t.Errorf("in Fri band = %v, want %v", got, tc.fri)
			}
		})
	}
}

// A slot lives on the Project board until it joins a sprint: its column
// holds it, so the day grid does not. A card carrying only a project name
// has no column — the Project board renders columns by epic — so hiding it
// here would leave it on no board at all; it shows by its dates like any
// other card, and its project name is a label it happens to wear.
func TestTeamGridHidesASlotButNotACardThatOnlyNamesAProject(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{
			{ItemID: "slot", Team: "t", Epic: "E", Project: "P", StartDate: today, Day: today},
			{ItemID: "project-only", Team: "t", Project: "P", StartDate: today, Day: today},
			{ItemID: "slot-in-sprint", Team: "t", Epic: "E", Project: "P", StartDate: today, Day: today, SprintStart: today},
		},
		SprintStates: map[string]SprintState{"t": {Current: today}},
	}
	got := []string{}
	for _, c := range TeamGrid(b, "t", today) {
		got = append(got, c.ItemID)
	}
	if len(got) != 2 || got[0] != "project-only" || got[1] != "slot-in-sprint" {
		t.Fatalf("grid = %v; want the project-only card and the slot that joined a sprint", got)
	}
}

// MeView draws the same line, for the same reason.
func TestMeViewHidesASlotButNotACardThatOnlyNamesAProject(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{
			{ItemID: "slot", Team: "t", Epic: "E", Project: "P", StartDate: today, Day: today},
			{ItemID: "project-only", Team: "t", Project: "P", StartDate: today, Day: today, Assignees: []string{"kvaps"}},
			{ItemID: "owned-slot", Team: "t", Epic: "E", Project: "P", StartDate: today, Day: today, Assignees: []string{"kvaps"}},
		},
		SprintStates: map[string]SprintState{"t": {Current: today}},
	}
	got := []string{}
	for _, c := range MeView(b, "kvaps", today) {
		got = append(got, c.ItemID)
	}
	// The owned slot shows because its owner is looking at their own work;
	// the unowned slot stays on the Project board.
	if len(got) != 2 {
		t.Fatalf("MeView = %v; want the project-only card and the owned slot", got)
	}
}
