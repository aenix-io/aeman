package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// TO DO is one line of front matter — `todo: true` — on a parked task the
// Roadmap marked ready (K15). It round-trips through the file, and a card
// without the flag writes no line at all, so every card written before the
// flag existed reads the same and re-encodes byte for byte.
func TestTheToDoFlagRoundTripsThroughItsFile(t *testing.T) {
	f := CardFile{Card: board.Card{ItemID: "01JB4TODO", Title: "ready", Parked: true, Todo: true}}
	data, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nparked: true\ntodo: true\n") {
		t.Fatalf("the file carries the flag after parked:\n%s", data)
	}
	back, err := DecodeCard("01JB4TODO", data)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Card.Todo || len(back.Extra) != 0 {
		t.Fatalf("after the round trip todo = %v, extra = %v; want the flag read as the field", back.Card.Todo, back.Extra)
	}

	plain, err := EncodeCard(CardFile{Card: board.Card{ItemID: "01JB4NONE", Title: "not ready", Parked: true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "todo:") {
		t.Fatalf("a card without the flag writes no todo line:\n%s", plain)
	}
}
