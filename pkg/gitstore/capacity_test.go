package gitstore

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/aenix-io/aeman/pkg/board"
)

// A person's capacity is one line of users/<login>.yaml — `capacity: 40`.
// Zero writes no line, so a file that carries something else — a key an
// older server or another writer put there — is unchanged by it.
func TestAPersonsCapacityRoundTripsThroughTheirFile(t *testing.T) {
	data, err := EncodeUser(UserFile{Capacity: 40, Created: "2026-09-08T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "capacity: 40\n") {
		t.Fatalf("the file carries the number:\n%s", data)
	}
	back, err := DecodeUser(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Capacity != 40 {
		t.Fatalf("after the round trip: %+v", back)
	}

	other, err := DecodeUser([]byte("personal: https://github.com/x/personal.git\ncreated: 2026-09-08T10:00:00Z\n"))
	if err != nil || other.Capacity != 0 {
		t.Fatalf("a file without the line decodes with none: %+v, %v", other, err)
	}
	rewritten, err := EncodeUser(other)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rewritten), "capacity") || !strings.Contains(string(rewritten), "personal: https://github.com/x/personal.git") {
		t.Fatalf("no capacity, no line, and the rest as it was:\n%s", rewritten)
	}
}

// A TEAM's points a week are one line of its own file — the whole of its
// capacity block, now that the derived cards-a-week limit beside it is gone.
func TestATeamsPointsRoundTripThroughItsFile(t *testing.T) {
	raw, err := EncodeTeam(TeamFile{
		Name: "portal", Rank: "m", Created: "2026-01-01T00:00:00Z",
		Capacity: board.Capacity{Points: 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "points: 40") {
		t.Fatalf("the file does not carry the points:\n%s", raw)
	}
	back, err := DecodeTeam(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Capacity.Points != 40 {
		t.Fatalf("capacity = %+v, want 40 points a week", back.Capacity)
	}
	// Nobody has said: no block at all, and nothing to read back.
	raw, err = EncodeTeam(TeamFile{Name: "cozy", Rank: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "capacity") {
		t.Fatalf("an unset capacity must not be written:\n%s", raw)
	}
}

// The team file's `capacity:` block is a MAPPING, and the codec used to eat
// it whole: everything in it that was not `points` disappeared on the next
// write to that file — a rename, a sprint pointer, a backlog list, the
// capacity itself. That takes the old `week`/`client`/`internal` with it (an
// old replica's file, or a board this server has not touched since the
// upgrade) and any key an outside writer put there, which the storage
// contract promises to keep.
func TestATeamFileKeepsCapacityKeysItDoesNotKnow(t *testing.T) {
	raw := []byte("name: portal\nrank: b\ncreated: 2026-01-01T00:00:00Z\n" +
		"capacity:\n  week: 12\n  points: 40\n  hours: 30\nmotto: ship it\n")
	f, err := DecodeTeam(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f.Capacity.Points != 40 {
		t.Fatalf("points = %d, want 40", f.Capacity.Points)
	}
	f.Capacity.Points = 25
	out, err := EncodeTeam(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"points: 25", "week: 12", "hours: 30", "motto: ship it"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the rewritten file lost %q:\n%s", want, out)
		}
	}
	// And it still reads back as itself.
	again, err := DecodeTeam(out)
	if err != nil {
		t.Fatal(err)
	}
	if again.Capacity.Points != 25 || again.Name != "portal" {
		t.Fatalf("round trip = %+v", again)
	}
	// A file with only unknown capacity keys keeps the block rather than
	// dropping it because the one key this server knows is zero.
	bare, err := EncodeTeam(TeamFile{Name: "cozy", Rank: "c",
		CapacityExtra: []ExtraField{{Key: "hours", Value: &yaml.Node{Kind: yaml.ScalarNode, Value: "30"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bare), "hours: 30") {
		t.Fatalf("an unknown-only capacity block was dropped:\n%s", bare)
	}
}
