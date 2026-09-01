package boardservice

import (
	"context"
	"errors"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// AsOfReader is a backend whose storage KEEPS the board's past — git does,
// by being a history rather than by writing one. Backends that hold only the
// present (an embedder's own store, see docs/embedding.md) do not implement
// it, and a past day is then refused rather than answered with today.
//
// ok is false when the moment asked for is behind what the storage holds —
// a shallow clone's horizon — as opposed to before the board began, which is
// an empty board and an answer.
type AsOfReader interface {
	LoadBoardAsOf(ctx context.Context, boardID string, at time.Time) (board.Board, bool, error)
}

// ErrNoHistory is a past day asked of storage that keeps none.
var ErrNoHistory = errors.New("this board's storage keeps no history")

// ErrHistoryTruncated is a past day the storage no longer reaches.
var ErrHistoryTruncated = errors.New("the history does not reach that day")

// BoardAsOf is the board as it stood at a past moment — what the day showed
// then, not today's cards filtered by that day. Every field is the day's own:
// a card done since reads unfinished, a card that moved team reads where it
// stood, a card created since is absent.
func (s *Service) BoardAsOf(ctx context.Context, boardID string, at time.Time) (board.Board, error) {
	r, ok := s.backend.(AsOfReader)
	if !ok {
		return board.Board{}, ErrNoHistory
	}
	bd, ok, err := r.LoadBoardAsOf(ctx, boardID, at)
	if err != nil {
		return board.Board{}, err
	}
	if !ok {
		return board.Board{}, ErrHistoryTruncated
	}
	return bd, nil
}
