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
		// In the weekly plan.
		{ItemID: "plan", Team: "portal", Plan: PlanFri, Week: "2026-09-07"},
		// Done, and so off the board on purpose.
		{ItemID: "done", Team: "portal", Progress: 100, SprintStart: "2026-07-06", StartDate: "2026-07-06"},

		// Stranded: open, in a sprint two behind, nothing else holding it.
		{ItemID: "stray", Team: "portal", Progress: 90, SprintStart: "2026-07-31",
			StartDate: "2026-07-31", Day: "2026-07-31", Assignees: []string{"kvaps"}},
		// Stranded with no dates at all — nothing places it anywhere.
		{ItemID: "dateless", Team: "portal", Progress: 30},
		// A subtask of a stranded card is stranded with it; one riding a card
		// that IS on a board is reachable through it.
		{ItemID: "kid-of-stray", Team: "portal", Parent: "stray"},
		{ItemID: "kid-of-today", Team: "portal", Parent: "today"},
		// A review card follows its original the same way.
		{ItemID: "review-of-stray", Team: "portal", ReviewOf: "stray"},
		{ItemID: "review-of-today", Team: "portal", ReviewOf: "today"},
	})
	got := Reachable(b, today)

	for _, id := range []string{"today", "yesterday", "deferred", "slot", "plan", "done",
		"kid-of-today", "review-of-today"} {
		if !got[id] {
			t.Errorf("%s is on a board someone can open, and Reachable says it is not", id)
		}
	}
	for _, id := range []string{"stray", "dateless", "kid-of-stray", "review-of-stray"} {
		if got[id] {
			t.Errorf("%s is on no board at all, and Reachable counts it as reachable", id)
		}
	}
}

// A day is opened FOR A TEAM. One team's sprint pointer left behind in June
// does not make another team's card of that day reachable: nobody opens the
// portal board on the day the sales pointer happens to name. Reading every
// team's days as one set spared exactly the cards this rule exists to find —
// «[P1] Ответить роману по ТС», sprint 2026-06-29, survived a cleanup because
// sales still pointed at that day.
func TestAStaleTeamsDayDoesNotSaveAnotherTeamsCard(t *testing.T) {
	const today, cur, prev = "2026-09-02", "2026-09-01", "2026-08-31"
	const june = "2026-06-29"
	b := NewBoard([]Card{
		{ItemID: "st-portal", Title: SprintStateTitle, Team: "portal", SprintStart: cur, StartDate: prev},
		// A team nobody has carried over since June.
		{ItemID: "st-sales", Title: SprintStateTitle, Team: "sales", SprintStart: "2026-07-06", StartDate: june},

		// The portal card of that June day: on no board anyone opens.
		{ItemID: "stray", Team: "portal", Progress: 90, SprintStart: june, StartDate: june, Day: june},
		// The sales card of the same day IS reachable: that is its own team's
		// previous sprint, and the sales board still shows it.
		{ItemID: "theirs", Team: "sales", Progress: 20, SprintStart: june, StartDate: june, Day: june},
	})
	got := Reachable(b, today)
	if got["stray"] {
		t.Error("a portal card of a June day is on no portal board — another team's pointer is not a day anyone opens for it")
	}
	if !got["theirs"] {
		t.Error("the sales card stands on its own team's previous sprint day")
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
