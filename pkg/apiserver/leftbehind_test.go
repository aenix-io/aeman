package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A card finished during a day and then tidied away with the × was on that
// day's board and was done there. The × moves it back into the previous
// sprint, dates and all, so its dates no longer say where it was worked —
// and only the day it was LEFT on does. A record of that day gives it back
// (LeftOn); today's board does not, because taking the card off today is
// exactly what the × is for.
func TestARecordGivesBackWhatTheCrossTookOff(t *testing.T) {
	const day, prev = "2026-08-24", "2026-08-17"
	b := board.Board{
		SprintStates: map[string]board.SprintState{
			"portal": {Current: day, Previous: prev},
			"other":  {Current: day, Previous: prev},
		},
		Cards: []board.Card{
			// Worked on the 24th and put away: its dates are the previous
			// sprint's now, and leftAt remembers the day it stood on.
			{ItemID: "tidied", Team: "portal", Assignees: []string{"kvaps"}, Progress: 100,
				SprintStart: prev, StartDate: prev, Day: prev, LeftAt: day, Rank: "a"},
			// The ordinary card of that day.
			{ItemID: "kept", Team: "portal", Assignees: []string{"kvaps"}, Progress: 60,
				SprintStart: day, StartDate: day, Day: day, Rank: "b"},
			// Another team's card left behind on the same day: not this
			// board's business.
			{ItemID: "elsewhere", Team: "other", Assignees: []string{"lex"}, Progress: 100,
				SprintStart: prev, StartDate: prev, Day: prev, LeftAt: day, Rank: "c"},
		},
	}

	ids := func(sel Selector) []string {
		var out []string
		for _, c := range FilterCards(b, sel) {
			out = append(out, c.ItemID)
		}
		return out
	}

	// Today's board: the × did its job, the card is gone.
	live := Selector{View: "team", Team: "portal", Day: day}
	if got := ids(live); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("the live day = %v; the × takes the card off it", got)
	}

	// The record of that day: it is back, done, beside the card that stayed.
	asRecord := live
	asRecord.LeftOn = day
	got := ids(asRecord)
	found := map[string]bool{}
	for _, id := range got {
		found[id] = true
	}
	if !found["tidied"] || !found["kept"] {
		t.Fatalf("the record of that day = %v; it must hold both", got)
	}
	if found["elsewhere"] {
		t.Fatalf("another team's card is not on this board: %v", got)
	}

	// The Me view draws the same line, and only for the person it belonged to.
	me := Selector{View: "me", User: "kvaps", Day: day, LeftOn: day}
	if got := ids(me); !contains(got, "tidied") {
		t.Fatalf("the Me record of that day = %v", got)
	}
	notMine := Selector{View: "me", User: "lex", Day: day, LeftOn: day}
	if got := ids(notMine); contains(got, "tidied") {
		t.Fatalf("somebody else's card = %v", got)
	}

	// A day the card was not left on gives nothing back.
	other := live
	other.LeftOn = prev
	if got := ids(other); contains(got, "tidied") {
		t.Fatalf("another day's record = %v", got)
	}
}
