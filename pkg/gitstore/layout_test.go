package gitstore

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// G1 — a card's path is cards/<a>/<b>/<id>.md with a, b the id's LAST two
// characters, one per level; it never encodes state, so it never changes.

func TestCardPathShardsByTail(t *testing.T) {
	got, err := CardPath("01JB4K2E7QZMX3R8V0N5T9WYC1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md" {
		t.Fatalf("CardPath = %q", got)
	}
}

// Two ids that differ only in their last character land in different
// leaves — the tail, not the head, spreads the load (a ULID's head is a
// timestamp and near-constant within a month).
func TestCardPathTailDecidesTheLeaf(t *testing.T) {
	a, _ := CardPath("01JB4K2E7QZMX3R8V0N5T9WYC1")
	b, _ := CardPath("01JB4K2E7QZMX3R8V0N5T9WYC2")
	if a[:len("cards/c/1")] == b[:len("cards/c/2")] {
		t.Fatalf("same leaf for ids differing in the last char: %q %q", a, b)
	}
	// And two ids with the same tail share a leaf however different their heads are.
	c, _ := CardPath("7ZZZZZZZZZZZZZZZZZZZZZZZC1")
	if c[:len("cards/c/1/")] != a[:len("cards/c/1/")] {
		t.Fatalf("different leaf for the same tail: %q %q", a, c)
	}
}

// Lower-case in the path whatever the id's case; an id too short to shard
// is refused, not guessed at.
func TestCardPathNormalisesAndRefuses(t *testing.T) {
	got, err := CardPath("01JB4K2E7QZMX3R8V0N5T9WYAB")
	if err != nil || !strings.HasPrefix(got, "cards/a/b/") {
		t.Fatalf("CardPath = %q, %v", got, err)
	}
	if _, err := CardPath("x"); err == nil {
		t.Fatal("a one-char id must be refused")
	}
	if _, err := CardPath(""); err == nil {
		t.Fatal("an empty id must be refused")
	}
}

// G2 — the file is front-matter + body. Empty fields are omitted, unknown
// keys survive a rewrite, and a decode→encode round trip is the identity.

func sample() CardFile {
	return CardFile{
		Card: board.Card{
			ItemID:      "01JB4K2E7QZMX3R8V0N5T9WYC1",
			Title:       "Carry-over ignores a deferred card",
			Assignees:   []string{"kitsunoff"},
			Author:      "kvaps",
			Team:        "portal",
			Zone:        board.ZoneYellow,
			Stage:       board.StageLocked,
			Progress:    40,
			StartDate:   "2026-08-26",
			Day:         "2026-08-28",
			SprintStart: "2026-08-24",
			Project:     "portal",
			Epic:        "Bugs",
			Parent:      "01JB4K2E7QZMX3R8V0N5T9WYC0",
			Rank:        "a0m",
			CreatedAt:   "2026-08-26T09:14:03Z",
			Description: "Free-form description.\n\nTwo paragraphs.",
			Notes: []board.Note{
				{ID: "01JB4K9P2R5T7VXY8Z0A1B2C3D", CreatedAt: "2026-08-26T10:02:11Z", Author: "kitsunoff", Body: "reproduced on board 37"},
				{ID: "01JB4K9P2R5T7VXY8Z0A1B2C3E", CreatedAt: "2026-08-27T08:00:00Z", Author: "", Body: "two lines\nsecond line"},
			},
		},
	}
}

func TestCardFileRoundTrip(t *testing.T) {
	in := sample()
	data, err := EncodeCard(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeCard(in.Card.ItemID, data)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	again, err := EncodeCard(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("round trip is not the identity:\n--- first\n%s\n--- second\n%s", data, again)
	}
	if out.Card.Title != in.Card.Title || out.Card.Progress != 40 || out.Card.Zone != board.ZoneYellow || out.Card.Parent != in.Card.Parent {
		t.Fatalf("fields lost: %+v", out.Card)
	}
	if out.Card.Description != in.Card.Description {
		t.Fatalf("description lost: %q", out.Card.Description)
	}
	if len(out.Card.Notes) != 2 || out.Card.Notes[1].Body != "two lines\nsecond line" || out.Card.Notes[0].Author != "kitsunoff" || out.Card.Notes[0].ID != in.Card.Notes[0].ID {
		t.Fatalf("notes lost: %+v", out.Card.Notes)
	}
}

// An empty field is not written at all: the file says what IS, not what
// is not. Progress 0 is the one zero that stands for "unset" in the domain
// and is omitted like the rest.
func TestCardFileOmitsEmptyFields(t *testing.T) {
	data, err := EncodeCard(CardFile{Card: board.Card{ItemID: "01JB4K2E7QZMX3R8V0N5T9WYC1", Title: "bare", CreatedAt: "2026-08-26T09:14:03Z"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"assignees:", "team:", "zone:", "stage:", "progress:", "day:", "start:", "sprint:", "week:", "project:", "epic:", "parent:", "reviewOf:", "reviewRound:", "recurrence:", "process:", "task:", "accumulate:", "link:", "github:", "doneFrom:", "movedFrom:", "## Notes"} {
		if bytes.Contains(data, []byte(key)) {
			t.Fatalf("empty %s written:\n%s", key, data)
		}
	}
	for _, key := range []string{"title: bare", "created: 2026-08-26T09:14:03Z"} {
		if !bytes.Contains(data, []byte(key)) {
			t.Fatalf("missing %s:\n%s", key, data)
		}
	}
}

// A newer server must not strip what an older one wrote, and vice versa:
// keys the decoder does not know ride along and come back out unchanged.
func TestCardFileKeepsUnknownKeys(t *testing.T) {
	data, _ := EncodeCard(sample())
	// Splice an unknown key into the front-matter.
	patched := bytes.Replace(data, []byte("title:"), []byte("someFutureKey: keep me\ntitle:"), 1)
	out, err := DecodeCard("01JB4K2E7QZMX3R8V0N5T9WYC1", patched)
	if err != nil {
		t.Fatal(err)
	}
	out.Card.Progress = 55 // an ordinary setter rewrite
	again, err := EncodeCard(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(again, []byte("someFutureKey: keep me")) {
		t.Fatalf("unknown key stripped on rewrite:\n%s", again)
	}
	if !bytes.Contains(again, []byte("progress: 55")) {
		t.Fatalf("the rewrite itself was lost:\n%s", again)
	}
}

// A cleared field disappears from the file rather than lingering as an
// empty value.
func TestCardFileClearedFieldIsOmitted(t *testing.T) {
	in := sample()
	data, _ := EncodeCard(in)
	out, _ := DecodeCard(in.Card.ItemID, data)
	out.Card.Epic = ""
	out.Card.Project = ""
	again, _ := EncodeCard(out)
	if bytes.Contains(again, []byte("epic:")) || bytes.Contains(again, []byte("project:")) {
		t.Fatalf("cleared fields still written:\n%s", again)
	}
}

// G3 — derived states are never written. A card at 100% has progress: 100
// and doneFrom; no stage line appears for it, and In Progress is never a
// value on disk. An explicit stage (locked, review, recurrent, done) is.
func TestCardFileDerivedStateNotStored(t *testing.T) {
	done := board.Card{ItemID: "01JB4K2E7QZMX3R8V0N5T9WYC1", Title: "shipped", Progress: 100, DoneFrom: 40, CreatedAt: "2026-08-26T09:14:03Z"}
	data, err := EncodeCard(CardFile{Card: done})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("stage:")) {
		t.Fatalf("a derived done wrote a stage line:\n%s", data)
	}
	if !bytes.Contains(data, []byte("progress: 100")) || !bytes.Contains(data, []byte("doneFrom: 40")) {
		t.Fatalf("progress or doneFrom missing:\n%s", data)
	}
	wip := board.Card{ItemID: "01JB4K2E7QZMX3R8V0N5T9WYC1", Title: "half", Progress: 40, CreatedAt: "2026-08-26T09:14:03Z"}
	data, _ = EncodeCard(CardFile{Card: wip})
	if bytes.Contains(data, []byte("stage:")) || bytes.Contains(data, []byte("in-progress")) {
		t.Fatalf("In Progress leaked onto disk:\n%s", data)
	}
	explicit := board.Card{ItemID: "01JB4K2E7QZMX3R8V0N5T9WYC1", Title: "locked", Stage: board.StageLocked, CreatedAt: "2026-08-26T09:14:03Z"}
	data, _ = EncodeCard(CardFile{Card: explicit})
	if !bytes.Contains(data, []byte("stage: locked")) {
		t.Fatalf("an explicit stage must be written:\n%s", data)
	}
}

// The notes block is a list: one item per note, id first, then the
// timestamp in brackets, the author, an em dash, the text; continuation
// lines are indented two spaces. A note with no author has no author token.
func TestCardFileNotesFormat(t *testing.T) {
	data, _ := EncodeCard(sample())
	want := "- 01JB4K9P2R5T7VXY8Z0A1B2C3D [2026-08-26T10:02:11Z] kitsunoff — reproduced on board 37\n" +
		"- 01JB4K9P2R5T7VXY8Z0A1B2C3E [2026-08-27T08:00:00Z] — two lines\n  second line\n"
	if !bytes.Contains(data, []byte(want)) {
		t.Fatalf("notes block differs:\n%s", data)
	}
}

// The body keeps its description even when it looks like a notes heading
// of its own: only the last "## Notes" section is ours.
func TestCardFileDescriptionMayMentionNotes(t *testing.T) {
	in := sample()
	in.Card.Description = "See ## Notes below for context.\n\n## Notes\n\nnot ours"
	data, err := EncodeCard(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeCard(in.Card.ItemID, data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Card.Description != in.Card.Description {
		t.Fatalf("description mangled: %q", out.Card.Description)
	}
	if len(out.Card.Notes) != 2 {
		t.Fatalf("notes mis-split: %d", len(out.Card.Notes))
	}
}
