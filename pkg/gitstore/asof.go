package gitstore

import (
	"errors"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"

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
	head := r.Head()
	if head.IsZero() {
		return Snapshot{}, false, ErrEmptyRepository
	}
	shallow, err := r.shallows()
	if err != nil {
		return Snapshot{}, false, err
	}
	h := head
	for {
		c, err := object.GetCommit(r.s, h)
		if err != nil {
			return Snapshot{}, false, err
		}
		if !c.Committer.When.After(at) {
			s, err := LoadAt(r, h)
			return s, err == nil, err
		}
		// Older than the boundary the clone was cut at: whatever came
		// before is not here to read.
		if shallow[h] {
			return Snapshot{}, false, nil
		}
		if c.NumParents() == 0 {
			// The board's own beginning: every commit is newer than the
			// day asked for, so on that day the board was empty.
			return Snapshot{}, true, nil
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
// the day is remembered for. Every card the day removed is read back from the
// commit that removed it — its last state, done if that is how it went — and
// a card removed and then made again is left to the tree, which has it.
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
	gone, err := cardsRemovedBetween(r, from, to)
	if err != nil {
		return Snapshot{}, false, err
	}
	for _, c := range gone {
		if held[c.ItemID] {
			continue
		}
		held[c.ItemID] = true
		s.Cards = append(s.Cards, c)
	}
	return s, true, nil
}

// cardsRemovedBetween reads every card whose file a commit in (from, to]
// deleted, in the state that commit's parent held — walking the day newest
// first, so a card deleted more than once is read as it went the last time.
func cardsRemovedBetween(r *Repo, from, to time.Time) ([]board.Card, error) {
	var out []board.Card
	seen := map[string]bool{}
	h := r.Head()
	for !h.IsZero() {
		c, err := object.GetCommit(r.s, h)
		if err != nil {
			return nil, err
		}
		if !c.Committer.When.After(from) {
			return out, nil // back at the day's start
		}
		if c.NumParents() == 0 {
			return out, nil
		}
		parent, err := c.Parent(0)
		if err != nil {
			return nil, err
		}
		if !c.Committer.When.After(to) {
			removed, err := cardsRemovedBy(parent, c)
			if err != nil {
				return nil, err
			}
			for _, card := range removed {
				if seen[card.ItemID] {
					continue
				}
				seen[card.ItemID] = true
				out = append(out, card)
			}
		}
		h = c.ParentHashes[0]
	}
	return out, nil
}

// cardsRemovedBy is the cards a commit deleted, read from the tree it was
// made on. Only card files count: a roster entry is not a card, and a
// changed file is not a removed one.
func cardsRemovedBy(parent, c *object.Commit) ([]board.Card, error) {
	before, err := parent.Tree()
	if err != nil {
		return nil, err
	}
	after, err := c.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(before, after)
	if err != nil {
		return nil, err
	}
	var out []board.Card
	for _, ch := range changes {
		action, err := ch.Action()
		if err != nil {
			return nil, err
		}
		if action != merkletrie.Delete {
			continue
		}
		kind, parts := ParsePath(ch.From.Name)
		if kind != PathCard {
			continue
		}
		id := parts[0]
		f, err := before.File(ch.From.Name)
		if err != nil {
			if errors.Is(err, object.ErrFileNotFound) {
				continue
			}
			return nil, err
		}
		data, err := f.Contents()
		if err != nil {
			return nil, err
		}
		card, err := DecodeCard(id, []byte(data))
		if err != nil {
			// A file the decoder cannot read is not a card anyone saw.
			continue
		}
		out = append(out, card.Card)
	}
	return out, nil
}
