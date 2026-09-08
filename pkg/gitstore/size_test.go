package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The size is one line of front matter — `size: L` — and it round-trips
// through the file as the letter, never as points: the letter is what a
// person said, and a writer that follows the file format can put one on a
// card without knowing the scale. An unsized card writes no line at all.
func TestASizedCardRoundTripsThroughItsFile(t *testing.T) {
	f := CardFile{Card: board.Card{ItemID: "01JB4SIZE", Title: "Реализация envoy-gateway", Size: board.SizeL}}
	data, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nsize: L\n") {
		t.Fatalf("the file carries the letter:\n%s", data)
	}
	back, err := DecodeCard("01JB4SIZE", data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Card.Size != board.SizeL {
		t.Fatalf("size after the round trip = %q, want L", back.Card.Size)
	}

	plain, err := EncodeCard(CardFile{Card: board.Card{ItemID: "01JB4NONE", Title: "лендинг"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "size:") {
		t.Fatalf("an unsized card writes no size line:\n%s", plain)
	}
}
