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
