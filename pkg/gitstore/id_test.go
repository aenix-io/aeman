package gitstore

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// Ids are ULIDs: 26 Crockford-base32 characters, a millisecond timestamp
// first so ids minted later sort later, random bits after. The server mints
// them; nothing forge-specific is in them.

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func TestNewIDShape(t *testing.T) {
	id := NewID(time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC))
	if len(id) != 26 {
		t.Fatalf("len = %d, want 26: %q", len(id), id)
	}
	for _, c := range id {
		if !strings.ContainsRune(crockford, c) {
			t.Fatalf("%q is not Crockford base32: %q", c, id)
		}
	}
	if _, err := CardPath(id); err != nil {
		t.Fatal(err)
	}
}

// Later means larger: the time prefix orders ids across calls, and within
// one millisecond two ids still differ.
func TestNewIDOrdersByTime(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	ids := []string{}
	for i := 0; i < 500; i++ {
		ids = append(ids, NewID(base.Add(time.Duration(i)*time.Millisecond)))
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatal("ids minted at increasing times do not sort in time order")
	}
	same := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID(base)
		if same[id] {
			t.Fatalf("duplicate id within one millisecond: %s", id)
		}
		same[id] = true
	}
}

// A derived id is a function of its inputs — the migration derives ids
// from GitHub item ids so a re-run is byte-identical, the sweep derives an
// iteration's id from (task, week) so two replicas write the same path.
func TestDeriveIDIsDeterministic(t *testing.T) {
	when := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	a := DeriveID(when, "task", "01JB4K2E7QZMX3R8V0N5T9WYC1", "2026-08-24")
	b := DeriveID(when, "task", "01JB4K2E7QZMX3R8V0N5T9WYC1", "2026-08-24")
	c := DeriveID(when, "task", "01JB4K2E7QZMX3R8V0N5T9WYC1", "2026-08-31")
	if a != b {
		t.Fatalf("same inputs, different ids: %s %s", a, b)
	}
	if a == c {
		t.Fatalf("different inputs, same id: %s", a)
	}
	if len(a) != 26 {
		t.Fatalf("derived id has %d chars", len(a))
	}
	// The time part is the given time, so a derived id sorts with the
	// minted ids of its moment.
	minted := NewID(when)
	if a[:10] != minted[:10] {
		t.Fatalf("time prefix differs: derived %s, minted %s", a[:10], minted[:10])
	}
	// The namespace keeps unrelated derivations apart even with equal keys.
	if DeriveID(when, "card", "x") == DeriveID(when, "note", "x") {
		t.Fatal("namespaces collide")
	}
}

// The time can be read back — the log and the sweep both need "when was
// this minted" without another field.
func TestIDTime(t *testing.T) {
	when := time.Date(2026, 8, 27, 12, 34, 56, 789_000_000, time.UTC)
	got, err := IDTime(NewID(when))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(when) {
		t.Fatalf("IDTime = %v, want %v", got, when)
	}
	if _, err := IDTime("not-an-id"); err == nil {
		t.Fatal("garbage must not parse as a time")
	}
}
