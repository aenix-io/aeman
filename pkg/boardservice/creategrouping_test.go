package boardservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// groupingBoard is one team, platform, with a current and a previous sprint.
func groupingBoard(cards []board.Card) *fakeBackend {
	return newFake(cards, map[string]board.SprintState{
		"platform": {Current: board.TodayIso(), Previous: board.AddDays(board.TodayIso(), -1), ItemID: "st-p"},
	})
}

// A subtask with nowhere else to be is still deleted: leaving it grouped
// would be an × that did nothing.
func TestTheGridRemoveStillDeletesAColumnlessSubtask(t *testing.T) {
	f := groupingBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
		{ItemID: "c1", Title: "plain child", Team: "platform", Parent: "p",
			SprintStart: board.TodayIso()},
	})
	if err := New(f).Remove(ctx, "acme", "c1", board.ViewAll, RemoveAuto); err != nil {
		t.Fatal(err)
	}
	b, _ := f.LoadBoard(ctx, "acme")
	if _, ok := findCard(b, "c1"); ok {
		t.Fatal("nowhere else to be: the × deletes it")
	}
}

// The create door validates the parent BEFORE writing anything: a card
// born and then deleted because its parent was never findable is a stray
// ADDED broadcast and a created event for a card that should not exist.
func TestCreatingUnderAnImpossibleParentCreatesNothing(t *testing.T) {
	f := groupingBoard(nil)
	svc := New(f)
	before, _ := f.LoadBoard(ctx, "acme")
	if _, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "ghost",
	}); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("a parent that does not exist: %v", err)
	}
	after, _ := f.LoadBoard(ctx, "acme")
	if len(after.Cards) != len(before.Cards) {
		t.Fatalf("the refusal may not leave a card behind: %d → %d", len(before.Cards), len(after.Cards))
	}
	// And nothing was ever written: a create-then-delete pair broadcasts a
	// parentless instant that watchers and mid-sync reloads see as a stray
	// top-level card, and logs a created event for a card that never was.
	if n := f.count("CreateCard"); n != 0 {
		t.Fatalf("the refusal must come before the write, saw %d creates", n)
	}
}

// When the grouping fails the create is undone — and if the undo fails
// too, BOTH reasons travel. One of the two create paths used to swallow
// the second and hand back the card it had just tried to delete.
func TestAFailedGroupingUndoesTheCreateAndReportsWhatItCannot(t *testing.T) {
	f := groupingBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	f.parentErr = errors.New("the grouping would not take")
	f.deleteErr = errors.New("and the branch would not budge")
	svc := New(f)
	card, err := svc.CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "p",
	})
	if err == nil {
		t.Fatal("the grouping failed, so the create must not stand")
	}
	if !strings.Contains(err.Error(), "would not take") ||
		!strings.Contains(err.Error(), "would not budge") {
		t.Fatalf("both reasons must travel: %v", err)
	}
	if card.ItemID != "" {
		t.Fatalf("and no card is handed back: %+v", card)
	}
}

// A create whose grouping fails leaves NOTHING behind: the point of the
// undo, and the case the double-failure test cannot show, since there the
// undo fails too.
func TestAFailedGroupingLeavesNoCardBehind(t *testing.T) {
	f := groupingBoard([]board.Card{
		{ItemID: "p", Title: "parent", Team: "platform"},
	})
	f.parentErr = errors.New("the grouping would not take")
	before, _ := f.LoadBoard(ctx, "acme")
	card, err := New(f).CreateCard(ctx, "acme", CreateCardArgs{
		Title: "child", Team: "platform", Parent: "p",
	})
	if err == nil {
		t.Fatal("the grouping failed")
	}
	if card.ItemID != "" {
		t.Fatalf("no card is handed back: %+v", card)
	}
	after, _ := f.LoadBoard(ctx, "acme")
	if len(after.Cards) != len(before.Cards) {
		t.Fatalf("and none is left on the board: %d → %d", len(before.Cards), len(after.Cards))
	}
}
