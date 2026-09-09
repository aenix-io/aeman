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
	// The Team board's rule is a function of the DAY asked about and nothing
	// else — no wall clock — so these cards can be dated plainly.
	return NewBoard([]Card{
		// In hand: open, scheduled before the day, nothing put off.
		{ItemID: "A1", Team: "A", StartDate: "2026-06-01", SprintStart: "2026-06-22"},
		// Planned into a later week: on no day board until its Monday (B1).
		{ItemID: "Aahead", Team: "A", Week: "2026-07-06"},
		// Put off to a later day — far enough ahead that it is deferred
		// whenever this test runs: gone from the board, and out of the
		// sprint in progress, until that day comes.
		{ItemID: "Adeferred", Team: "A", StartDate: "2999-07-02", SprintStart: "2026-06-22"},
		// A week gone by and still open — a debt, and still in hand.
		{ItemID: "Adebt", Team: "A", Week: "2026-06-08", StartDate: "2026-06-08"},
		// Finished on the 22nd: that day keeps it, and no other.
		{ItemID: "Adone", Team: "A", StartDate: "2026-06-01", Progress: 100, DoneAt: "2026-06-22"},
		// Finished in a sprint gone by: it belongs to the day it was finished
		// on — and to its own sprint's day, which is the whole sprint.
		{ItemID: "Alate", Team: "A", StartDate: "2026-06-01", SprintStart: "2026-06-15",
			Progress: 100, DoneAt: "2026-07-20"},
		// Nobody has said when: no week, no dates, no sprint. This is the
		// Triage strip, and a day board never draws it.
		{ItemID: "Astrip", Team: "A"},
		// Parked on a list: not planned at all, so on no day board.
		{ItemID: "Aparked", Team: "A", StartDate: "2026-06-01", Parked: true},
		// A subtask rides with its parent and is never placed on its own.
		{ItemID: "Akid", Team: "A", Parent: "A1", StartDate: "2026-06-01"},
		{ItemID: "B1", Team: "B", StartDate: "2026-06-01"},
		{ItemID: "N1", Team: "", StartDate: "2026-06-01"},
		{ItemID: "Astate", Team: "A", Title: SprintStateTitle, SprintStart: "2026-06-26", StartDate: "2026-06-22"},
	})
}

// The Team board shows what people are working on, on the day asked about:
// open work in hand, less what was put off to a later day or a later week.
// It used to answer through seven layered rules — the week's work, the
// sprint's day, the card's own day, the range between its dates, the days of
// sprints it had passed through — which put work nobody was doing on the
// board and, at the same time, dropped a card scheduled for last Tuesday and
// never finished, because no rule reached it any more.
func TestTeamGrid(t *testing.T) {
	b := gridBoard()
	cases := []struct {
		name      string
		team, day string
		want      []string
	}{
		{"in hand: open work, and the debt of a week gone by", "A", "2026-06-23",
			[]string{"A1", "Adebt"}},
		// The sprint's own day is the whole sprint: Alate belongs to the
		// 06-15 sprint and was finished a month later, and the meeting that
		// reads that day is asking exactly what became of it.
		{"a sprint's day shows its sprint, closed work included",
			"A", "2026-06-15", []string{"A1", "Adebt", "Alate"}},
		{"and the day it was finished on holds it", "A", "2026-07-20",
			[]string{"A1", "Aahead", "Adebt", "Alate"}},
		{"the day it was finished keeps it", "A", "2026-06-22",
			[]string{"A1", "Adebt", "Adone"}},
		// A day far ahead is a PLAN: it holds what somebody put on it, and
		// nothing else — today's open work is today's business.
		{"a card put off arrives on its day, and nothing else does", "A", "2999-07-02",
			[]string{"Adeferred"}},
		{"a week ahead is on no day board until its Monday", "A", "2026-07-05",
			[]string{"A1", "Adebt"}},
		{"and stands on the board from that Monday on", "A", "2026-07-06",
			[]string{"A1", "Aahead", "Adebt"}},
		{"before any of it exists, nothing is in hand", "A", "2026-05-01", []string{}},
		// The day a sprint began shows that sprint's work, whatever has
		// become of it since: A1 and Adone both carry the 06-22 sprint, and
		// Adone was finished that day in any case.
		{"a sprint's own day shows its work", "A", "2026-06-22",
			[]string{"A1", "Adebt", "Adone"}},
		{"another team is isolated", "B", "2026-06-23", []string{"B1"}},
		{"the no-team group is a team like any other", "", "2026-06-23", []string{"N1"}},
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

	// A day already past is not reconstructed from the card's dates at all:
	// the board answers it from its own history, as it stood. So the sprint a
	// card used to be in decides nothing here — which is what stopped work
	// nobody was doing that day from appearing on it.
	past := deferred
	past.SprintStart = previous
	hist := Board{
		Cards: []Card{past},
		SprintStates: map[string]SprintState{
			"alpha": {Current: current, Previous: previous},
		},
	}
	if got := TeamGrid(hist, "alpha", previous); len(got) != 0 {
		t.Fatalf("a card put off to a later day is on no earlier day either, got %+v", got)
	}
}

// A slot is on the day board once its week has come, like everything else.
//
// It used to be held back until it joined a sprint — the Project board's
// column was said to hold it meanwhile — but the Team board is now the same
// set the Triage board shows for the week, and a slot whose week has arrived
// is in that set. A card carrying only a PROJECT name is not a slot at all
// (the Project board renders columns by epic, so it has no column) and shows
// like any other card.
func TestTeamGridShowsASlotOnceItsWeekHasCome(t *testing.T) {
	today := TodayIso()
	b := NewBoardIn("acme", []Card{
		{ItemID: "slot", Team: "t", Epic: "E", Project: "P", StartDate: today, Day: today},
		{ItemID: "project-only", Team: "t", Project: "P", StartDate: today, Day: today},
		{ItemID: "later", Team: "t", Epic: "E", Project: "P",
			StartDate: AddDays(MondayOf(today), 21), Day: AddDays(MondayOf(today), 25)},
	})
	got := ids(TeamGrid(b, "t", today))
	if !reflect.DeepEqual(got, []string{"slot", "project-only"}) {
		t.Fatalf("grid = %v; want this week's slot and the project-only card, and not the slot three weeks out", got)
	}
}

// A slot stands on the day grid from the week it starts in, and stays there
// while it is open — past the end of its span it is a debt, which is the one
// thing nobody should be able to lose sight of. Before its week it is the
// Project board's business alone (B1), and after TODAY it is a plan again:
// a day still to come holds what was put there, not what is still owed.
func TestTeamGridShowsASlotFromItsWeekUntilItIsDone(t *testing.T) {
	today := TodayIso()
	week := MondayOf(today)
	b := NewBoardIn("acme", []Card{
		{ItemID: "slot", Team: "t", Epic: "E", Project: "P",
			StartDate: AddDays(week, -14), Day: AddDays(today, -1)},
	})
	b.SprintStates = map[string]SprintState{"t": {Current: AddDays(week, -14)}}
	// Inside its span, and past the end of it: still open, still in hand.
	for _, day := range []string{AddDays(week, -14), AddDays(week, -7), AddDays(today, -1), today} {
		if got := TeamGrid(b, "t", day); len(got) != 1 {
			t.Fatalf("TeamGrid(%s) = %d card(s); an open slot whose week has come is in hand", day, len(got))
		}
	}
	// Before its week it is not.
	if got := TeamGrid(b, "t", AddDays(week, -21)); len(got) != 0 {
		t.Fatalf("TeamGrid(%s) = %d card(s); a week ahead is on no day board", AddDays(week, -21), len(got))
	}
	// And a day still to come is a plan: the debt is today's business, not
	// tomorrow's — nothing reaches that day any more.
	if got := TeamGrid(b, "t", AddDays(today, 1)); len(got) != 0 {
		t.Fatalf("TeamGrid(tomorrow) = %d card(s); a debt stands on today, not on a day nobody planned it for", len(got))
	}
	// Finished, it belongs to the day it was finished on.
	b.Cards[0].Progress = 100
	b.Cards[0].DoneAt = AddDays(today, -1)
	if got := TeamGrid(b, "t", AddDays(today, -1)); len(got) != 1 {
		t.Fatal("the day it was finished keeps it")
	}
	if got := TeamGrid(b, "t", "2026-09-17"); len(got) != 0 {
		t.Fatal("and the next day does not")
	}
}

// A DEFERRED card leaves the day grid at once — that is what deferring is —
// and its week must not hold it there. Defer moves the dates and leaves the
// week where it was, so the card the person pushed a month out was still
// standing in this week's grid, on a board they had taken it off.
func TestTeamGridLetsADeferredCardGo(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{{
			ItemID: "def", Team: "t", Assignees: []string{"bob"},
			Week: MondayOf(today), StartDate: AddDays(today, 30), Day: AddDays(today, 30),
			SprintStart: today,
		}},
		SprintStates: map[string]SprintState{"t": {Current: today}},
	}
	if got := TeamGrid(b, "t", today); len(got) != 0 {
		t.Fatalf("grid = %d card(s); a card deferred a month out is gone from today", len(got))
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

// The week's own work stands on the day grid all week — its person's column,
// or Unassigned when nobody has taken it. This is the set the Triage board
// shows for that week: what the weekly panel used to hold beside the grid,
// and the reason a card placed in a week is not invisible until someone
// gives it a day.
func TestTeamGridCarriesTheWeeksOwnWork(t *testing.T) {
	today := TodayIso()
	week := MondayOf(today)
	b := NewBoard([]Card{
		// Placed in this week and nothing else: no dates, no sprint.
		{ItemID: "placed", Team: "t", Week: week},
		// A slot covering this week, on nobody's day.
		{ItemID: "slot", Team: "t", Epic: "E", Project: "P",
			StartDate: AddDays(week, -7), Day: AddDays(week, 4)},
		// A process turn filed into this week.
		{ItemID: "turn", Team: "t", Task: "task", Week: week, Stage: StageRecurrent},
		// A debt: owed last week, still open — it stands beside this week's
		// work without leaving the week it was owed in.
		{ItemID: "debt", Team: "t", Week: AddDays(week, -7)},
		// Placed in a week to come: on no day board until its Monday (B1).
		{ItemID: "ahead", Team: "t", Week: AddDays(week, 7)},
		// Another team's week is not this grid's business.
		{ItemID: "elsewhere", Team: "other", Week: week},
	})
	b.SprintStates = map[string]SprintState{"t": {Current: today}}

	got := ids(TeamGrid(b, "t", today))
	want := []string{"placed", "slot", "turn", "debt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grid = %v, want %v", got, want)
	}

	// Looking a week ahead: what is PLANNED for then, which is the card
	// placed in that week and nothing else. This week's work — the debt
	// included — is this week's business; a day still to come holds what
	// somebody put there, or "tomorrow" means the whole backlog.
	next := AddDays(today, 7)
	if got := ids(TeamGrid(b, "t", next)); !reflect.DeepEqual(got, []string{"ahead"}) {
		t.Fatalf("a week ahead = %v, want the card placed in that week alone", got)
	}
	// Finished, the debt belongs to the day it was finished on and no other.
	for i := range b.Cards {
		if b.Cards[i].ItemID == "debt" {
			b.Cards[i].Progress, b.Cards[i].DoneAt = 100, today
		}
	}
	if got := ids(TeamGrid(b, "t", AddDays(today, -1))); slicesContains(got, "debt") {
		t.Fatalf("work finished today is not in hand yesterday: %v", got)
	}
}

func slicesContains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// Finished work belongs to the day it RECORDS being finished on (doneAt),
// and to no other day at all. That is what keeps yesterday's finished work
// off today's board, and it is the whole rule — there is no second one.
//
// It used to guess when the card said nothing: doneAt, else the card's end
// date, else its start. Both fallbacks read a PLAN as if it were evidence,
// and a card stretched three weeks ahead and closed today stood on a day
// three weeks out, as though it had been finished then, and on none of the
// days it was actually worked.
//
// Nothing is lost by refusing to guess, because a card whose writer said
// nothing is not left to this rule: a day already gone is served as a
// snapshot of the tree at that day's last commit, and the day's own commits
// name the cards they touched — so the history says which of them were
// finished on it and fills the field in (gitstore.markFinishedInDay). What
// reaches here unrecorded was closed on no day this board can name, and
// there is no day AHEAD on which somebody finished it.
func TestAFinishedCardBelongsToTheDayItRecords(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "recorded", Team: "t", StartDate: "2026-09-01", Day: "2026-09-20",
			Progress: 100, DoneAt: "2026-09-03"},
		{ItemID: "unrecorded", Team: "t", StartDate: "2026-09-01", Day: "2026-09-20", Progress: 100},
		{ItemID: "one-day", Team: "t", StartDate: "2026-09-02", Progress: 100},
	})
	shows := func(id, day string) bool {
		for _, c := range TeamGrid(b, "t", day) {
			if c.ItemID == id {
				return true
			}
		}
		return false
	}
	// Recorded: its own day, and nothing else — not the days of its span.
	if !shows("recorded", "2026-09-03") {
		t.Error("the day it recorded must hold it")
	}
	for _, day := range []string{"2026-09-01", "2026-09-10", "2026-09-20"} {
		if shows("recorded", day) {
			t.Errorf("a card that says when it was finished stands on no other day (%s)", day)
		}
	}
	// Unrecorded: on no day. Never invented onto one at the far end of a plan.
	for _, day := range []string{"2026-09-01", "2026-09-03", "2026-09-10", "2026-09-20"} {
		if shows("unrecorded", day) {
			t.Errorf("a card that says nothing is put on no day by guesswork (%s)", day)
		}
	}
	if shows("one-day", "2026-09-02") || shows("one-day", "2026-09-03") {
		t.Error("one date is a plan, not a record of being finished")
	}
	// And open work does not stop at the day it was due to end: work that ran
	// over is the work most in need of being looked at, and a board that
	// dropped it was how a card scheduled for last Tuesday left every view at
	// once. Said of TODAY — a day still to come is a plan (plannedFor), so
	// this card is dated against the real clock rather than the fixed days
	// above.
	today := TodayIso()
	b = NewBoard([]Card{{ItemID: "ranover", Team: "t",
		StartDate: AddDays(today, -10), Day: AddDays(today, -2)}})
	for _, day := range []string{AddDays(today, -10), AddDays(today, -2), today} {
		if !shows("ranover", day) {
			t.Errorf("open work stands from its start on (%s)", day)
		}
	}
	if shows("ranover", AddDays(today, -11)) {
		t.Error("but not before the day it was put off to")
	}
}

// Looking FORWARD, the Team board answers a different question. TODAY is
// "what is in hand": open work stands there whatever its plan said, because
// work that ran over is the work most in need of being looked at. A day still
// to COME is a plan, not a state — it holds what somebody has actually put
// there, and nothing else.
//
// Without that bound the day board had no forgetting at all: every open card
// of a team stood on every future day, so "tomorrow" stopped meaning tomorrow
// and a month out was simply the whole backlog (122 of a team's 122 open
// cards on one production board).
func TestATomorrowIsAPlanAndTodayIsAState(t *testing.T) {
	today := TodayIso()
	tomorrow := AddDays(today, 1)
	nextWeek := AddDays(MondayOf(today), 7)
	lastWeek := AddDays(MondayOf(today), -7)

	b := NewBoard([]Card{
		// Open, planned for a day gone by: in hand TODAY, and on no day ahead.
		{ItemID: "ranover", Team: "T", StartDate: AddDays(today, -3), Day: AddDays(today, -3)},
		// A debt of a week gone by, with no dates: the same answer.
		{ItemID: "debt", Team: "T", Week: lastWeek},
		// Its span reaches tomorrow: planned for it.
		{ItemID: "spans", Team: "T", StartDate: AddDays(today, -1), Day: tomorrow},
		// Planned for the day itself.
		{ItemID: "onthatday", Team: "T", StartDate: tomorrow, Day: tomorrow},
		// Placed in next week: on next week's days, and not before.
		{ItemID: "nextweek", Team: "T", Week: nextWeek},
		// This week's work with no dates of its own: this week's days hold it.
		{ItemID: "thisweek", Team: "T", Week: MondayOf(today)},
	})
	on := func(day string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b, "T", day) {
			out[c.ItemID] = true
		}
		return out
	}

	now := on(today)
	for _, id := range []string{"ranover", "debt", "spans", "thisweek"} {
		if !now[id] {
			t.Errorf("today is what is in hand, and %s is: %v", id, now)
		}
	}
	if now["onthatday"] || now["nextweek"] {
		t.Errorf("work planned for a later day is not in hand today: %v", now)
	}

	next := on(tomorrow)
	for _, id := range []string{"spans", "onthatday"} {
		if !next[id] {
			t.Errorf("%s is planned for tomorrow and belongs there: %v", id, next)
		}
	}
	for _, id := range []string{"ranover", "debt"} {
		if next[id] {
			t.Errorf("%s is today's problem, not tomorrow's plan: %v", id, next)
		}
	}

	// A week ahead arrives on its own Monday and stands through its days.
	if on(nextWeek)["nextweek"] == false {
		t.Error("a card placed in next week stands on the days of that week")
	}
	if on(AddDays(nextWeek, 2))["nextweek"] == false {
		t.Error("including its Wednesday")
	}
	if on(AddDays(nextWeek, 7))["nextweek"] {
		t.Error("and not on the week after it")
	}
	// This week's card does not reach into next week either.
	if on(nextWeek)["thisweek"] {
		t.Error("a card placed in THIS week is not planned for the next one")
	}
}

// THE SPRINT'S OWN DAY IS THE WHOLE SPRINT. Every few mornings a lead opens
// the day the sprint began — the "current sprint" jump lands there — and goes
// through it with the team. That view has to hold everything the sprint has
// been: the work it opened with, the work typed into it on its second and
// third days, and the work already CLOSED. A day that showed only what is
// still open answered "what is left", which is not the question the meeting
// asks.
//
// Two things still leave that day, and they are the same two everywhere: work
// somebody put off past today, and work planned into a week still to come.
// Deferring is the act of taking a card out of the sprint in progress — send
// it to tomorrow and it goes from today's sprint at once and arrives
// tomorrow; send it three days out and the sprint that opens tomorrow starts
// without it.
func TestTheSprintsOwnDayIsTheWholeSprint(t *testing.T) {
	today := TodayIso()
	opened := AddDays(today, -2) // the sprint began the day before yesterday
	b := NewBoard([]Card{
		{ItemID: "fromtheoff", Team: "T", SprintStart: opened, StartDate: opened},
		// Typed into the sprint on its second day.
		{ItemID: "midsprint", Team: "T", SprintStart: opened, StartDate: AddDays(today, -1)},
		// Closed yesterday, inside the sprint: the meeting is about this too.
		{ItemID: "closed", Team: "T", SprintStart: opened, StartDate: opened,
			Progress: 100, DoneAt: AddDays(today, -1)},
		// Put off to tomorrow: out of the sprint from the moment it was.
		{ItemID: "tomorrow", Team: "T", SprintStart: opened, StartDate: AddDays(today, 1), Day: AddDays(today, 1)},
		// Put off three days: the sprint that opens tomorrow starts without it.
		{ItemID: "later", Team: "T", SprintStart: opened, StartDate: AddDays(today, 3), Day: AddDays(today, 3)},
		// Planned into a week to come: the same answer, by the other door.
		{ItemID: "nextweek", Team: "T", SprintStart: opened, Week: AddDays(MondayOf(today), 7)},
		// Put off to today — yesterday's "send it to tomorrow", the morning
		// after. Not deferred any more, so the sprint has it back.
		{ItemID: "arrived", Team: "T", SprintStart: opened, StartDate: today, Day: today},
	})
	on := func(day string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b, "T", day) {
			out[c.ItemID] = true
		}
		return out
	}

	sprint := on(opened)
	for _, id := range []string{"fromtheoff", "midsprint", "closed"} {
		if !sprint[id] {
			t.Errorf("the sprint's day is the whole sprint, and %s is part of it: %v", id, sprint)
		}
	}
	for _, id := range []string{"tomorrow", "later", "nextweek"} {
		if sprint[id] {
			t.Errorf("%s was put off and is not this sprint's work any more: %v", id, sprint)
		}
	}

	// A card put off to TODAY is not put off any more, and the sprint's day
	// has it back. That is what the lead sees the next morning: send a card
	// to tomorrow and it leaves the sprint for the rest of the day, and when
	// tomorrow comes it is in the sprint again — the sprint is still running,
	// and the card is in play again. Sending it further out is how it stays
	// away for longer.
	if !on(opened)["arrived"] {
		t.Error("a card whose day has come is back in the sprint")
	}

	// The card sent to tomorrow arrives tomorrow, and the one sent three days
	// out waits for its own day — so a sprint opened tomorrow starts without it.
	if !on(AddDays(today, 1))["tomorrow"] {
		t.Error("a card sent to tomorrow shows tomorrow")
	}
	if on(AddDays(today, 1))["later"] {
		t.Error("and the one sent further out does not")
	}
	if !on(AddDays(today, 3))["later"] {
		t.Error("it arrives on the day it was sent to")
	}
	// And TODAY is still what is in hand: the closed card belongs to the day
	// it was closed on, not to every day after it.
	if on(today)["closed"] {
		t.Error("finished work does not follow the team around; it stays on its day")
	}
}
