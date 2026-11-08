package gitstore

import (
	"context"
	"errors"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// UNDO IS DERIVED FROM THE ACTION'S COMMITS (docs/design/ordering.md). An
// ordering action records nothing of its own: its commits carry its
// Aeman-Action-Id and name the cards they touched, and each card file's
// `rank` line at the commit and at its parent IS the change. Within the
// undo window the commits are found by a walk back from the tip of every
// repository.

// UndoWindow is how far back an action can be undone.
const UndoWindow = 24 * time.Hour

// ErrActionNotFound is an action id no commit of the undo window carries.
var ErrActionNotFound = errors.New("gitstore: no such action in the last 24 hours")

// ActionRanks is the rank changes the commits of one action made in this
// repository, newest commit last, and when the action was made; ok is false
// when no commit of the window carries the id.
func (r *Repo) ActionRanks(id string, now time.Time) (changes []board.RankChange, at time.Time, ok bool, err error) {
	if id == "" || r.Head().IsZero() {
		return nil, time.Time{}, false, nil
	}
	var commits []*object.Commit
	err = r.Walk(r.Head(), func(c *object.Commit) (bool, error) {
		if now.Sub(c.Committer.When) > UndoWindow {
			return false, nil
		}
		if ParseTrailers(c.Message).ActionID == id {
			commits = append(commits, c)
		}
		return true, nil
	})
	if err != nil || len(commits) == 0 {
		return nil, time.Time{}, false, err
	}
	at = commits[len(commits)-1].Committer.When
	var out []board.RankChange
	for i := len(commits) - 1; i >= 0; i-- { // oldest first
		c := commits[i]
		var parent *object.Commit
		if c.NumParents() > 0 {
			if parent, err = c.Parent(0); err != nil {
				return nil, time.Time{}, false, err
			}
		}
		for _, cardID := range ParseTrailers(c.Message).Cards {
			p, perr := CardPath(cardID)
			if perr != nil {
				continue
			}
			after, aerr := rankIn(c, cardID, p)
			if aerr != nil {
				return nil, time.Time{}, false, aerr
			}
			before := ""
			if parent != nil {
				if before, err = rankIn(parent, cardID, p); err != nil {
					return nil, time.Time{}, false, err
				}
			}
			if before != after && after != "" {
				out = mergeRank(out, board.RankChange{ID: cardID, Before: before, After: after})
			}
		}
	}
	return out, at, true, nil
}

// rankIn is a card's rank in a commit's tree, "" when the file is not there.
func rankIn(c *object.Commit, id, p string) (string, error) {
	data, err := blobAt(c, p)
	if err != nil || data == nil {
		return "", err
	}
	f, err := DecodeCard(id, data)
	if err != nil {
		return "", nil //nolint:nilerr // an unreadable file has no rank to restore
	}
	return f.Card.Rank, nil
}

// mergeRank folds a later commit's change of a card into the list: one
// entry per card, its first Before and its last After.
func mergeRank(out []board.RankChange, ch board.RankChange) []board.RankChange {
	for i := range out {
		if out[i].ID == ch.ID {
			out[i].After = ch.After
			return out
		}
	}
	return append(out, ch)
}

// ActionRanks is the rank changes of one action on this repository.
func (b *Backend) ActionRanks(_ context.Context, id string) ([]board.RankChange, time.Time, error) {
	changes, at, ok, err := b.repo.ActionRanks(id, b.now())
	if err == nil && !ok {
		err = ErrActionNotFound
	}
	return changes, at, err
}

// ActionRanks is the rank changes of one action across every domain: each
// repository's commits carrying the id.
func (mb *MultiBackend) ActionRanks(_ context.Context, id string) ([]board.RankChange, time.Time, error) {
	var out []board.RankChange
	var at time.Time
	found := false
	for _, d := range mb.domainList() {
		changes, when, ok, err := d.Repo.ActionRanks(id, time.Now())
		if err != nil {
			return nil, time.Time{}, err
		}
		if !ok {
			continue
		}
		found = true
		if at.IsZero() || when.Before(at) {
			at = when
		}
		for _, ch := range changes {
			out = mergeRank(out, ch)
		}
	}
	if !found {
		return nil, time.Time{}, ErrActionNotFound
	}
	return out, at, nil
}
