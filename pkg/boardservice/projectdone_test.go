package boardservice

import (
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A PROJECT CARD IS DONE OR NOT DONE (#288). It has no progress bar: the
// plan it stands for is finished or it is not, so its progress takes 0 or
// 100 and nothing between — 100 marks it done, 0 takes it back — and any
// other value is refused by name. Marking it done and reopening it (the
// stage doors) are untouched, and so is a task's progress.
func TestAProjectCardIsDoneOrNotDone(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "slot", Title: "BYOK", Scope: "Security", Project: "core"},
			{ItemID: "task", Title: "Receipts", Team: "alpha"},
		}, nil)
	}
	for _, v := range []int{10, 60, 90} {
		f := seed()
		if err := New(f).SetProgress(ctx, "acme", "slot", v); !errors.Is(err, ErrProjectCardProgress) {
			t.Fatalf("progress %d on a project card = %v, want ErrProjectCardProgress", v, err)
		}
		if logged(f, "SetProgress slot") {
			t.Fatalf("progress %d was written", v)
		}
	}
	f := seed()
	if err := New(f).SetProgress(ctx, "acme", "slot", 100); err != nil {
		t.Fatalf("100 marks it done: %v", err)
	}
	if c := f.get("slot"); !board.Complete(c.Stage, c.Progress) {
		t.Fatalf("not done: %+v", c)
	}
	if err := New(f).SetProgress(ctx, "acme", "slot", 0); err != nil {
		t.Fatalf("0 takes it back: %v", err)
	}
	if err := New(f).SetProgress(ctx, "acme", "task", 60); err != nil {
		t.Fatalf("a task's progress is its own: %v", err)
	}
}

// And no stage door moves a project card between done and not done (#288):
// review (which would also make a review card for it) and locked hold a
// card inside 10–90, and In progress nudges it to 10 — all refused by name
// (ErrProjectCard). Reopening a finished project card takes it back to not
// done, 0, whatever it carried before done. Old values are not migrated: a
// project card already at 60% keeps it until somebody changes it.
func TestNoStageDoorPutsAProjectCardBetweenDoneAndNotDone(t *testing.T) {
	seed := func() *fakeBackend {
		return newFake([]board.Card{
			{ItemID: "slot", Title: "BYOK", Scope: "Security", Project: "core"},
			{ItemID: "done", Title: "Shipped", Scope: "Security", Project: "core", Progress: 100, DoneFrom: 60},
		}, nil)
	}
	for _, stage := range []board.StageKey{board.StageReview, board.StageLocked} {
		f := seed()
		if err := New(f).SetStage(ctx, "acme", "slot", stage); !errors.Is(err, ErrProjectCard) {
			t.Fatalf("%s on a project card = %v, want ErrProjectCard", stage, err)
		}
		for _, c := range f.b.Cards {
			if c.ReviewOf == "slot" {
				t.Fatal("a review card was made for a project card")
			}
		}
	}
	f := seed()
	if err := New(f).SetInProgress(ctx, "acme", "slot"); !errors.Is(err, ErrProjectCard) {
		t.Fatalf("In progress on a project card = %v, want ErrProjectCard", err)
	}
	if err := New(f).Reopen(ctx, "acme", "done"); err != nil {
		t.Fatal(err)
	}
	if c := f.get("done"); c.Progress != 0 || board.Complete(c.Stage, c.Progress) {
		t.Fatalf("reopened = %+v, want not done at 0", c)
	}
}
