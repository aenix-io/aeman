package gitstore

import (
	"strings"
	"testing"

	"github.com/aenix-io/aeman/pkg/board"
)

// A person's capacity is one line of users/<login>.yaml — `capacity: 40` —
// beside the link to their personal repository, and either may stand
// without the other: most people have a capacity and no personal board, and
// a personal board says nothing about a week's worth of points. Zero writes
// no line, so a file that only ever carried a link is unchanged by it.
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
	if back.Capacity != 40 || back.Personal != "" {
		t.Fatalf("after the round trip: %+v", back)
	}

	linked, err := EncodeUser(UserFile{Personal: "https://github.com/x/personal.git", Created: "2026-09-08T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(linked), "capacity") {
		t.Fatalf("no capacity, no line:\n%s", linked)
	}
	if back, err = DecodeUser(linked); err != nil || back.Capacity != 0 || back.Personal == "" {
		t.Fatalf("a link-only file decodes as before: %+v, %v", back, err)
	}
}

// A TEAM's points a week live beside its cards a week in the team file's
// capacity block — two numbers, two measurements — and an unknown key in that
// block is kept, like every other unknown key in the tree.
func TestATeamsPointsRoundTripThroughItsFile(t *testing.T) {
	raw, err := EncodeTeam(TeamFile{
		Name: "portal", Rank: "m", Created: "2026-01-01T00:00:00Z",
		Capacity: board.Capacity{Week: 12, Points: 40, Client: 30},
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
	if back.Capacity.Points != 40 || back.Capacity.Week != 12 || back.Capacity.Client != 30 {
		t.Fatalf("capacity = %+v, want week 12, points 40, client 30", back.Capacity)
	}
	// Nobody has said: no key, and nothing to read back.
	raw, err = EncodeTeam(TeamFile{Name: "cozy", Rank: "n", Capacity: board.Capacity{Week: 12}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "points:") {
		t.Fatalf("an unset points must not be written:\n%s", raw)
	}
}
