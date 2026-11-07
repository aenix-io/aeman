package gitstore

import (
	"bytes"
	"errors"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// The roster — teams, customers, users — is configuration that
// used to hide in state cards. Here each is a file: a
// name where it has one, a rank, a created time, and the fields the domain
// gives it. Every path carries identity only.

func TestRosterPaths(t *testing.T) {
	cases := map[string]string{
		TeamPath("01JB4TEAM"): "teams/01JB4TEAM.yaml",
		BoardPath:             "board.yaml",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("path %q, want %q", got, want)
		}
	}
}

// ParsePath is the inverse: from a changed path (a remote commit's diff)
// to what it is, so the cache reloads exactly the touched object.
func TestParsePathClassifies(t *testing.T) {
	cases := []struct {
		path string
		kind PathKind
		ids  []string
	}{
		{"cards/c/1/01JB4K2E7QZMX3R8V0N5T9WYC1.md", PathCard, []string{"01JB4K2E7QZMX3R8V0N5T9WYC1"}},
		{"teams/01JB4TEAM.yaml", PathTeam, []string{"01JB4TEAM"}},
		{"teams/_.yaml", PathTeam, []string{"_"}},
		// The retired Project board's roster (H15): one kind, the whole
		// projects/ tree, under any of the names it was ever written with.
		{"projects/01JB4PROJ/project.yaml", PathProjectBoard, nil},
		{"projects/01JB4PROJ/scopes/01JB4SCOPE.yaml", PathProjectBoard, nil},
		{"projects/01JB4PROJ/epics/01JB4SCOPE.yaml", PathProjectBoard, nil},
		{"projects/01JB4PROJ/deadlines/01JB4DL.yaml", PathProjectBoard, nil},
		{"projects", PathUnknown, nil},
		{"board.yaml", PathBoard, nil},
		{"README.md", PathUnknown, nil},
		// The retired Process feature's tree (schema 1) is not of the layout
		// any more: a pre-migration record names it as unknown and moves on.
		{"processes/01JB4PROC/process.yaml", PathUnknown, nil},
		{"processes/01JB4PROC/tasks/01JB4TASK.md", PathUnknown, nil},
		{"cards/c/1/notes.txt", PathUnknown, nil},
		{"cards/c/01JB4K2E7QZMX3R8V0N5T9WYC1.md", PathUnknown, nil}, // one level short
	}
	for _, c := range cases {
		kind, ids := ParsePath(c.path)
		if kind != c.kind {
			t.Fatalf("%s: kind %v, want %v", c.path, kind, c.kind)
		}
		if len(ids) != len(c.ids) {
			t.Fatalf("%s: ids %v, want %v", c.path, ids, c.ids)
		}
		for i := range ids {
			if ids[i] != c.ids[i] {
				t.Fatalf("%s: ids %v, want %v", c.path, ids, c.ids)
			}
		}
	}
}

func TestTeamFileRoundTrip(t *testing.T) {
	in := TeamFile{Name: "portal", Rank: "a0", Created: "2026-06-01T08:00:00Z", Sprint: SprintPointer{Current: "2026-08-24", Previous: "2026-08-21"}}
	data, err := EncodeTeam(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "name: portal\nrank: a0\ncreated: 2026-06-01T08:00:00Z\nsprint:\n  current: 2026-08-24\n  previous: 2026-08-21\n"
	if string(data) != want {
		t.Fatalf("team file:\n%s\nwant:\n%s", data, want)
	}
	out, err := DecodeTeam(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Rank != in.Rank || out.Sprint != in.Sprint || out.Created != in.Created {
		t.Fatalf("round trip: %+v", out)
	}
	// The no-team group: no name, no sprint yet — nothing but rank and created.
	bare, _ := EncodeTeam(TeamFile{Rank: "a", Created: "2026-06-01T08:00:00Z"})
	if bytes.Contains(bare, []byte("name:")) || bytes.Contains(bare, []byte("sprint:")) {
		t.Fatalf("empty fields written:\n%s", bare)
	}
}

// board.yaml carries the layout version. A newer schema than this server
// knows is refused outright; an older (or missing) one is reported so the
// server can migrate it in a commit.
func TestBoardFileSchema(t *testing.T) {
	data, err := EncodeBoard(BoardFile{Schema: SchemaVersion, Title: "aeman board"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "schema: 3\ntitle: aeman board\n" {
		t.Fatalf("board file:\n%s", data)
	}
	b, err := DecodeBoard(data)
	if err != nil || b.Title != "aeman board" || b.Schema != SchemaVersion {
		t.Fatalf("decode: %+v %v", b, err)
	}
	if _, err := DecodeBoard([]byte("schema: 99\ntitle: future\n")); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("newer schema: err = %v, want ErrSchemaNewer", err)
	}
	old, err := DecodeBoard([]byte("title: legacy\n"))
	if err != nil || old.Schema != 0 {
		t.Fatalf("missing schema must decode as 0 for migration: %+v %v", old, err)
	}
}

// The same rule as for cards: a key this server does not know survives a
// rewrite untouched.
func TestRosterKeepsUnknownKeys(t *testing.T) {
	in := []byte("name: portal\nrank: a0\nfutureKey: keep me\ncreated: 2026-06-01T08:00:00Z\n")
	tf, err := DecodeTeam(in)
	if err != nil {
		t.Fatal(err)
	}
	tf.Rank = "a1"
	out, err := EncodeTeam(tf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("futureKey: keep me\n")) || !bytes.Contains(out, []byte("rank: a1\n")) {
		t.Fatalf("rewrite lost something:\n%s", out)
	}
}

// A card's own place on the shelf survives its file: a card that lost `parked`
// would come back in the strip, asking again for a week somebody had already
// answered.
func TestAParkedCardRoundTripsThroughItsFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		card board.Card
	}{
		{"on the shelf", board.Card{ItemID: "c1", Title: "A card", Team: "alpha", Parked: true}},
		{"not parked at all", board.Card{ItemID: "c1", Title: "A card", Team: "alpha"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := EncodeCard(CardFile{Card: tc.card})
			if err != nil {
				t.Fatal(err)
			}
			out, err := DecodeCard(tc.card.ItemID, data)
			if err != nil {
				t.Fatal(err)
			}
			if out.Card.Parked != tc.card.Parked {
				t.Fatalf("parked = %t, want %t", out.Card.Parked, tc.card.Parked)
			}
		})
	}
}
