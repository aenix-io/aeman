package apiserver

import (
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The board's roster carries, beside how many cards a person is carrying,
// how much they WEIGH (load) and how much a week they get through
// (capacity) — server-side, across every team, so a day board read through
// a filter still draws the whole number beside the person and can go red
// past it without a second request.
func TestMembersCarryLoadAndCapacity(t *testing.T) {
	today := board.TodayIso()
	lastWeek := board.AddDays(board.MondayOf(today), -3)
	b := board.Board{
		People: map[string]board.Person{"lead": {Capacity: 30}},
		Cards: []board.Card{
			{ItemID: "a", Team: "alpha", Assignees: []string{"kvaps"}, Size: board.SizeL, SprintStart: today},
			{ItemID: "b", Team: "alpha", Assignees: []string{"kvaps"}, Size: board.SizeM, SprintStart: today},
			{ItemID: "c", Team: "alpha", Assignees: []string{"kvaps"}, Size: board.SizeXL, Progress: 100, DoneAt: lastWeek},
			{ItemID: "d", Team: "beta", Assignees: []string{"lead"}, Size: board.SizeS, SprintStart: today},
		},
	}
	info := BoardResource(b)
	byLogin := map[string]Member{}
	for _, m := range info.Metadata.Members {
		byLogin[m.Login] = m
	}
	if m := byLogin["kvaps"]; m.Load != 6 || m.Capacity != 0 {
		t.Fatalf("kvaps: %+v — want load 6 (L+M open) and no capacity: a record is not one", m)
	}
	if m := byLogin["lead"]; m.Load != 1 || m.Capacity != 30 {
		t.Fatalf("lead: %+v — want load 1 and the roster's 30", m)
	}
}

// A card resource states one of the four letters or nothing. The storage is
// open — anything may commit to these repositories — so a card can carry a
// size nothing knows; the board weighs it as unsized, and the API must say
// the same rather than echo a value its own PATCH would refuse (400).
func TestACardResourceStatesOnlyASizeTheScaleKnows(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "junk", Size: board.SizeKey("large")},
		{ItemID: "good", Size: board.SizeXL},
	}}
	byID := map[string]Card{}
	for _, c := range b.Cards {
		byID[c.ItemID] = CardResource(b, c)
	}
	if got := byID["junk"].Spec.Size; got != "" {
		t.Errorf("a size nothing knows is stated as %q, want it absent", got)
	}
	if got := byID["good"].Spec.Size; got != "XL" {
		t.Errorf("a real size = %q, want XL", got)
	}
}

// `stage=done` is a value the MCP tool advertises and the derive-capacity
// skill's first step depends on — and it matched NOTHING. Done is derived
// from progress, never stored, so comparing it against the stored stage
// asked for a card whose file says `stage: done`, which no door on this
// board writes. The filter has to ask the same question the board asks.
func TestStageDoneSelectsTheCardsThatAreDone(t *testing.T) {
	b := board.Board{Cards: []board.Card{
		{ItemID: "done-by-progress", Progress: 100},
		{ItemID: "open", Progress: 40},
		{ItemID: "locked", Stage: board.StageLocked, Progress: 10},
	}}
	stage := "done"
	got := []string{}
	for _, c := range FilterCards(b, Selector{View: "all", Stage: &stage}) {
		got = append(got, c.ItemID)
	}
	if len(got) != 1 || got[0] != "done-by-progress" {
		t.Fatalf("stage=done selected %v, want the finished card", got)
	}
	// The stored stages still filter as themselves.
	locked := "locked"
	got = got[:0]
	for _, c := range FilterCards(b, Selector{View: "all", Stage: &locked}) {
		got = append(got, c.ItemID)
	}
	if len(got) != 1 || got[0] != "locked" {
		t.Fatalf("stage=locked selected %v", got)
	}
}
