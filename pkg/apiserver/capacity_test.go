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
