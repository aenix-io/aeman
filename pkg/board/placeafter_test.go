package board

import (
	"strings"
	"testing"
)

func ranked(pairs ...string) []Ranked {
	out := make([]Ranked, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Ranked{ID: pairs[i], Rank: pairs[i+1]})
	}
	return out
}

// applyChanges is the list once the changes are written, in key order.
func applyChanges(list []Ranked, changes []RankChange) []string {
	keys := map[string]string{}
	for _, r := range list {
		keys[r.ID] = r.Rank
	}
	for _, c := range changes {
		keys[c.ID] = c.After
	}
	out := make([]Ranked, 0, len(keys))
	for _, r := range list {
		out = append(out, Ranked{ID: r.ID, Rank: keys[r.ID]})
	}
	SortRanked(out)
	ids := make([]string, len(out))
	for i, r := range out {
		ids[i] = r.ID
	}
	return ids
}

// G12 — with room between the neighbours a move writes ONE key, the
// mover's, between them.
func TestPlaceAfterWritesOneKeyWhenThereIsRoom(t *testing.T) {
	list := ranked("a", "b", "c", "d", "x", "f")
	got, err := PlaceAfter(list, "x", "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "x" || got[0].Before != "f" || got[0].After <= "b" || got[0].After >= "d" {
		t.Fatalf("changes = %+v, want x alone between b and d", got)
	}
	if strings.Join(applyChanges(list, got), " ") != "a x c" {
		t.Fatalf("order = %v", applyChanges(list, got))
	}
}

// The top of the list is afterID ""; an anchor the list does not hold is
// its end — never a key from nowhere.
func TestPlaceAfterTheTopAndAnUnknownAnchor(t *testing.T) {
	list := ranked("a", "b", "c", "d", "x", "f")
	top, _ := PlaceAfter(list, "x", "")
	if strings.Join(applyChanges(list, top), " ") != "x a c" {
		t.Fatalf("top = %v", applyChanges(list, top))
	}
	end, _ := PlaceAfter(list, "a", "nobody")
	if strings.Join(applyChanges(list, end), " ") != "c x a" {
		t.Fatalf("unknown anchor = %v", applyChanges(list, end))
	}
}

// Equal neighbours (two writers picked the same key) leave no gap; the
// window around them is renumbered, the ties broken in their board order.
func TestPlaceAfterRenumbersTiedNeighbours(t *testing.T) {
	list := ranked("a", "b", "c", "m", "d", "m", "e", "q", "x", "z")
	got, err := PlaceAfter(list, "x", "c")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(applyChanges(list, got), " ") != "a c x d e" {
		t.Fatalf("order = %v (%+v)", applyChanges(list, got), got)
	}
	for _, c := range got {
		if c.ID == "a" {
			t.Fatalf("the far neighbour was rewritten: %+v", got)
		}
	}
}

// G12 — an exhausted gap renumbers a BOUNDED window, grown until the keys
// fit, never the whole list.
func TestPlaceAfterRebalancesABoundedWindow(t *testing.T) {
	tight := "k" + strings.Repeat("0", 30) + "1"
	var pairs []string
	for _, ch := range "bcdefghij" {
		pairs = append(pairs, "l"+string(ch), string(ch))
	}
	pairs = append(pairs, "k0", "k", "k1", tight)
	for _, ch := range "lmnopqrstu" {
		pairs = append(pairs, "r"+string(ch), string(ch))
	}
	list := ranked(pairs...)
	got, err := PlaceAfter(list, "lb", "k0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 6 {
		t.Fatalf("rewrote %d keys, want a bounded window: %+v", len(got), got)
	}
	for _, c := range got {
		if RankTooLong(c.After) {
			t.Fatalf("key %q still exhausted", c.After)
		}
	}
	order := strings.Join(applyChanges(list, got), " ")
	if !strings.Contains(order, "k0 lb k1") {
		t.Fatalf("order = %s", order)
	}
}
