package board

import (
	"errors"
	"fmt"
	"strings"
)

// The rank key orders whatever sits in a list — cards, teams, projects,
// epics, processes, tasks, deadlines — as a plain string compared bytewise.
// Between any two keys there is always room for another by appending, so a
// move rewrites ONE file and never renumbers its neighbours. When repeated
// inserts at one spot have grown a run of keys past MaxRankLen, the run is
// rebalanced: evenly spaced keys between the nearest neighbours that have
// room, in the same commit as the move that tipped it.
//
// One invariant makes "always room" true: a key never ends in the floor
// digit '0'. Bytewise, nothing fits between "a" and "a0"; because "a0" is
// never minted, that gap never has to be filled.

// rankAlphabet is the key alphabet, in byte order so string comparison and
// digit order agree. Base 36 keeps keys short and readable in a diff.
const rankAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// MaxRankLen is the length past which a key asks for a rebalance.
const MaxRankLen = 32

// ErrRankOrder is returned when the neighbours handed in are not in order.
var ErrRankOrder = errors.New("rank: neighbours are equal or inverted")

// RankBetween returns a key strictly between prev and next. An empty prev
// means "before everything", an empty next "after everything"; both empty
// gives the first key of a list. It never fails on ordered input: when the
// two are adjacent at their length, the key grows by a character.
func RankBetween(prev, next string) (string, error) {
	if next != "" && prev >= next {
		return "", fmt.Errorf("%w: %q %q", ErrRankOrder, prev, next)
	}
	var b strings.Builder
	for i := 0; ; i++ {
		lo := digitAt(prev, i, 0)                 // prev's digit, the floor once exhausted
		hi := digitAt(next, i, len(rankAlphabet)) // next's digit, the ceiling once exhausted
		switch {
		case hi-lo > 1:
			// Room in the middle at this position; the midpoint is never
			// the floor digit because hi ≥ lo+2.
			b.WriteByte(rankAlphabet[(lo+hi)/2])
			return b.String(), nil
		case hi-lo == 1:
			// Adjacent digits: take prev's, which puts the key below next
			// whatever follows, then grow past prev's tail.
			b.WriteByte(rankAlphabet[lo])
			b.WriteString(afterTail(prev, i+1))
			return b.String(), nil
		default:
			// Equal digits: copy and look further along.
			b.WriteByte(rankAlphabet[lo])
		}
	}
}

// digitAt returns the alphabet index of s[i], or def past the end.
func digitAt(s string, i, def int) int {
	if i >= len(s) {
		return def
	}
	return strings.IndexByte(rankAlphabet, s[i])
}

// afterTail returns a suffix that sorts strictly after prev[from:] while
// staying as short as it can: copy prev's remaining digits until one can
// be stepped up (to the midpoint between it and the ceiling, never the
// floor), or append a middle digit once prev is exhausted.
func afterTail(prev string, from int) string {
	var b strings.Builder
	for i := from; ; i++ {
		if i >= len(prev) {
			b.WriteByte(rankAlphabet[len(rankAlphabet)/2])
			return b.String()
		}
		d := strings.IndexByte(rankAlphabet, prev[i])
		if d < len(rankAlphabet)-1 {
			b.WriteByte(rankAlphabet[(d+len(rankAlphabet))/2])
			return b.String()
		}
		b.WriteByte(prev[i])
	}
}

// RankTooLong reports whether a key has outgrown the cap and its run should
// be rebalanced.
func RankTooLong(key string) bool {
	return len(key) > MaxRankLen
}

// RankRebalance returns n keys, evenly spaced, strictly between lo and hi
// (either may be empty for an open end) and none longer than MaxRankLen.
// The caller assigns them to the run's members in their current order.
func RankRebalance(lo, hi string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	if hi != "" && lo >= hi {
		return nil, fmt.Errorf("%w: %q %q", ErrRankOrder, lo, hi)
	}
	count := uint64(n) //nolint:gosec // n > 0 was checked above
	// The shortest width at which lo and hi leave n+1 gaps between them.
	for width := 1; width <= MaxRankLen; width++ {
		l := rankValue(lo, width)
		h := rankCeiling(hi, width)
		if h-l > count {
			step := (h - l) / (count + 1)
			out := make([]string, 0, n)
			for i := uint64(1); i <= count; i++ {
				out = append(out, rankString(l+step*i, width))
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("rank: no room for %d keys between %q and %q within %d chars", n, lo, hi, MaxRankLen)
}

// rankValue maps a key to its integer at the given width, padded with the
// floor digit; a key longer than width is truncated (rounded down).
func rankValue(key string, width int) uint64 {
	base := uint64(len(rankAlphabet))
	var v uint64
	for i := 0; i < width; i++ {
		v *= base
		if i < len(key) {
			v += uint64(strings.IndexByte(rankAlphabet, key[i])) //nolint:gosec // an alphabet index, never negative
		}
	}
	return v
}

// rankCeiling is the exclusive upper bound at the given width: the key's
// own value (rounded down when longer — conservative), or one past the
// widest key when the end is open.
func rankCeiling(key string, width int) uint64 {
	if key == "" {
		v := uint64(1)
		for i := 0; i < width; i++ {
			v *= uint64(len(rankAlphabet))
		}
		return v
	}
	return rankValue(key, width)
}

// rankString renders an integer as a width-digit key with trailing floor
// digits trimmed, so keys stay as short as their order allows and never
// end in '0'.
func rankString(v uint64, width int) string {
	base := uint64(len(rankAlphabet))
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = rankAlphabet[v%base]
		v /= base
	}
	s := strings.TrimRight(string(buf), rankAlphabet[:1])
	if s == "" {
		return string(buf[:1])
	}
	return s
}
