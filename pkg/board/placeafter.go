package board

import (
	"errors"
	"fmt"
	"sort"
)

// Ranked is one entry of an ordered list: an id and its key.
type Ranked struct {
	ID   string
	Rank string
	// Fixed is an entry a rebalance window must not re-key (Board.Pinned):
	// the window stops at it, and it bounds the run instead.
	Fixed bool
}

// RankChange is one key an ordering action rewrites: the card, the key it
// had and the key it gets. An action's changes are its whole plan
// (docs/design/ordering.md).
type RankChange struct {
	ID     string `json:"uid"`
	Before string `json:"before,omitempty"`
	After  string `json:"after"`
}

// SortRanked puts a list in board order: by key, ties by id.
func SortRanked(list []Ranked) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Rank != list[j].Rank {
			return list[i].Rank < list[j].Rank
		}
		return list[i].ID < list[j].ID
	})
}

// RankedOf is a board's cards as a ranked list, in the board's order, the
// teams' state cards left out (they order the roster, not the board).
func RankedOf(cards []Card) []Ranked {
	out := make([]Ranked, 0, len(cards))
	for _, c := range cards {
		if IsStateTitle(c.Title) {
			continue
		}
		out = append(out, Ranked{ID: c.ItemID, Rank: c.Rank})
	}
	return out
}

// RankedOfBoard is RankedOf with the board's pinned cards marked Fixed.
func RankedOfBoard(b Board) []Ranked {
	out := RankedOf(b.Cards)
	if len(b.Pinned) > 0 {
		for i := range out {
			out[i].Fixed = b.Pinned[out[i].ID]
		}
	}
	return out
}

// ErrNoRoom is a place with no key to give: the gap is exhausted and no
// window around it can be renumbered — the whole list, or as far as the
// cards the mover may not write let it reach.
var ErrNoRoom = errors.New("rank: no room to place the card")

// PlaceAfter is the keys that put id right after afterID in list ("" = the
// top; an anchor the list does not hold = the end). With room between the
// two neighbours it is ONE key, the mover's (G12). When the gap is
// exhausted — the key would outgrow MaxRankLen — or the neighbours are
// tied or inverted (two writers picked one key), a WINDOW of neighbours
// around the place is renumbered with evenly spaced keys between the
// nearest entries outside it, grown until the keys fit: a bounded run,
// never the whole list unless the list is the window. Only keys that
// change are returned. list is in board order and may hold id.
func PlaceAfter(list []Ranked, id, afterID string) ([]RankChange, error) {
	var mover Ranked
	found := false
	rest := make([]Ranked, 0, len(list))
	for _, r := range list {
		if r.ID == id {
			mover, found = r, true
			continue
		}
		if !ValidRank(r.Rank) {
			continue // an unplaced card is no neighbour: normalisation places it
		}
		rest = append(rest, r)
	}
	if !found {
		mover = Ranked{ID: id}
	}
	at := 0
	if afterID != "" {
		at = len(rest)
		for i, r := range rest {
			if r.ID == afterID {
				at = i + 1
				break
			}
		}
	}
	return placeAt(rest, []Ranked{mover}, at)
}

// PlaceAt is PlaceAfter by position: the mover lands at index at of rest
// (the list without it).
func PlaceAt(rest []Ranked, mover Ranked, at int) ([]RankChange, error) {
	return PlaceRunAt(rest, []Ranked{mover}, at)
}

// PlaceRunAt puts a RUN of movers, in the order given, at index at of rest
// (the list without them): evenly spaced keys between the two neighbours
// when they fit, else a bounded window renumbered around the place, as
// PlaceAfter does for one. A run already in place writes nothing.
func PlaceRunAt(rest []Ranked, movers []Ranked, at int) ([]RankChange, error) {
	if at < 0 {
		at = 0
	}
	if at > len(rest) {
		at = len(rest)
	}
	if len(movers) == 0 {
		return nil, nil
	}
	return placeAt(rest, movers, at)
}

func placeAt(rest []Ranked, movers []Ranked, at int) ([]RankChange, error) {
	prev, next := "", ""
	if at > 0 {
		prev = rest[at-1].Rank
	}
	if at < len(rest) {
		next = rest[at].Rank
	}
	if validNeighbours(prev, next, at > 0, at < len(rest)) {
		if inPlace(movers, prev, next) {
			return nil, nil
		}
		if len(movers) == 1 {
			if k, err := RankBetween(prev, next); err == nil && !RankTooLong(k) {
				return []RankChange{{ID: movers[0].ID, Before: movers[0].Rank, After: k}}, nil
			}
		} else if keys, err := RankRebalance(prev, next, len(movers)); err == nil && shortEnough(keys) {
			return changesFor(movers, keys), nil
		}
	}
	// The window: w entries on each side of the place, the movers between —
	// never past an entry that is Fixed (one the mover may not write): that
	// one bounds the run instead, and a gap with no room short of it is
	// refused rather than reaching into somebody else's repository.
	minLo, maxHi := 0, len(rest)
	for i := at - 1; i >= 0; i-- {
		if rest[i].Fixed {
			minLo = i + 1
			break
		}
	}
	for i := at; i < len(rest); i++ {
		if rest[i].Fixed {
			maxHi = i
			break
		}
	}
	for w := 1; ; w *= 2 {
		lo, hi := at-w, at+w
		if lo < minLo {
			lo = minLo
		}
		if hi > maxHi {
			hi = maxHi
		}
		whole := lo == minLo && hi == maxHi
		floor, ceil := "", ""
		if lo > 0 {
			floor = rest[lo-1].Rank
		}
		if hi < len(rest) {
			ceil = rest[hi].Rank
		}
		run := make([]Ranked, 0, hi-lo+len(movers))
		run = append(run, rest[lo:at]...)
		run = append(run, movers...)
		run = append(run, rest[at:hi]...)
		if lo > 0 && !ValidRank(floor) || hi < len(rest) && !ValidRank(ceil) || ceil != "" && floor >= ceil {
			if whole {
				return nil, ErrNoRoom
			}
			continue // the bounds themselves are tied or blank: widen
		}
		keys, err := RankRebalance(floor, ceil, len(run))
		if err == nil && (whole && !RankTooLong(longest(keys)) || shortEnough(keys)) {
			return changesFor(run, keys), nil
		}
		if whole {
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrNoRoom, err)
			}
			return nil, ErrNoRoom
		}
	}
}

// longest is the longest of some keys.
func longest(keys []string) string {
	out := ""
	for _, k := range keys {
		if len(k) > len(out) {
			out = k
		}
	}
	return out
}

// inPlace reports a run whose keys already stand, in order, strictly
// between prev and next: nothing to write.
func inPlace(movers []Ranked, prev, next string) bool {
	last := prev
	for i, m := range movers {
		if !ValidRank(m.Rank) || (i > 0 || prev != "") && m.Rank <= last || next != "" && m.Rank >= next {
			return false
		}
		last = m.Rank
	}
	return true
}

// changesFor pairs a run with its new keys, keeping only the keys that
// change.
func changesFor(run []Ranked, keys []string) []RankChange {
	var out []RankChange
	for i, r := range run {
		if r.Rank != keys[i] {
			out = append(out, RankChange{ID: r.ID, Before: r.Rank, After: keys[i]})
		}
	}
	return out
}

// validNeighbours reports whether a key can go between prev and next: both
// usable keys (a present neighbour with no key is no bound) in order.
func validNeighbours(prev, next string, hasPrev, hasNext bool) bool {
	if hasPrev && !ValidRank(prev) || hasNext && !ValidRank(next) {
		return false
	}
	return next == "" || prev < next
}

// shortEnough is a window's keys with headroom left: half the cap, so the
// next inserts there do not exhaust it again at once.
func shortEnough(keys []string) bool {
	for _, k := range keys {
		if len(k) > MaxRankLen/2 {
			return false
		}
	}
	return true
}

// ValidRank reports whether a key is one this server could have minted: a
// non-empty run of the alphabet, not ending in the floor digit, within the
// cap. An invalid key is no key (docs/design/ordering.md, normalisation).
func ValidRank(k string) bool {
	if k == "" || len(k) > MaxRankLen || k[len(k)-1] == rankAlphabet[0] {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// Planner computes an ordering action's keys over a board: the board is
// the MERGED board as it stands at the moment of the write, its cards in
// board order. A backend runs it and writes what it returns — the plan is
// decided in one place, over the state it applies to (ordering.md).
type Planner func(b Board) ([]RankChange, error)

// Fixed is a planner that returns changes already decided — the write of a
// plan computed elsewhere (the server's cache plans; its queued write
// applies the same keys).
func Fixed(changes []RankChange) Planner {
	return func(Board) ([]RankChange, error) { return changes, nil }
}
