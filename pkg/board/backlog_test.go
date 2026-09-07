package board

import (
	"reflect"
	"testing"
)

// A card has three states, not two.
//
// It used to be either scheduled for a week — and drawn on the boards — or in
// neither, which put it in the Triage strip: "nobody has said when". A third
// now stands between them: a card put DELIBERATELY on a shelf, where it waits
// without pretending to be planned. The strip keeps its meaning as the INBOX —
// work that arrived and has not been looked at — and a backlog is where
// something goes once somebody has looked and decided "not now".
//
// The two are exclusive: a card is on a shelf or in a week, never both, or the
// board would show the same work in two places and count it twice.
//
// Every team HAS a backlog. It is not declared anywhere and cannot be made,
// renamed or removed: a team that plans has work it is not planning, so the
// shelf is a fact about the team rather than a thing somebody sets up. Extra
// lists can be made beside it — "some day", "blocked on legal" — and those
// belong to the BOARD, not to any one team: what they hold is the kind of
// decision that outlives whose team the work is currently in.

func TestABacklogCardIsNotInTheStrip(t *testing.T) {
	b := Board{}
	inbox := Card{ItemID: "in", Team: "alpha"}
	shelved := Card{ItemID: "later", Team: "alpha", Parked: true}

	if !NeedsTriage(b, inbox, TodayIso()) {
		t.Fatal("a card nobody has placed is in the strip: that is what the strip is")
	}
	if NeedsTriage(b, shelved, TodayIso()) {
		t.Fatal("a card on its team's shelf has been looked at; the strip is the inbox")
	}
}

// The backlog is not a plan, so it draws on no day board. This is the same
// answer a week AHEAD gets (B1), and for the same reason: nobody is doing this
// work today.
func TestABacklogCardIsOnNoDayBoard(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{{ItemID: "listed", Team: "alpha", Parked: true,
			Assignees: []string{"kvaps"}, StartDate: today, Day: today, SprintStart: today}},
		SprintStates: map[string]SprintState{"alpha": {Current: today}},
	}
	if got := TeamGrid(b, "alpha", today); len(got) != 0 {
		t.Fatalf("TeamGrid = %d card(s); a parked card is not this week's work", len(got))
	}
	if got := MeView(b, "kvaps", today); len(got) != 0 {
		t.Fatalf("MeView = %d card(s); a parked card is nobody's day", len(got))
	}
}

// Every team has a shelf of its own, and nothing declares it. That is the
// point: a backlog nobody had to create is one that is there the first time
// somebody needs it, and a team cannot end up in the state "no backlog yet"
// which is where a shelf goes unused.
func TestEveryTeamHasABacklogWithoutDeclaringOne(t *testing.T) {
	// Nothing is declared anywhere: no board file, no roster entry.
	for _, team := range []string{"alpha", "beta", ""} {
		c := Card{ItemID: "x", Team: team, Parked: true}
		if !InBacklog(c) {
			t.Fatalf("team %q: a parked card is on its team's shelf, declared or not", team)
		}
	}
	// The no-team group is a team like any other here, and keeps its own.
	if !InBacklog(Card{Parked: true}) {
		t.Fatal("the no-team group has a shelf too")
	}
}

// A parked card stands in NO Triage column, week or no week.
//
// The two are exclusive and every door that gives a week takes the card off
// its shelf — but the storage is a git repository anything may write to, so a
// card CAN arrive carrying both. Drawn by its week it would stand in the grid
// AND in the drawer: the same work in two places, counted twice against the
// week, which is the exact double-drawing the exclusivity exists to prevent.
// The shelf wins, because the drawer is where such a card can be dealt with.
func TestAParkedCardStandsInNoWeekEvenCarryingOne(t *testing.T) {
	today := TodayIso()
	week := MondayOf(today)
	b := Board{}
	if got := TriageWeekOf(b, Card{Week: week}, today); got != week {
		t.Fatalf("an ordinary card stands in its own week: %q", got)
	}
	if got := TriageWeekOf(b, Card{Week: week, Parked: true}, today); got != "" {
		t.Fatalf("a parked card stands in no column: %q", got)
	}
}

// Ordering inside a list is the reader's own: dragging is how a backlog gets
// prioritised, so the rank decides, and cards without one fall back to age —
// oldest first, since a card that has waited longest is the one being asked
// about.
func TestABacklogIsOrderedByHandThenByAge(t *testing.T) {
	cards := []Card{
		{ItemID: "c", CreatedAt: "2026-01-03T00:00:00Z"},
		{ItemID: "a", CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "b", CreatedAt: "2026-01-02T00:00:00Z"},
	}
	if got := ids(BacklogOrder(cards)); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("with no ranks the oldest is first: %v", got)
	}

	ranked := []Card{
		{ItemID: "old", CreatedAt: "2026-01-01T00:00:00Z"},
		{ItemID: "pinned", CreatedAt: "2026-01-09T00:00:00Z", Rank: "a"},
	}
	if got := ids(BacklogOrder(ranked)); !reflect.DeepEqual(got, []string{"pinned", "old"}) {
		t.Fatalf("a card somebody placed by hand outranks age: %v", got)
	}
}

// A parked card is off the day boards, and its SUBTASKS go with it.
//
// This half is the parent's own. The board filters never place a subtask by
// itself — they ride their parent, which the API layer appends — so a parked
// parent leaving the grid is what takes its pieces along. The delivery side is
// pinned in pkg/apiserver, where the children are actually appended.
//
// Nothing is written on the subtasks to achieve it. Writing the shelf on each
// child would make coming back a second operation that can half-succeed: a
// card in the plan with two of its five pieces still parked. Derived, the
// return is free and cannot come back wrong.
func TestAParkedCardLeavesTheDayBoards(t *testing.T) {
	today := TodayIso()
	b := Board{
		Cards: []Card{
			{ItemID: "parent", Team: "alpha", Parked: true,
				Assignees: []string{"kvaps"}, StartDate: today, Day: today, SprintStart: today},
			{ItemID: "kid", Team: "alpha", Parent: "parent",
				Assignees: []string{"kvaps"}, StartDate: today, Day: today, SprintStart: today},
		},
		SprintStates: map[string]SprintState{"alpha": {Current: today}},
	}
	if got := TeamGrid(b, "alpha", today); len(got) != 0 {
		t.Fatalf("TeamGrid = %v; a parked card is not this week's work", ids(got))
	}
	if got := MeView(b, "kvaps", today); len(got) != 0 {
		t.Fatalf("MeView = %v; a parked card is nobody's day", ids(got))
	}

	// Planned again, it is back — and nothing had to be undone to bring it.
	b.Cards[0].Parked = false
	if got := ids(TeamGrid(b, "alpha", today)); len(got) != 1 || got[0] != "parent" {
		t.Fatalf("TeamGrid = %v; the card returns as it was", got)
	}
}
