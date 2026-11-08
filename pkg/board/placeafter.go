package board

import (
	"errors"
	"sort"
)

// Ranked is one entry of an ordered list: an id and its key.
type Ranked struct {
	ID   string
	Rank string
}

// RankChange is one key an ordering action rewrites: the card, the key it
// had and the key it gets. An action's changes are its whole plan — what
// is written, and what an Undo puts back (docs/design/ordering.md).
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

// errNoRoom is a window that could not be renumbered even as the whole list.
var errNoRoom = errors.New("rank: no room to place the card")

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
	return placeAt(rest, mover, at)
}

// PlaceAt is PlaceAfter by position: the mover lands at index at of rest
// (the list without it).
func PlaceAt(rest []Ranked, mover Ranked, at int) ([]RankChange, error) {
	if at < 0 {
		at = 0
	}
	if at > len(rest) {
		at = len(rest)
	}
	return placeAt(rest, mover, at)
}

func placeAt(rest []Ranked, mover Ranked, at int) ([]RankChange, error) {
	prev, next := "", ""
	if at > 0 {
		prev = rest[at-1].Rank
	}
	if at < len(rest) {
		next = rest[at].Rank
	}
	if validNeighbours(prev, next, at > 0, at < len(rest)) {
		if k, err := RankBetween(prev, next); err == nil && !RankTooLong(k) {
			if k == mover.Rank {
				return nil, nil
			}
			return []RankChange{{ID: mover.ID, Before: mover.Rank, After: k}}, nil
		}
	}
	// The window: w entries on each side of the place, the mover between.
	for w := 1; ; w *= 2 {
		lo, hi := at-w, at+w
		if lo < 0 {
			lo = 0
		}
		if hi > len(rest) {
			hi = len(rest)
		}
		whole := lo == 0 && hi == len(rest)
		floor, ceil := "", ""
		if lo > 0 {
			floor = rest[lo-1].Rank
		}
		if hi < len(rest) {
			ceil = rest[hi].Rank
		}
		run := make([]Ranked, 0, hi-lo+1)
		run = append(run, rest[lo:at]...)
		run = append(run, mover)
		run = append(run, rest[at:hi]...)
		if floor == "" && lo > 0 || ceil != "" && floor >= ceil {
			if whole {
				return nil, errNoRoom
			}
			continue // the bounds themselves are tied or blank: widen
		}
		keys, err := RankRebalance(floor, ceil, len(run))
		if err == nil && (whole || shortEnough(keys)) {
			var out []RankChange
			for i, r := range run {
				if r.Rank != keys[i] {
					out = append(out, RankChange{ID: r.ID, Before: r.Rank, After: keys[i]})
				}
			}
			return out, nil
		}
		if whole {
			if err == nil {
				err = errNoRoom
			}
			return nil, err
		}
	}
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
