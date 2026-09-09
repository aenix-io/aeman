package board

import "testing"

// What a person can open and see. The × used to demote a worked card into the
// previous sprint, which took it off today's board and left it alive in a
// sprint no view reaches: not the day grid (the sprint is neither the current
// one nor the previous), not the Me board (the sprint gate), and no carry-over
// ever takes it, since a carry-over moves the closing sprint's own cards. The
// production board held a hundred and thirty-two such cards. Reachable is what
// names them — the migration reports and removes them, and nothing else has to
// guess the rule.
func TestReachableIsEveryCardABoardStillShows(t *testing.T) {
	const today, cur, prev = "2026-09-02", "2026-09-01", "2026-08-31"
	b := NewBoard([]Card{
		{ItemID: "st", Title: SprintStateTitle, Team: "portal", SprintStart: cur, StartDate: prev},
		{ItemID: "ep", Title: EpicStateTitle, Epic: "Auth", Project: "freedom"},

		// On the board today.
		{ItemID: "today", Team: "portal", SprintStart: cur, StartDate: cur, Day: cur},
		// On the previous sprint's day — a step back finds it, and the next
		// carry-over takes it.
		{ItemID: "yesterday", Team: "portal", SprintStart: prev, StartDate: prev, Day: prev},
		// Planned for a later day: it comes back on its own.
		{ItemID: "deferred", Team: "portal", SprintStart: cur, StartDate: "2026-09-10", Day: "2026-09-10"},
		// In a Project-board column, which is a board of its own.
		{ItemID: "slot", Team: "portal", Epic: "Auth", Project: "freedom", Week: "2026-09-07", Day: "2026-09-11"},
		// Scheduled for a week, which is a column of the Triage board.
		{ItemID: "week", Team: "portal", Week: "2026-09-07"},
		// Done, and so off the board on purpose.
		{ItemID: "done", Team: "portal", Progress: 100, SprintStart: "2026-07-06", StartDate: "2026-07-06"},

		// Open, in a sprint two behind, nothing else holding it — and the
		// Team board reaches it all the same now: it is open, it was not put
		// off and it is in no week ahead, so it is in somebody's hands. That
		// is what the day board's rule became, and it is why a card whose
		// days ran out can no longer be lost.
		{ItemID: "old", Team: "portal", Progress: 90, SprintStart: "2026-07-31",
			StartDate: "2026-07-31", Day: "2026-07-31", Assignees: []string{"kvaps"}},
		// The same, with no dates at all.
		{ItemID: "dateless", Team: "portal", Progress: 30},
		{ItemID: "kid-of-old", Team: "portal", Parent: "old"},
		{ItemID: "kid-of-today", Team: "portal", Parent: "today"},
		{ItemID: "review-of-old", Team: "portal", ReviewOf: "old"},
		{ItemID: "review-of-today", Team: "portal", ReviewOf: "today"},
	})
	got := Reachable(b, today)

	for _, id := range []string{"today", "yesterday", "deferred", "slot", "week", "done",
		"kid-of-today", "review-of-today", "old", "dateless", "kid-of-old", "review-of-old"} {
		if !got[id] {
			t.Errorf("%s is on a board someone can open, and Reachable says it is not", id)
		}
	}
	// Nothing here is on no board, and that is the point: a day board shows
	// what is open and not put off, so the shape of card this whole rule was
	// written to find — stranded by a × into a sprint nobody opens — cannot
	// be made any more.
	if lost := Unreachable(b, today); len(lost) != 0 {
		t.Errorf("nothing should be unreachable now, got %v", ids(lost))
	}
}

// A day board no longer reads a card's SPRINT to decide whether to draw it,
// so the trap this rule was written for is gone: a card could be stranded in
// a sprint nobody opens, and whether anybody found it depended on which team's
// pointer happened to name that day. («[P1] Ответить роману по ТС», sprint
// 2026-06-29, survived a cleanup because sales still pointed at that day.)
// Open work is in hand whatever sprint it sits in, on its own team's board and
// on no other.
func TestATeamsBoardHoldsItsOwnOpenWorkWhateverSprintItSitsIn(t *testing.T) {
	const today, cur, prev = "2026-09-02", "2026-09-01", "2026-08-31"
	const june = "2026-06-29"
	b := NewBoard([]Card{
		{ItemID: "st-portal", Title: SprintStateTitle, Team: "portal", SprintStart: cur, StartDate: prev},
		{ItemID: "st-sales", Title: SprintStateTitle, Team: "sales", SprintStart: "2026-07-06", StartDate: june},

		{ItemID: "old-portal", Team: "portal", Progress: 90, SprintStart: june, StartDate: june, Day: june},
		{ItemID: "old-sales", Team: "sales", Progress: 20, SprintStart: june, StartDate: june, Day: june},
	})
	got := Reachable(b, today)
	for _, id := range []string{"old-portal", "old-sales"} {
		if !got[id] {
			t.Errorf("%s is open and put off to nothing: it is in somebody's hands", id)
		}
	}
	// And each stands on its OWN team's board, not the other's.
	if ids(TeamGrid(b, "portal", today))[0] != "old-portal" {
		t.Error("portal's board holds portal's card")
	}
	if got := ids(TeamGrid(b, "sales", today)); len(got) != 1 || got[0] != "old-sales" {
		t.Errorf("sales' board = %v, want its own card alone", got)
	}
}

// A personal card belongs to its owner's board, which has no sprint and no
// team: judging it by the team rules would call every one of them stranded.
func TestAPersonalCardIsAlwaysReachable(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "mine", Domain: PersonalDomain("kvaps"), Progress: 40},
	})
	if !Reachable(b, "2026-09-02")["mine"] {
		t.Fatal("a personal card is on its owner's board, whatever the team rules say")
	}
}

// A process task is the thing turns are copied FROM: it stands on the Process
// tab, not on a day, and it is not work anyone lost.
func TestAProcessTaskIsReachable(t *testing.T) {
	b := NewBoard([]Card{
		{ItemID: "pr", Title: ProcessStateTitle, Process: "Payments"},
		{ItemID: "task", Title: ProcessTaskTitle, Process: "Payments", Team: "portal", Recurrence: "month"},
	})
	if !Reachable(b, "2026-09-02")["task"] {
		t.Fatal("a process task lives on the Process tab")
	}
}

// A PARKED card is on a board — the backlog drawer, which is the whole point
// of parking — and the day boards deliberately do not draw it. Nothing in
// Reachable said so, and that was a rounding error while the day rule lost
// cards by the dozen: the migration's cleanup had real strays to find. It is
// not a rounding error now that the day rule loses nothing, because a cleanup
// DELETES what this list hands it, and a shelf is the one thing left on it.
func TestAParkedCardIsOnTheShelfWhichIsABoard(t *testing.T) {
	const today = "2026-09-09"
	b := NewBoard([]Card{
		{ItemID: "st", Title: SprintStateTitle, Team: "portal", SprintStart: today, StartDate: today},
		{ItemID: "shelved", Team: "portal", Parked: true, Progress: 30, Assignees: []string{"kvaps"}},
		{ItemID: "shelved-list", Team: "portal", Parked: true, Progress: 0, Assignees: []string{"bob"}},
	})
	got := Reachable(b, today)
	for _, id := range []string{"shelved", "shelved-list"} {
		if !got[id] {
			t.Errorf("%s is on its team's shelf, and Reachable calls it lost", id)
		}
	}
	if lost := Unreachable(b, today); len(lost) != 0 {
		t.Errorf("nothing here is lost, got %v", ids(lost))
	}
}
