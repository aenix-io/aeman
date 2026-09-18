package server

import (
	"reflect"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// fullBoard is a board with every container of board.Board and board.Card
// non-empty. The completeness of THIS fixture is what the guard below
// checks: a new reference-typed field arrives here first, and only then can
// the clone be asked about.
func fullBoard() board.Board {
	card := func(id string) board.Card {
		return board.Card{
			ItemID:    id,
			Title:     id,
			Assignees: []string{"ann"},
			Notes:     []board.Note{{ID: "n1", Body: "first"}},
			Mirrors:   []board.Placement{{Project: "p"}},
		}
	}
	return board.Board{
		Board:         "acme",
		Cards:         []board.Card{card("c1")},
		Tasks:         []board.Card{card("t1")},
		SprintStates:  map[string]board.SprintState{"alpha": {ItemID: "s1"}},
		People:        map[string]board.Person{"ann": {Capacity: 20}},
		TeamOrder:     []string{"alpha"},
		Epics:         []board.EpicCol{{ItemID: "e1", Name: "epic"}},
		Deadlines:     []board.Deadline{{ItemID: "d1", Week: "2026-09-14"}},
		Processes:     []board.Process{{ItemID: "pr1", Name: "proc"}},
		Projects:      []string{"proj"},
		ProjectStates: map[string]string{"proj": "ps1"},
		Domains:       map[string]string{"s1": "primary"},
	}
}

// refFields names every slice- or map-typed field of a struct: the ones a
// copy of it still shares with the original.
func refFields(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		switch t.Field(i).Type.Kind() {
		case reflect.Slice, reflect.Map:
			out = append(out, t.Field(i).Name)
		}
	}
	return out
}

// A board handed out of the cache must own its containers. Whoever holds one
// reads it with no lock while the cache keeps editing its own rows in place
// under e.mu, so a shared array is a torn read and a shared map is a fatal
// "concurrent map read and map write".
//
// The check is by field NAME rather than against a written-down list: adding
// a slice or a map to board.Board or board.Card and forgetting to clone it
// is exactly the way this regresses, and it would regress silently.
func TestDetachedSharesNoContainerWithTheCache(t *testing.T) {
	in := fullBoard()
	out := detached(in)

	check := func(what string, before, after reflect.Value) {
		t.Helper()
		for _, name := range refFields(before.Type()) {
			b, a := before.FieldByName(name), after.FieldByName(name)
			if b.Len() == 0 {
				t.Errorf("%s.%s is empty in the fixture; populate it, or the clone is never asked about it", what, name)
				continue
			}
			if b.Pointer() == a.Pointer() {
				t.Errorf("%s.%s is the cache's own container, not a copy: detached must clone it", what, name)
			}
		}
	}

	bv, ov := reflect.ValueOf(in), reflect.ValueOf(out)
	check("Board", bv, ov)

	// Every []Card field of Board, derived rather than listed. A third one
	// added later is caught above as a container, but its cards' own
	// Assignees, Notes and Mirrors would stay shared, and a hardcoded pair
	// of call sites would pass while they did.
	cards := reflect.TypeFor[[]board.Card]()
	for i := range bv.NumField() {
		f := bv.Type().Field(i)
		if f.Type != cards {
			continue
		}
		if bv.Field(i).Len() == 0 {
			t.Errorf("Board.%s is empty in the fixture; the cards inside it are never checked", f.Name)
			continue
		}
		check(f.Name+"[0]", bv.Field(i).Index(0), ov.Field(i).Index(0))
	}
}

// The point of the clone, said as behaviour rather than as pointers: the
// writes the cache makes in place after handing a board out are the ones
// that used to reach the holder.
func TestWritingTheCacheDoesNotReachABoardAlreadyHandedOut(t *testing.T) {
	e := &boardEntry{board: fullBoard(), loaded: true, loadedAt: time.Now()}
	handed, state := e.cached()
	if state != cacheFresh {
		t.Fatalf("state = %v, want fresh; the snapshot under test was never taken", state)
	}

	// Writes of each shape the cache takes in place: a field on a row, a
	// field inside a row's own slice, and entries in its maps.
	e.board.Cards[0].Title = "renamed"
	e.board.Cards[0].Notes[0].Body = "edited"
	e.board.Tasks[0].Process = "moved"
	e.board.Deadlines[0].Week = "2026-12-25"
	e.board.Epics[0].Name = "recolumned"
	e.board.Processes[0].Paused = true
	e.board.ProjectStates["proj"] = "other"
	e.board.People["ann"] = board.Person{Capacity: 99}
	e.board.Domains["s1"] = "elsewhere"

	if got := handed.Cards[0].Title; got != "c1" {
		t.Errorf("card title = %q, want %q", got, "c1")
	}
	if got := handed.Cards[0].Notes[0].Body; got != "first" {
		t.Errorf("note body = %q, want %q", got, "first")
	}
	if got := handed.Tasks[0].Process; got != "" {
		t.Errorf("task process = %q, want empty", got)
	}
	if got := handed.Deadlines[0].Week; got != "2026-09-14" {
		t.Errorf("deadline week = %q, want %q", got, "2026-09-14")
	}
	if got := handed.Epics[0].Name; got != "epic" {
		t.Errorf("epic name = %q, want %q", got, "epic")
	}
	if handed.Processes[0].Paused {
		t.Error("process paused through a handed-out board")
	}
	if got := handed.ProjectStates["proj"]; got != "ps1" {
		t.Errorf("project state = %q, want %q", got, "ps1")
	}
	if got := handed.People["ann"].Capacity; got != 20 {
		t.Errorf("capacity = %d, want 20", got)
	}
	if got := handed.Domains["s1"]; got != "primary" {
		t.Errorf("domain = %q, want %q", got, "primary")
	}
}

// An empty board must stay empty rather than gain allocated containers: a
// nil Domains is how a single-domain board says it records none, and Visible
// branches on a nil ProjectStates.
func TestDetachedKeepsNilContainersNil(t *testing.T) {
	// Derived from Board rather than listed. A hand-written list checked six
	// of the eleven, and the guard next door demanding all eleven non-empty
	// is what made the other five look covered.
	out := reflect.ValueOf(detached(board.Board{}))
	for _, name := range refFields(out.Type()) {
		if !out.FieldByName(name).IsNil() {
			t.Errorf("detached turned a nil %s into an allocated empty one", name)
		}
	}
}

// detached clones board.Board's own containers and each board.Card's, and
// stops there. That is complete only while every other struct reachable from
// a board is flat: a slice added to EpicCol or Deadline would slip past the
// clone and past the guard above, which walks one level and would keep
// passing.
//
// Note and SprintState cannot gain one at all. Both are compared with ==,
// and a struct holding a slice is not comparable, so the compiler refuses
// one before this test could object.
//
// A pointer counts wherever it sits, Board and Card included: detached
// clones no pointer, so a copy of any struct holding one shares the pointee.
// So does an interface, whose content is decided at runtime, and so does an
// unexported container, which no clone outside the board package can reach.
//
// So pin the flatness the clone rests on. This names the field the day one
// appears, for the kinds it enumerates: slices, maps, pointers, interfaces.
//
// It does NOT cover arrays, and the omission is deliberate rather than
// pending. An array is a value: [3]string copies cleanly and shares nothing,
// while [2][]string and [2]Card share through their elements, so the correct
// test is recursive and not one more kind in the switch. Adding the kind
// would flag the safe case. The deeper point is that a guard reading types
// can only be as complete as whoever wrote the list, and the failure it keeps
// meeting is a divergence between what the types declare and what the clone
// does; catching that needs a check that observes the clone instead.
func TestTypesBelowTheClonedOnesAreFlat(t *testing.T) {
	descended := map[reflect.Type]bool{
		reflect.TypeFor[board.Board](): true,
		reflect.TypeFor[board.Card]():  true,
	}
	// detachedCards runs on Board.Cards and Board.Tasks and nowhere else, so
	// exempting Card by TYPE is only right while those are the only places a
	// Card appears. The exemption is keyed by type and the behaviour is keyed
	// by path, and that mismatch is what lets a Card somewhere else keep
	// sharing its notes with the cache while both guards stay green.
	cardType := reflect.TypeFor[board.Card]()
	boardType := reflect.TypeFor[board.Board]()
	reachesCard := func(f reflect.Type) bool {
		for f.Kind() == reflect.Slice || f.Kind() == reflect.Map || f.Kind() == reflect.Pointer {
			f = f.Elem()
		}
		return f == cardType
	}
	seen := map[reflect.Type]bool{}
	type gap struct{ field, why string }
	var found []gap
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		switch t.Kind() {
		case reflect.Slice, reflect.Array, reflect.Pointer:
			walk(t.Elem())
		case reflect.Map:
			walk(t.Key())
			walk(t.Elem())
		case reflect.Struct:
			if seen[t] {
				return
			}
			seen[t] = true
			for i := range t.NumField() {
				f := t.Field(i)
				if !f.IsExported() {
					// detached is in another package, so it cannot reach
					// these at all: a container here is uncloneable rather
					// than merely uncloned, and the only useful thing the
					// guard can do is say so.
					switch f.Type.Kind() {
					case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
						found = append(found, gap{t.Name() + "." + f.Name,
							"unexported, so no clone outside its own package can reach it"})
					}
					continue
				}
				viaDetachedCards := t == boardType && (f.Name == "Cards" || f.Name == "Tasks")
				if reachesCard(f.Type) && !viaDetachedCards {
					found = append(found, gap{t.Name() + "." + f.Name,
						"detachedCards only reaches Board.Cards and Board.Tasks, so this card's own containers stay shared"})
				}
				switch f.Type.Kind() {
				case reflect.Slice, reflect.Map:
					// Cloned on Board and Card, nowhere else.
					if !descended[t] {
						found = append(found, gap{t.Name() + "." + f.Name,
							"detached does not descend into " + t.Name() + ": clone it there, or make detached recurse"})
						break
					}
					// Cloned, but one level deep. A container OF containers
					// keeps sharing its inner ones through the copy.
					if e := f.Type.Elem(); e.Kind() == reflect.Slice || e.Kind() == reflect.Map || e.Kind() == reflect.Pointer {
						found = append(found, gap{t.Name() + "." + f.Name,
							"its elements are containers and the clone is one level deep, so they stay shared"})
					}
				case reflect.Pointer:
					// Cloned nowhere: a copy of any struct shares the pointee.
					found = append(found, gap{t.Name() + "." + f.Name,
						"detached clones no pointer, so every copy shares the pointee"})
				case reflect.Interface:
					// What is behind it is decided at runtime, so no clone
					// written against the types can know whether to descend.
					found = append(found, gap{t.Name() + "." + f.Name,
						"an interface may hold a container and the clone cannot see inside it"})
				}
				walk(f.Type)
			}
		}
	}
	walk(reflect.TypeFor[board.Board]())

	for _, g := range found {
		t.Errorf("%s escapes the clone: %s", g.field, g.why)
	}
}
