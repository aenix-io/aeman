package gitstore

import (
	"strings"
	"testing"
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
