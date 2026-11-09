package board

import (
	"reflect"
	"slices"
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
		// Put off to a later day — far enough ahead that it is deferred
		// whenever this test runs: gone from the board, and out of the
		// sprint in progress, until that day comes.
		{ItemID: "Adeferred", Team: "A", StartDate: "2999-07-02", SprintStart: "2026-06-22"},
		// A day gone by and still open — a debt, and still in hand.
		{ItemID: "Adebt", Team: "A", StartDate: "2026-06-08"},
		// Finished on the 22nd: that day keeps it, and no other.
		{ItemID: "Adone", Team: "A", StartDate: "2026-06-01", Progress: 100, DoneAt: "2026-06-22"},
		// Finished in a sprint gone by: it belongs to the day it was finished
		// on — and to its own sprint's day, which is the whole sprint.
		{ItemID: "Alate", Team: "A", StartDate: "2026-06-01", SprintStart: "2026-06-15",
			Progress: 100, DoneAt: "2026-07-20"},
		// Nobody has said when: no dates, no sprint. A task like that is
		// parked in its team's backlog by every door (K1, K3), and a day
		// board never draws it.
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
		// Finished a month after its sprint was carried past: that work is
		// the 06-15 sprint's, and the day it was closed on — in a later
		// sprint — does not take it (D21).
		{"a card closed after its sprint was carried past is not on the day it was closed",
			"A", "2026-07-20", []string{"A1", "Adebt"}},
		{"the day it was finished keeps it", "A", "2026-06-22",
			[]string{"A1", "Adebt", "Adone"}},
		// A day far ahead is a PLAN: it holds what somebody put on it, and
		// nothing else — today's open work is today's business.
		{"a card put off arrives on its day, and nothing else does", "A", "2999-07-02",
			[]string{"Adeferred"}},
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

// THE ME BOARD IS THE TEAM BOARD FOR ONE PERSON (D6). Whatever day is asked,
// a person's board holds exactly the cards of theirs the Team board draws in
// the column of the team each stands under — the same debt, the same closed
// work, the same plan for a day ahead — and nobody else's. It had a rule of
// its own and the two drifted: a card closed on a day of a later sprint stood
// on the Team board that day and on no day of its owner's board.
func TestMeViewIsTheTeamBoardForOnePerson(t *testing.T) {
	for name, b := range map[string]Board{"me": meBoard(), "grid": gridBoard()} {
		ix := NewIndex(b)
		teams := map[string]bool{}
		for _, c := range b.Cards {
			teams[ix.PlacedTeam(c)] = true
		}
		for day := "2026-05-30"; day <= "2026-07-25"; day = AddDays(day, 1) {
			for _, user := range []string{"u", "v", ""} {
				want := []string{}
				onGrid := map[string]bool{}
				for team := range teams {
					for _, c := range TeamGrid(b, team, day) {
						onGrid[c.ItemID] = true
					}
				}
				for _, c := range b.Cards {
					if onGrid[c.ItemID] && (user == "" || slices.Contains(c.Assignees, user)) {
						want = append(want, c.ItemID)
					}
				}
				if got := ids(MeView(b, user, day)); !reflect.DeepEqual(got, want) {
					t.Errorf("%s: MeView(%q,%s) = %v, Team board's = %v", name, user, day, got, want)
				}
			}
		}
	}
}

// The person is the filter: their cards, and a parent whose subtask is theirs
// (the subtask rides it); user "" is everyone.
func TestMeViewPicksThePersonsCards(t *testing.T) {
	b := meBoard()
	day := "2026-06-26"
	if got := ids(MeView(b, "v", day)); !reflect.DeepEqual(got, []string{"u4"}) {
		t.Errorf("v's board = %v, want [u4]", got)
	}
	b.Cards = append(b.Cards, Card{ItemID: "kid", Parent: "u4", Assignees: []string{"w"}, Team: "A",
		StartDate: day, SprintStart: day})
	b = NewBoard(b.Cards)
	if got := ids(MeView(b, "w", day)); !reflect.DeepEqual(got, []string{"u4"}) {
		t.Errorf("w owns only a subtask of u4: board = %v, want the parent [u4]", got)
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

// A DEFERRED card leaves the day grid at once — that is what deferring is —
// and its sprint must not hold it there.
func TestTeamGridLetsADeferredCardGo(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{{
			ItemID: "def", Team: "t", Assignees: []string{"bob"},
			StartDate: AddDays(today, 30), Day: AddDays(today, 30),
			SprintStart: today,
		}},
		SprintStates: map[string]SprintState{"t": {Current: today}},
	}
	if got := TeamGrid(b, "t", today); len(got) != 0 {
		t.Fatalf("grid = %d card(s); a card deferred a month out is gone from today", len(got))
	}
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

	b := NewBoard([]Card{
		// Open, planned for a day gone by: in hand TODAY, and on no day ahead.
		{ItemID: "ranover", Team: "T", StartDate: AddDays(today, -3), Day: AddDays(today, -3)},
		// In the sprint, with no dates of its own: in hand today.
		{ItemID: "debt", Team: "T", SprintStart: AddDays(today, -7)},
		// Its span reaches tomorrow: planned for it.
		{ItemID: "spans", Team: "T", StartDate: AddDays(today, -1), Day: tomorrow},
		// Planned for the day itself.
		{ItemID: "onthatday", Team: "T", StartDate: tomorrow, Day: tomorrow},
	})
	on := func(day string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b, "T", day) {
			out[c.ItemID] = true
		}
		return out
	}

	now := on(today)
	for _, id := range []string{"ranover", "debt", "spans"} {
		if !now[id] {
			t.Errorf("today is what is in hand, and %s is: %v", id, now)
		}
	}
	if now["onthatday"] {
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
		// Put off to today — yesterday's "send it to tomorrow", the morning
		// after. Not deferred any more, so the sprint has it back.
		{ItemID: "arrived", Team: "T", SprintStart: opened, StartDate: today, Day: today},
	})
	b.SprintStates = map[string]SprintState{"T": {Current: opened}}
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
	for _, id := range []string{"tomorrow", "later"} {
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
	// And TODAY shows the current sprint's closed work too: the board is the
	// sprint so far, and Carry Over is what later clears it onto the previous
	// sprint.
	if !on(today)["closed"] {
		t.Error("the current sprint's closed work shows on today too")
	}
}

// THE CURRENT SPRINT KEEPS THE WORK IT FINISHED, ON EVERY DAY UP TO TODAY. A
// lead agreed a card into this sprint, somebody did it, and the sprint goes on
// holding it, done — on the day it began and on today alike, so the Team board
// reads as "what has this sprint been" rather than "what is left". Carry Over
// is the reset: a finished card is left on the closing sprint, so once a new
// sprint opens it is a PREVIOUS sprint's work and drops off today — a previous
// sprint answers for its closed work only on its own day (a record).
func TestASprintsOwnDayKeepsTheWorkItFinished(t *testing.T) {
	today := TodayIso()
	opened := AddDays(today, -2) // the sprint began the day before yesterday
	before := AddDays(today, -5) // and one before it, now closed
	b := NewBoard([]Card{
		{ItemID: "st", Title: SprintStateTitle, Team: "T", SprintStart: opened, StartDate: before},
		// This sprint's work, closed on its second day: overdue when it was
		// taken, which is exactly the card a lead asks about.
		{ItemID: "closed", Team: "T", SprintStart: opened,
			StartDate: AddDays(today, -9), Day: AddDays(today, -9),
			Progress: 100, DoneAt: AddDays(today, -1)},
		// Still going, same sprint.
		{ItemID: "open", Team: "T", SprintStart: opened, StartDate: opened},
		// Closed in the sprint BEFORE this one: its own day keeps it.
		{ItemID: "older", Team: "T", SprintStart: before, StartDate: before,
			Progress: 100, DoneAt: AddDays(today, -4)},
	})
	b.SprintStates = map[string]SprintState{"T": {Current: opened, Previous: before, ItemID: "st"}}
	on := func(day string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b, "T", day) {
			out[c.ItemID] = true
		}
		return out
	}

	// The sprint's own day holds its work, finished included.
	sprint := on(opened)
	if !sprint["closed"] {
		t.Errorf("the sprint's day keeps what the sprint finished: %v", sprint)
	}
	if !sprint["open"] {
		t.Errorf("and what it is still doing: %v", sprint)
	}
	// Today shows the current sprint's work, closed included: the board reads
	// as "the sprint so far", not "what is left".
	if !on(today)["closed"] {
		t.Error("the current sprint's finished work shows on today too")
	}
	// It is still reachable on the day it was finished, as every finished card is.
	if !on(AddDays(today, -1))["closed"] {
		t.Error("the day it was finished on keeps it")
	}
	// Each sprint's day answers for its own sprint, and only that.
	if !on(before)["older"] {
		t.Error("a closed sprint's day is the record of that sprint")
	}
	if on(opened)["older"] {
		t.Error("and it is not adopted by the sprint that followed")
	}
}

// The Me board reads no week (K7, D17 retired; a card has no week since
// project cards went): a task is drawn by its dates and sprint, and one with
// nothing else is on no day board at all. A parked card is not the person's
// day either, whoever owns it.
func TestTheMeBoardReadsNoWeek(t *testing.T) {
	t.Parallel()
	today := TodayIso()
	thisWeek := MondayOf(today)
	b := NewBoard([]Card{
		{ItemID: "st", Title: SprintStateTitle, Team: "alpha", SprintStart: thisWeek, StartDate: thisWeek},
		{ItemID: "week-only", Team: "alpha", Assignees: []string{"u"}},
		{ItemID: "dated", Team: "alpha", Assignees: []string{"u"},
			StartDate: today, Day: today, SprintStart: thisWeek},
		{ItemID: "shelved", Team: "alpha", Assignees: []string{"u"}, Parked: true},
	})
	got := ids(MeView(b, "u", today))
	if !reflect.DeepEqual(got, []string{"dated"}) {
		t.Fatalf("MeView = %v, want the dated card alone", got)
	}
}

// The Me board is the one that keeps everything of the person's that is not
// closed, whichever day they look at (docs/dates.md). An OPEN card whose sprint
// fell behind the team's current one — left out of a carry-over, so its sprint
// never advanced and its end date is already past — used to vanish from it: the
// range gate stopped at the end date, and the sprint-span gate wanted the
// ACTIVE sprint, which this card's stale sprint is older than. It belongs on
// the owner's LIVE board all the same, the way the Team board keeps overran
// work. Observed in production: an open card two carry-overs behind showed in
// Team but not in Me. Still bounded: a done card carried past stays on its own
// sprint, and a day still to come is a plan, not a dumping ground for open
// work.
func TestMeBoardKeepsStrandedOpenWork(t *testing.T) {
	today := TodayIso()
	stale := AddDays(today, -12) // its sprint, two carry-overs behind
	previous := AddDays(today, -3)
	b := Board{
		Cards: []Card{
			// Open, assigned, sprinted — but the sprint is stale and the end
			// date is past. The card the bug hid.
			{ItemID: "stranded", Team: "alpha", Assignees: []string{"u"},
				SprintStart: stale, StartDate: AddDays(today, -14), Day: AddDays(today, -10),
				Progress: 90},
			// Finished in that same stale sprint: carried past, so it belongs to
			// its own sprint's days, not to today.
			{ItemID: "stranded-done", Team: "alpha", Assignees: []string{"u"},
				SprintStart: stale, StartDate: AddDays(today, -14), Day: AddDays(today, -10),
				Progress: 100, DoneAt: AddDays(today, -11)},
			// An old sprint-LESS card, still open.
			{ItemID: "stray", Team: "alpha", Assignees: []string{"u"},
				StartDate: AddDays(today, -14), Day: AddDays(today, -10)},
		},
		SprintStates: map[string]SprintState{"alpha": {Current: today, Previous: previous}},
	}
	on := func(day string) map[string]bool {
		got := map[string]bool{}
		for _, c := range MeView(b, "u", day) {
			got[c.ItemID] = true
		}
		return got
	}

	now := on(today)
	if !now["stranded"] {
		t.Errorf("an open card of the person's stays on their board today even when its sprint fell behind: %v", now)
	}
	if now["stranded-done"] {
		t.Errorf("a finished card carried past belongs to its own sprint, not today: %v", now)
	}
	// An old sprint-less card that is still open is debt, on the owner's board
	// as on the Team board's column (D6): it used to be kept off today here
	// alone.
	if !now["stray"] {
		t.Errorf("an open sprint-less card is debt on today, as on the Team board: %v", now)
	}

	// A day still to COME is a plan: the stranded card is not dumped onto it.
	if on(AddDays(today, 1))["stranded"] {
		t.Error("the stranded open card is today's work, not a future day's plan")
	}
}

// Two things watched at a real standup. (1) EVERY day of the current sprint
// accumulates its work, closed included — a lead opening a middle day of a
// running sprint sees what the sprint has finished, not only what it opened
// that day with. (2) Carry Over leaves finished work on the closing sprint: a
// card done TODAY but in a sprint the team has just carried past does not cling
// to today; it belongs to that closed sprint's own day.
func TestCurrentSprintAccumulatesDoneAndCarryOverLeavesItBehind(t *testing.T) {
	today := TodayIso()
	opened := AddDays(today, -3)
	mid := AddDays(today, -1) // a day of the sprint that is neither its start nor today

	b := NewBoard([]Card{
		// Done on the sprint's FIRST day: a middle day is neither its doneAt
		// nor today, so only the whole-sprint rule can put it there.
		{ItemID: "doneEarly", Team: "T", SprintStart: opened, StartDate: opened, Progress: 100, DoneAt: opened},
		{ItemID: "open", Team: "T", SprintStart: opened, StartDate: opened},
	})
	b.SprintStates = map[string]SprintState{"T": {Current: opened}}
	on := func(day string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b, "T", day) {
			out[c.ItemID] = true
		}
		return out
	}
	for _, day := range []string{opened, mid, today} {
		if !on(day)["doneEarly"] {
			t.Errorf("the current sprint's finished work must show on %s (every day of the sprint)", day)
		}
	}

	b2 := NewBoard([]Card{
		{ItemID: "doneToday", Team: "T", SprintStart: opened, StartDate: opened, Progress: 100, DoneAt: today},
		{ItemID: "carried", Team: "T", SprintStart: today, StartDate: opened},
	})
	b2.SprintStates = map[string]SprintState{"T": {Current: today, Previous: opened}}
	on2 := func(day string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b2, "T", day) {
			out[c.ItemID] = true
		}
		return out
	}
	if on2(today)["doneToday"] {
		t.Error("Carry Over leaves finished work behind; a done card of the closed sprint must not cling to today even if finished today")
	}
	if !on2(today)["carried"] {
		t.Error("the carried-over open card belongs to today")
	}
	if !on2(opened)["doneToday"] {
		t.Error("the closed sprint's own day keeps the work it finished")
	}
}

// The leftovers a planning meeting closes BEFORE pressing Carry over belong
// to the sprint they were worked in, not to the one the meeting opens. The
// day of the meeting, read back later as a record (the sprint pointers as
// they stood that evening, the new sprint current), does not hold them,
// though it is the day they were closed on; the old sprint's days do. The
// same holds on the Me board, which is the Team board's rule for one person
// (D21).
func TestLeftoversClosedBeforeTheCarryOverStayInTheirSprint(t *testing.T) {
	old, meeting := "2026-11-02", "2026-11-06"
	b := NewBoard([]Card{
		{ItemID: "leftover", Team: "T", Assignees: []string{"u"}, SprintStart: old, StartDate: old,
			Day: old, Progress: 100, DoneAt: meeting},
		{ItemID: "carried", Team: "T", Assignees: []string{"u"}, SprintStart: meeting, StartDate: old},
		// A card with no sprint has no sprint to belong to: it stands on the
		// day it was closed, as every dated card does.
		{ItemID: "dated", Team: "T", Assignees: []string{"u"}, StartDate: old, Day: old,
			Progress: 100, DoneAt: meeting},
	})
	b.SprintStates = map[string]SprintState{"T": {Current: meeting, Previous: old}}
	team := ids(TeamGrid(b, "T", meeting))
	me := ids(MeView(b, "u", meeting))
	for name, got := range map[string][]string{"team": team, "me": me} {
		if !reflect.DeepEqual(got, []string{"carried", "dated"}) {
			t.Errorf("%s on the meeting day = %v, want [carried dated]", name, got)
		}
	}
}
