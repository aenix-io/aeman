package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// Mirrors are stored as structured YAML — a list of {project, epic} pairs —
// because names are user text: a flat string with a separator would break on
// the first epic containing it, and issue #124 already taught this board
// what happens when a reference format cannot survive its values.
func TestMirrorsSurviveTheCardFile(t *testing.T) {
	in := board.Card{
		ItemID: "01JB4K2E7QZMX3R8V0N5T9WYA1", Title: "shared work",
		Project: "engineering", Epic: "Cozystack",
		Mirrors: []board.Placement{
			{Project: "freedom", Epic: "Launch"},
			{Project: "ops", Epic: "K8s, the hard way"}, // a comma in the name
		},
		Rank: "a", CreatedAt: "2026-08-29T10:00:00Z",
	}
	data, err := EncodeCard(CardFile{Card: in})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mirrors:") {
		t.Fatalf("the file must carry the mirrors:\n%s", data)
	}
	out, err := DecodeCard(in.ItemID, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Card.Mirrors) != 2 || out.Card.Mirrors[0] != in.Mirrors[0] || out.Card.Mirrors[1] != in.Mirrors[1] {
		t.Fatalf("mirrors = %+v, want %+v", out.Card.Mirrors, in.Mirrors)
	}
	// And a card without mirrors writes no key at all.
	plain, err := EncodeCard(CardFile{Card: board.Card{ItemID: "01JB4K2E7QZMX3R8V0N5T9WYB2", Title: "plain"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "mirrors") {
		t.Fatalf("no mirrors, no key:\n%s", plain)
	}
}

// The storage is open to hand edits, and a hand-written scalar entry
// (`mirrors: [foo]`) is not a column — decoding it into an {"", ""}
// placement would smuggle in the very empty pair the service refuses
// everywhere else. Half a column is no column: skipped on read.
func TestHalfWrittenMirrorEntriesAreSkippedOnRead(t *testing.T) {
	data := []byte(`---
title: hand-written
project: engineering
epic: Cozystack
mirrors:
  - foo
  - project: freedom
  - epic: Launch
  - project: freedom
    epic: Launch
rank: a
created: 2026-08-29T10:00:00Z
---
`)
	out, err := DecodeCard("01JB4K2E7QZMX3R8V0N5T9WYB2", data)
	if err != nil {
		t.Fatal(err)
	}
	want := []board.Placement{{Project: "freedom", Epic: "Launch"}}
	if len(out.Card.Mirrors) != 1 || out.Card.Mirrors[0] != want[0] {
		t.Fatalf("only the full pair survives, got %+v", out.Card.Mirrors)
	}
}

// A hand-written mirror equal to the home pair, or written twice, is the
// exact state the x bug lives in: the slot drawn twice and the x
// unmirroring instead of removing. The decoder drops both in a post-pass —
// the mirrors here come BEFORE the home keys on purpose, because a
// hand-written file guarantees no key order.
func TestHandWrittenDuplicateMirrorsAreDropped(t *testing.T) {
	data := []byte(`---
mirrors:
  - project: engineering
    epic: Cozystack
  - project: freedom
    epic: Launch
  - project: freedom
    epic: Launch
title: hand-written
project: engineering
epic: Cozystack
rank: a
created: 2026-08-29T10:00:00Z
---
`)
	out, err := DecodeCard("01JB4K2E7QZMX3R8V0N5T9WYB2", data)
	if err != nil {
		t.Fatal(err)
	}
	want := board.Placement{Project: "freedom", Epic: "Launch"}
	if len(out.Card.Mirrors) != 1 || out.Card.Mirrors[0] != want {
		t.Fatalf("the home twin and the duplicate must go, got %+v", out.Card.Mirrors)
	}
}
