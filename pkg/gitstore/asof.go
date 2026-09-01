package gitstore

import (
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"
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
