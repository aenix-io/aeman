package boardservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A lead sets a person's capacity when they know better than the record —
// a half week, a newcomer, somebody covering for two — and it lands in the
// roster (users/<login>.yaml). Zero takes it back, leaving the person with
// no number at all; one that could not be a person's week is refused by name.
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

// A TEAM's points a week are set the same way, in the team's own file. It is
// not read off its people: working
// that sum out means splitting people between teams by a record eleven days
// deep — which is the derive-capacity skill's job, where the record's own
// thinness can be said out loud.
func TestALeadSetsATeamsPointsAWeek(t *testing.T) {
	f := newFake(nil, map[string]board.SprintState{"alpha": {Current: "2026-09-07"}})
	svc := New(f)
	ctx := context.Background()

	if err := svc.SetTeamPoints(ctx, "acme", "alpha", 40); err != nil {
		t.Fatal(err)
	}
	if got := f.b.SprintStates["alpha"].Capacity.Points; got != 40 {
		t.Fatalf("points = %d, want 40", got)
	}
	f.log = nil
	if err := svc.SetTeamPoints(ctx, "acme", "alpha", 40); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.log {
		if c != "LoadBoard" {
			t.Fatalf("re-saying the number must write nothing, got %v", f.log)
		}
	}
	if err := svc.SetTeamPoints(ctx, "acme", "alpha", 0); err != nil {
		t.Fatal(err)
	}
	if got := f.b.SprintStates["alpha"].Capacity.Points; got != 0 {
		t.Fatalf("points = %d, want 0 — zero takes the number back", got)
	}
	// A team nobody declared has nothing to say a capacity about.
	if err := svc.SetTeamPoints(ctx, "acme", "ghost", 10); !errors.Is(err, ErrTeamNotFound) {
		t.Fatalf("an unknown team: err = %v, want ErrTeamNotFound", err)
	}
	for _, bad := range []int{-1, 1000} {
		if err := svc.SetTeamPoints(ctx, "acme", "alpha", bad); !errors.Is(err, ErrBadCapacity) {
			t.Errorf("points %d: err = %v, want ErrBadCapacity", bad, err)
		}
	}
}

// A login goes into a file PATH — users/<login>.yaml — and it is the only
// value a request supplies that does: every other writer names its file by a
// fresh ULID. It was taken as given, so `..` or a slash in it produced a file
// somewhere else in the repository that nothing on this board can read back,
// and a login with a space or a colon produced one nothing would ever look
// for. A capacity may still be set for somebody the board has never seen —
// that is the newcomer a lead sizes before their first card — but it has to
// be a name that could be one.
func TestACapacityIsSetForALoginThatCouldBeOne(t *testing.T) {
	f := newFake(nil, nil)
	svc := New(f)
	ctx := context.Background()

	for _, bad := range []string{"../etc/passwd", "a/b", "with space", ".hidden", "", strings.Repeat("x", 100)} {
		if err := svc.SetPersonCapacity(ctx, "acme", bad, 10); !errors.Is(err, ErrBadCapacity) {
			t.Errorf("SetPersonCapacity(%q) = %v, want ErrBadCapacity", bad, err)
		}
	}
	// A newcomer the board has never seen is still a person: forge logins
	// carry dashes, dots and digits.
	for _, ok := range []string{"kvaps", "Julia-psps", "vsop78", "a.b_c"} {
		if err := svc.SetPersonCapacity(ctx, "acme", ok, 10); err != nil {
			t.Errorf("SetPersonCapacity(%q) = %v", ok, err)
		}
	}
}
