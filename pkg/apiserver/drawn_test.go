package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A gesture is made ON a board, and a person cannot press × on a card they
// cannot see. An agent standing on the Me board should not be able to either —
// it has to say which board it is acting from, and then it has said it.
//
// The test that matters is that this question and the LISTING answer alike:
// a gate of its own would drift, and the drift would be a card the board draws
// and refuses to act on (or the other way round, which is worse).
func TestDrawnAsksTheSameQuestionTheListingAnswers(t *testing.T) {
	t.Parallel()
	today := board.TodayIso()
	b := board.NewBoard([]board.Card{
		{ItemID: "mine", Title: "mine", Team: "alpha", Assignees: []string{"kvaps"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "theirs", Title: "theirs", Team: "alpha", Assignees: []string{"carol"},
			StartDate: today, Day: today, SprintStart: today},
		{ItemID: "parked", Title: "on the shelf", Team: "alpha", Parked: true},
	})
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}

	me := Selector{View: "me", User: "kvaps", Day: today}
	if !Drawn(b, me, "mine") {
		t.Fatal("my own card is on my board and the gate says no")
	}
	if Drawn(b, me, "theirs") {
		t.Fatal("somebody else's card is not on my board")
	}
	// The lead's grid draws both.
	grid := Selector{View: "team", Team: "alpha", Day: today}
	for _, id := range []string{"mine", "theirs"} {
		if !Drawn(b, grid, id) {
			t.Fatalf("%s is on the team's grid and the gate says no", id)
		}
	}
	// A parked card is on no day board at all, and on its team's shelf.
	if Drawn(b, grid, "parked") {
		t.Fatal("a parked card was drawn on the day grid")
	}
	if !Drawn(b, Selector{View: "backlog", Team: "alpha"}, "parked") {
		t.Fatal("a parked card is not on its own shelf")
	}
	// The escape hatch draws everything, which is what it is for.
	for _, id := range []string{"mine", "theirs", "parked"} {
		if !Drawn(b, Selector{View: "all"}, id) {
			t.Fatalf("view=all does not draw %s", id)
		}
	}
	// A card scheduled FURTHER OUT than a grid opens with is still on the
	// Triage board: a gate that kept the listing's six-week default would
	// refuse a gesture on a card the board plainly draws, and a caller that
	// named no window is asking about the board rather than a screenful.
	far := board.AddDays(board.MondayOf(today), 7*8)
	b.Cards = append(b.Cards, board.Card{ItemID: "far", Title: "much later", Team: "alpha", Week: far})
	b = board.NewBoard(b.Cards)
	b.SprintStates = map[string]board.SprintState{"alpha": {Current: today}}
	if !Drawn(b, Selector{View: "triage", Team: "alpha"}, "far") {
		t.Fatal("a card eight weeks out is on the Triage board and the gate says no")
	}
	// A caller that DID name a window is held to it.
	if Drawn(b, Selector{View: "triage", Team: "alpha", Weeks: 2}, "far") {
		t.Fatal("a two-week window drew a card eight weeks out")
	}

	// And a card nobody has is on no board.
	if Drawn(b, Selector{View: "all"}, "nothing") {
		t.Fatal("a card that does not exist was drawn")
	}
}
