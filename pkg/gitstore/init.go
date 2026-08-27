package gitstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage"
)

// InitBoard bootstraps an empty remote: board.yaml and the no-team group in
// one commit, pushed. A remote that already holds a board is left exactly as
// it is — init is safe to run twice — and a remote that holds something
// that is not a board is refused rather than written over.
func InitBoard(ctx context.Context, s storage.Storer, remote Remote, opts Options, title string) error {
	// A full clone: init is a one-off, and it must read the board it finds
	// to know it is one — not every server speaks shallow to begin with.
	_, err := Clone(ctx, s, remote, opts, 0)
	switch {
	case err == nil:
		r := wrap(s, opts)
		if _, err := Load(r); err != nil {
			return fmt.Errorf("gitstore: remote is not empty and not a board: %w", err)
		}
		return nil // already a board
	case errors.Is(err, ErrEmptyRepository):
		// Unborn: ours to create.
	default:
		return err
	}
	// The failed clone already initialised the storer; point HEAD at the
	// board branch and write the first commit into it.
	r := wrap(s, opts)
	if err := s.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, r.branch)); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	boardFile, err := EncodeBoard(BoardFile{Schema: SchemaVersion, Title: title})
	if err != nil {
		return err
	}
	noTeam, err := EncodeTeam(TeamFile{Rank: "a", Created: now})
	if err != nil {
		return err
	}
	if _, err := r.Commit(Action{Name: "init", Summary: "init board", At: time.Now()}, []FileWrite{
		{Path: BoardPath, Data: boardFile},
		{Path: TeamPath("_"), Data: noTeam},
	}); err != nil {
		return err
	}
	return r.Push(ctx, remote)
}
