package boardservice

import (
	"context"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A lead sets a person's capacity when they know better than the record —
// a half week, a newcomer, somebody covering for two — and it lands in the
// roster (users/<login>.yaml). Zero takes it back, so the board derives one
// again; a number that could not be a person's week is refused by name.
func TestALeadSetsAPersonsCapacityAndZeroTakesItBack(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-09-07"}})
	svc := New(f)
	ctx := context.Background()

	if err := svc.SetPersonCapacity(ctx, "acme", "tym83", 40); err != nil {
		t.Fatal(err)
	}
	if got := f.b.People["tym83"].Capacity; got != 40 {
		t.Fatalf("capacity = %d, want 40", got)
	}
	// Saying the same number again is not a change and writes nothing.
	n := f.count("SetPersonCapacity tym83")
	if err := svc.SetPersonCapacity(ctx, "acme", "tym83", 40); err != nil {
		t.Fatal(err)
	}
	if f.count("SetPersonCapacity tym83") != n {
		t.Fatal("re-saying the capacity must write nothing")
	}
	if err := svc.SetPersonCapacity(ctx, "acme", "tym83", 0); err != nil {
		t.Fatal(err)
	}
	if got := f.b.People["tym83"].Capacity; got != 0 {
		t.Fatalf("zero takes the number back, got %d", got)
	}
	// Taking back a number nobody set writes nothing either: there is no
	// file to make for a person the roster never had.
	if err := svc.SetPersonCapacity(ctx, "acme", "nobody", 0); err != nil {
		t.Fatal(err)
	}
	if f.saw("SetPersonCapacity nobody 0") {
		t.Fatal("clearing an unset capacity must not create a roster entry")
	}

	for _, bad := range []int{-1, 1000} {
		if err := svc.SetPersonCapacity(ctx, "acme", "tym83", bad); !errors.Is(err, ErrBadCapacity) {
			t.Errorf("capacity %d: err = %v, want ErrBadCapacity", bad, err)
		}
	}
	if err := svc.SetPersonCapacity(ctx, "acme", "  ", 10); !errors.Is(err, ErrBadCapacity) {
		t.Errorf("a blank login: err = %v, want ErrBadCapacity", err)
	}
}
