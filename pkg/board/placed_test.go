package board

import (
	"testing"
)

// docs/design/ownership.md, O13 — the team a task STANDS UNDER on the day
// boards and the backlogs is its EFFECTIVE team (O3): its own, else the
// nearest ancestor's. A task with no team of its own under an epic of portal
// is portal's work on the Team board, in the Backlog and in every team
// filter — and the epic's team is still not written down onto it. A review
// card stands under its task's; the no-team group
// is the cards whose effective team is empty.

func placedBoard() Board {
	b := NewBoard([]Card{
		{ItemID: "epic", Title: "Billing", Type: TypeEpic, Team: "portal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Parked: true},
		{ItemID: "own", Title: "Own team", Parent: "epic", Team: "ops", Parked: true},
		{ItemID: "sub", Title: "PDF", Parent: "t"},
		{ItemID: "r", Title: "review: Receipts", ReviewOf: "t", Parked: true},
		{ItemID: "plan", Title: "Docs", Type: TypeEpic},
		{ItemID: "loose", Title: "Under a teamless epic", Parent: "plan", Parked: true},
	})
	return b
}

func TestATaskStandsUnderItsEffectiveTeam(t *testing.T) {
	b := placedBoard()
	teams := PlacedTeams(b)
	for id, want := range map[string]string{
		"t":     "portal", // inherited from the epic
		"own":   "ops",    // its own wins
		"sub":   "portal", // rides its task, which inherits
		"r":     "portal", // a review card stands under its task's
		"loose": "",       // no ancestor has a team: the no-team group
		"epic":  "portal", // a plan item keeps its own
		"plan":  "",
	} {
		if got := teams[id]; got != want {
			t.Errorf("%s stands under %q, want %q", id, got, want)
		}
		c, _ := Find(b, id)
		if got := PlacedTeam(b, c); got != want {
			t.Errorf("PlacedTeam(%s) = %q, want %q", id, got, want)
		}
	}
	// Nothing is written down the tree.
	if c, _ := Find(b, "t"); c.Team != "" {
		t.Fatalf("the task's own team = %q, want none", c.Team)
	}
}

// The Team grid draws a task under its effective team's column set, and
// reads its effective team's sprint: a finished task of portal's current
// sprint stays on the sprint's days, as any portal task does.
func TestTheTeamGridDrawsATaskUnderItsEffectiveTeam(t *testing.T) {
	today := TodayIso()
	b := NewBoard([]Card{
		{ItemID: "epic", Title: "Billing", Type: TypeEpic, Team: "portal"},
		{ItemID: "t", Title: "Receipts", Parent: "epic", Assignees: []string{"ann"}, StartDate: today, Day: today, SprintStart: today},
		{ItemID: "done", Title: "Done earlier", Parent: "epic", Assignees: []string{"ann"}, SprintStart: today, Progress: 100, DoneAt: AddDays(today, -1)},
	})
	b.SprintStates = map[string]SprintState{"portal": {Current: today}}
	ids := func(team string) map[string]bool {
		out := map[string]bool{}
		for _, c := range TeamGrid(b, team, today) {
			out[c.ItemID] = true
		}
		return out
	}
	if got := ids("portal"); !got["t"] || !got["done"] {
		t.Fatalf("portal's grid = %v, want the inherited task and the sprint's finished one", got)
	}
	if got := ids(""); len(got) != 0 {
		t.Fatalf("the no-team grid = %v, want nothing of portal's", got)
	}
}

// A day is a record for a task when its EFFECTIVE team's sprint has moved
// past it.
func TestADayIsARecordByTheEffectiveTeam(t *testing.T) {
	b := placedBoard()
	c, _ := Find(b, "t")
	if !IsRecord(b, c, map[string]bool{"portal": true}) {
		t.Fatal("a day past for portal is not a record for portal's inherited task")
	}
	if IsRecord(b, c, map[string]bool{"": true}) {
		t.Fatal("a day past for the no-team group is a record for portal's task")
	}
}
