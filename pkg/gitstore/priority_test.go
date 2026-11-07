package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The priority is one line of front matter — `priority: P1` — on any card
// (K15). It round-trips through the file, and a card without one writes no
// line, so every card written before it existed reads the same and
// re-encodes byte for byte.
func TestThePriorityRoundTripsThroughItsFile(t *testing.T) {
	f := CardFile{Card: board.Card{ItemID: "01JB4PRIO", Title: "matters", Type: board.TypeEpic, Priority: board.PriorityP1}}
	data, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\npriority: P1\n") {
		t.Fatalf("the file carries the priority:\n%s", data)
	}
	back, err := DecodeCard("01JB4PRIO", data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Card.Priority != board.PriorityP1 || len(back.Extra) != 0 {
		t.Fatalf("after the round trip priority = %q, extra = %v", back.Card.Priority, back.Extra)
	}
	plain, err := EncodeCard(CardFile{Card: board.Card{ItemID: "01JB4NONE", Title: "plain"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "priority:") {
		t.Fatalf("a card without a priority writes no line:\n%s", plain)
	}
}

// `todo` was a flag of an unreleased build (the TO DO this priority
// replaced). A file that carries one is read with the key as an UNKNOWN key —
// kept, written back as it was, and meaning nothing.
func TestAStrayTodoKeyIsKeptAndIgnored(t *testing.T) {
	data := []byte("---\ntitle: old\nparked: true\ntodo: true\nrank: a\n---\n")
	f, err := DecodeCard("01JB4TODO", data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Card.Priority != board.PriorityNone || len(f.Extra) != 1 || f.Extra[0].Key != "todo" {
		t.Fatalf("the stray key is an unknown key: priority %q, extra %+v", f.Card.Priority, f.Extra)
	}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "todo: true") {
		t.Fatalf("the unknown key is written back:\n%s", out)
	}
}
