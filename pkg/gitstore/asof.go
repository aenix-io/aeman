package gitstore

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// LoadAsOf reads the board as it stood at a past moment: the tree of the
// newest commit made at or before it. Nothing is reconstructed — the storage
// IS the history, so "the board on the 21st" is a tree that once was the
// board, not a replay of events over today's cards.
//
// ok is false when the answer is behind the clone's horizon: a shallow
// boundary older than nothing we hold. Answering such a day with the oldest
// state we happen to have would put a stranger's values on it, so it is
// refused and the caller deepens (or says the history is cut). A day BEFORE
// the board's first commit is not that case: the board existed and was
// empty, and that is an answer.
func LoadAsOf(r *Repo, at time.Time) (Snapshot, bool, error) {
	h, ok, err := commitAsOf(r, at)
	if err != nil || !ok {
		return Snapshot{}, ok, err
	}
	if h.IsZero() {
		// Before the board's first commit: it existed and was empty.
		return Snapshot{}, true, nil
	}
	s, err := LoadAt(r, h)
	return s, err == nil, err
}

// commitAsOf is the newest commit made at or before `at`. A zero hash with ok
// means the board had not begun yet; ok is false when the answer is behind the
// clone's horizon. The walk starts at the closest moment already resolved
// (r.asOfStart), so stepping back through the days costs each day once rather
// than the whole history every time.
func commitAsOf(r *Repo, at time.Time) (plumbing.Hash, bool, error) {
	head := r.Head()
	if head.IsZero() {
		return plumbing.ZeroHash, false, ErrEmptyRepository
	}
	shallow, err := r.shallows()
	if err != nil {
		return plumbing.ZeroHash, false, err
	}
	horizon := horizonOf(shallow)
	start, known := r.asOfStart(at, head, horizon)
	if known != nil {
		return known.hash, known.ok, nil
	}
	h := start
	for {
		c, err := object.GetCommit(r.s, h)
		if err != nil {
			return plumbing.ZeroHash, false, err
		}
		if !c.Committer.When.After(at) {
			r.asOfResolved(at, head, horizon, h, true)
			return h, true, nil
		}
		if shallow[h] {
			r.asOfResolved(at, head, horizon, plumbing.ZeroHash, false)
			return plumbing.ZeroHash, false, nil
		}
		if c.NumParents() == 0 {
			r.asOfResolved(at, head, horizon, plumbing.ZeroHash, true)
			return plumbing.ZeroHash, true, nil
		}
		h = c.ParentHashes[0]
	}
}

// LoadAsOfDay is the board a DAY ended with, plus the cards the day itself
// removed — each in the state it was in when it went.
//
// A day is everything that stood on it. The × takes a card off the board and
// the file goes, so a card worked and tidied away the same day is absent from
// the tree the day ended with; reading only that tree loses exactly the work
// the day is remembered for.
//
// What the day removed is read from what is already written down rather than
// from comparing trees: every commit NAMES the cards it touched (the
// Aeman-Cards trailer), and the two snapshots the day is bounded by say what
// it began and ended with. Only the few cards that actually went are looked
// up in a tree. The first version diffed every commit of the day against its
// parent, which on a board of 2400 cards and three hundred commits a day cost
// 75 seconds per request — the board hung on every past day.
//
// `from` is the previous day's last moment and `to` this one's. ok follows
// LoadAsOf: false when the day is behind the clone's horizon.
func LoadAsOfDay(r *Repo, from, to time.Time) (Snapshot, bool, error) {
	s, ok, err := LoadAsOf(r, to)
	if err != nil || !ok {
		return s, ok, err
	}
	held := make(map[string]bool, len(s.Cards))
	for _, c := range s.Cards {
		held[c.ItemID] = true
	}
	gone, err := cardsRemovedBetween(r, from, to, held)
	if err != nil {
		return Snapshot{}, false, err
	}
	s.Cards = append(s.Cards, gone...)
	return s, true, nil
}

// cardsRemovedBetween reads every card that stood on the day and is not on
// its last tree — the ones the day removed — in the state each was in when it
// went. `held` is what the day ended with.
func cardsRemovedBetween(r *Repo, from, to time.Time, held map[string]bool) ([]board.Card, error) {
	day, touched, err := commitsOfDay(r, from, to)
	if err != nil {
		return nil, err
	}
	// What the day BEGAN with: a writer that leaves no trailers still shows
	// up here, as a card present at the start and absent at the end. Only the
	// IDS are wanted, and a card's id is in its path — so the day's first tree
	// is listed, not parsed: reading 2400 card files to learn which ones exist
	// cost more than the rest of the day put together.
	began, ok, err := commitAsOf(r, from)
	if err != nil {
		return nil, err
	}
	start := map[string]bool{}
	if ok && !began.IsZero() {
		start, err = cardIDsAt(r, began)
		if err != nil {
			return nil, err
		}
	}

	ids := make([]string, 0, len(touched))
	for id := range touched {
		if !held[id] {
			ids = append(ids, id)
		}
	}
	for id := range start {
		if _, named := touched[id]; !held[id] && !named {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids) // a stable answer; the caller sorts by rank anyway

	out := make([]board.Card, 0, len(ids))
	for _, id := range ids {
		c, found, err := lastStateInDay(id, day, touched[id])
		if err != nil {
			return nil, err
		}
		if !found {
			// Never written during the day: it went with the day's first
			// commit, and the state it went in is the one the day began with.
			if start[id] {
				was, ok, err := cardAt(r, began, id)
				if err != nil {
					return nil, err
				}
				if ok {
					out = append(out, was)
				}
			}
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// horizonOf names a clone's shallow boundary — what it can and cannot reach.
func horizonOf(shallow map[plumbing.Hash]bool) string {
	if len(shallow) == 0 {
		return ""
	}
	hs := make([]string, 0, len(shallow))
	for h := range shallow {
		hs = append(hs, h.String())
	}
	sort.Strings(hs)
	return strings.Join(hs, ",")
}

// cardIDsAt lists the cards a commit's tree holds, by their paths alone: a
// card's id IS its file name, so nothing is read but the trees.
func cardIDsAt(r *Repo, h plumbing.Hash) (map[string]bool, error) {
	c, err := object.GetCommit(r.s, h)
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	err = tree.Files().ForEach(func(f *object.File) error {
		if kind, parts := ParsePath(f.Name); kind == PathCard {
			out[parts[0]] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// cardAt reads one card from a commit's tree.
func cardAt(r *Repo, h plumbing.Hash, id string) (board.Card, bool, error) {
	p, err := CardPath(id)
	if err != nil {
		return board.Card{}, false, err
	}
	c, err := object.GetCommit(r.s, h)
	if err != nil {
		return board.Card{}, false, err
	}
	f, err := c.File(p)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
			return board.Card{}, false, nil
		}
		return board.Card{}, false, err
	}
	data, err := f.Contents()
	if err != nil {
		return board.Card{}, false, err
	}
	card, err := DecodeCard(id, []byte(data))
	if err != nil {
		return board.Card{}, false, nil //nolint:nilerr // not a card anyone saw
	}
	return card.Card, true, nil
}

// commitsOfDay is the day's commits, newest first, and the cards they name:
// every commit says which cards it touched (the Aeman-Cards trailer), so the
// ones that left are found without opening a single tree.
func commitsOfDay(r *Repo, from, to time.Time) ([]*object.Commit, map[string]int, error) {
	var day []*object.Commit
	touched := map[string]int{}
	h := r.Head()
	for !h.IsZero() {
		c, err := object.GetCommit(r.s, h)
		if err != nil {
			return nil, nil, err
		}
		if !c.Committer.When.After(from) {
			break // back at the day's start
		}
		if !c.Committer.When.After(to) {
			for _, id := range ParseTrailers(c.Message).Cards {
				if _, seen := touched[id]; !seen {
					touched[id] = len(day)
				}
			}
			day = append(day, c)
		}
		if c.NumParents() == 0 {
			break
		}
		h = c.ParentHashes[0]
	}
	return day, touched, nil
}

// lastStateInDay reads a card's last state inside the day: the newest commit
// of it whose tree still holds the file. The search starts at the commit that
// last NAMED the card — which is the one that removed it, whose own tree no
// longer has it and whose neighbour below does — so it opens a tree or two
// rather than the day's worth.
func lastStateInDay(id string, day []*object.Commit, at int) (board.Card, bool, error) {
	p, err := CardPath(id)
	if err != nil {
		return board.Card{}, false, err
	}
	for i := at; i < len(day); i++ {
		f, err := day[i].File(p)
		if err != nil {
			if errors.Is(err, object.ErrFileNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
				continue
			}
			return board.Card{}, false, err
		}
		data, err := f.Contents()
		if err != nil {
			return board.Card{}, false, err
		}
		card, err := DecodeCard(id, []byte(data))
		if err != nil {
			// A file the decoder cannot read is not a card anyone saw; the
			// day answers without it rather than failing over one bad file.
			return board.Card{}, false, nil //nolint:nilerr // deliberate
		}
		return card.Card, true, nil
	}
	return board.Card{}, false, nil
}
