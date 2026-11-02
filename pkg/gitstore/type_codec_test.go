package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A planning card is a card file with one more key, `type`, written right
// after the title so a reader of the file sees what it is before anything
// else (docs/design/hierarchy.md, "Layout"). A task has no `type` line at
// all: the file says what is, not what is not.
func TestTheTypeKeyRoundTripsAfterTheTitle(t *testing.T) {
	f := CardFile{Card: board.Card{
		ItemID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Title: "Launch in Portugal", Type: board.TypeInitiative,
		Parent: "01ARZ3NDEKTSV4RRFFQ69G5FAW", StartDate: "2026-11-01", Day: "2026-12-31", Rank: "a0",
	}}
	out, err := EncodeCard(f)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.HasPrefix(text, "---\ntitle: Launch in Portugal\ntype: initiative\n") {
		t.Fatalf("type is written right after the title:\n%s", text)
	}
	back, err := DecodeCard(f.Card.ItemID, out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Card.Type != board.TypeInitiative || back.Card.Parent != f.Card.Parent {
		t.Fatalf("round trip lost the type or the parent: %+v", back.Card)
	}
	if len(back.Extra) != 0 {
		t.Fatalf("type is a known key, not Extra: %+v", back.Extra)
	}
	// A task writes no type line.
	task, err := EncodeCard(CardFile{Card: board.Card{ItemID: f.Card.ItemID, Title: "Invoices", Rank: "a0"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(task), "type:") {
		t.Fatalf("a task has no type line:\n%s", task)
	}
}
