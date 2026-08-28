package board

import (
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
)

// The rank key orders cards, teams, projects, epics, processes, tasks and
// deadlines: a plain string compared bytewise, with room to insert between
// any two neighbours by appending. Moving one thing rewrites one file.

// Between two keys there is always a key, and it sorts between them.
func TestRankBetweenOrders(t *testing.T) {
	cases := [][2]string{
		{"a", "c"},
		{"a", "b"},  // adjacent at one length: must grow
		{"az", "b"}, // the gap is only in a longer suffix
		{"a", "a1"}, // next is a direct extension of prev
		{"0", "1"},
		{"zz", "zzz"},
		{"", "01"}, // before a key that itself starts with the floor digit
	}
	for _, c := range cases {
		got, err := RankBetween(c[0], c[1])
		if err != nil {
			t.Fatalf("RankBetween(%q,%q): %v", c[0], c[1], err)
		}
		if got <= c[0] || got >= c[1] {
			t.Fatalf("RankBetween(%q,%q) = %q, not strictly between", c[0], c[1], got)
		}
	}
}

// An open end means "before everything" or "after everything": the key
// still lands on the right side of the one neighbour there is, and a key
// for an empty list is simply valid.
func TestRankBetweenOpenEnds(t *testing.T) {
	first, err := RankBetween("", "")
	if err != nil || first == "" {
		t.Fatalf("RankBetween(\"\",\"\") = %q, %v", first, err)
	}
	after, err := RankBetween(first, "")
	if err != nil || after <= first {
		t.Fatalf("after the only key: %q (err %v), want > %q", after, err, first)
	}
	before, err := RankBetween("", first)
	if err != nil || before >= first || before == "" {
		t.Fatalf("before the only key: %q (err %v), want non-empty < %q", before, err, first)
	}
}

// Equal or inverted neighbours are a caller bug, not something to paper
// over with a key that sorts wrong.
func TestRankBetweenRefusesEqualOrInverted(t *testing.T) {
	if _, err := RankBetween("m", "m"); err == nil {
		t.Fatal("equal neighbours: want an error")
	}
	if _, err := RankBetween("n", "m"); err == nil {
		t.Fatal("inverted neighbours: want an error")
	}
}

// The invariant that makes "always room" true: no minted key ever ends in
// the floor digit, because nothing can be inserted between "a" and "a0".
func TestRankNeverEndsInFloorDigit(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	keys := []string{}
	for i := 0; i < 2000; i++ {
		lo, hi := "", ""
		if len(keys) > 0 {
			at := rng.IntN(len(keys) + 1)
			if at > 0 {
				lo = keys[at-1]
			}
			if at < len(keys) {
				hi = keys[at]
			}
			k, err := RankBetween(lo, hi)
			if err != nil {
				t.Fatalf("insert %d between %q and %q: %v", i, lo, hi, err)
			}
			if strings.HasSuffix(k, "0") {
				t.Fatalf("key %q ends in the floor digit", k)
			}
			keys = append(keys[:at], append([]string{k}, keys[at:]...)...)
			continue
		}
		k, err := RankBetween(lo, hi)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatal("random inserts broke the order")
	}
}

// The pathological insert — always right after the last key, a thousand
// times — keeps the whole list ordered; the keys grow, they do not collide.
func TestRankBetweenRepeatedInsertsStayOrdered(t *testing.T) {
	keys := []string{}
	lo := ""
	for i := 0; i < 1000; i++ {
		k, err := RankBetween(lo, "")
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		keys = append(keys, k)
		lo = k
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatal("keys are not in insertion order")
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			t.Fatalf("duplicate key at %d: %q", i, keys[i])
		}
	}
	// And the same wedged between two fixed neighbours — the case that
	// actually grows keys, and the one a rebalance exists for.
	lo, hi := "a", "b"
	for i := 0; i < 200; i++ {
		k, err := RankBetween(lo, hi)
		if err != nil {
			t.Fatalf("wedge %d: %v", i, err)
		}
		if k <= lo || k >= hi {
			t.Fatalf("wedge %d: %q not between %q and %q", i, k, lo, hi)
		}
		lo = k
	}
	if !RankTooLong(lo) {
		t.Fatalf("200 wedged inserts should have outgrown the cap, got %d chars", len(lo))
	}
}

// Rebalance hands a run of keys evenly spaced replacements: as many as it
// was given, in the same order, none over the length cap, and strictly
// between the two boundary keys the run sits in (which may be open).
func TestRankRebalanceEvenAndBounded(t *testing.T) {
	got, err := RankRebalance("a", "b", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d keys, want 4", len(got))
	}
	prev := "a"
	for i, k := range got {
		if len(k) > MaxRankLen {
			t.Fatalf("key %d %q over the cap %d", i, k, MaxRankLen)
		}
		if k <= prev || k >= "b" {
			t.Fatalf("key %d %q not between %q and \"b\"", i, k, prev)
		}
		if strings.HasSuffix(k, "0") {
			t.Fatalf("rebalanced key %q ends in the floor digit", k)
		}
		prev = k
	}
	// Open boundaries: the run is the whole list.
	open, err := RankRebalance("", "", 3)
	if err != nil || len(open) != 3 || !sort.StringsAreSorted(open) {
		t.Fatalf("open rebalance: %v %v", open, err)
	}
	// A run wide enough to need a second digit still fits the cap.
	many, err := RankRebalance("a", "b", 100)
	if err != nil || len(many) != 100 || !sort.StringsAreSorted(many) {
		t.Fatalf("100 between a and b: %d keys, %v", len(many), err)
	}
	if _, err := RankRebalance("b", "a", 1); err == nil {
		t.Fatal("inverted bounds: want an error")
	}
}

// A key over the cap is what triggers a rebalance; the predicate is the
// single place that decides it.
func TestRankNeedsRebalance(t *testing.T) {
	if RankTooLong(strings.Repeat("a", MaxRankLen)) {
		t.Fatal("a key AT the cap is fine")
	}
	if !RankTooLong(strings.Repeat("a", MaxRankLen+1)) {
		t.Fatal("a key over the cap must trigger a rebalance")
	}
}
