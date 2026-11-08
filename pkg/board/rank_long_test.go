package board

import (
	"math/rand"
	"strings"
	"testing"
)

// longKey is a key of up to maxLen characters with long runs of the top
// digit — the shape a board grows where cards were put at the end again and
// again ("zzzzzzzzzzzzv"), and the one a 64-bit integer cannot hold past 12
// digits.
func longKey(r *rand.Rand, maxLen int) string {
	n := 1 + r.Intn(maxLen)
	var b strings.Builder
	for i := 0; i < n; i++ {
		if r.Intn(3) > 0 {
			b.WriteByte('z')
		} else {
			b.WriteByte(rankAlphabet[r.Intn(len(rankAlphabet))])
		}
	}
	k := strings.TrimRight(b.String(), "0")
	if k == "" {
		return "z"
	}
	return k
}

// G12 — RankRebalance holds at EVERY width up to MaxRankLen: the keys lie
// strictly between the bounds, in order, valid. At 13 digits and more the
// arithmetic used to overflow 64 bits and hand back keys that sorted to the
// front of the board (the reported case below).
func TestRankRebalanceHoldsForLongKeys(t *testing.T) {
	check := func(lo, hi string, n int) {
		t.Helper()
		keys, err := RankRebalance(lo, hi, n)
		if err != nil {
			return // no room at all is an answer; garbage is not
		}
		prev := lo
		for i, k := range keys {
			if !ValidRank(k) {
				t.Fatalf("RankRebalance(%q, %q, %d)[%d] = %q, not a valid key", lo, hi, n, i, k)
			}
			if prev != "" && k <= prev || hi != "" && k >= hi {
				t.Fatalf("RankRebalance(%q, %q, %d)[%d] = %q, not strictly between %q and %q", lo, hi, n, i, k, prev, hi)
			}
			prev = k
		}
	}
	check("zzzzzzzzzzzv", "zzzzzzzzzzzzzzzzzf", 6)
	check("zzzzzzzzzzzzzx", "", 6)
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 5000; i++ {
		a, b := longKey(r, 24), longKey(r, 24)
		if a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		n := 1 + r.Intn(20)
		check(a, b, n)
		check(a, "", n)
		check("", b, n)
	}
}

// RankBetween on long keys stays strictly between them, and repeated
// inserts at the end of a "zzzz…" run keep sorting after it.
func TestRankBetweenHoldsForLongKeys(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 5000; i++ {
		a, b := longKey(r, 24), longKey(r, 24)
		if a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		k, err := RankBetween(a, b)
		if err != nil || k <= a || k >= b || !ValidRank(k) && len(k) <= MaxRankLen {
			t.Fatalf("RankBetween(%q, %q) = %q, %v", a, b, k, err)
		}
	}
	last := "zzzzzzzzzzzzzx"
	for i := 0; i < 50; i++ {
		changes, err := PlaceRunAt([]Ranked{{ID: "a", Rank: "zzzzzzzzzzzzzw"}, {ID: "b", Rank: last}}, []Ranked{{ID: "m"}}, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range changes {
			if ch.ID == "m" && ch.After <= last {
				t.Fatalf("a card put after %q got %q, which sorts before it", last, ch.After)
			}
		}
	}
}
