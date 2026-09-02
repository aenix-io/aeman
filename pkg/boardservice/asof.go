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

// DayReader is storage that can answer for a whole DAY rather than for the
// moment it ended: it gives back the cards the day itself removed, in the
// state they were in when they went. The × takes a finished card off the
// board, so a card worked and tidied away the same day is no longer in the
// tree that day ended with — and that day is exactly when the work happened.
//
// Optional: storage without it (an embedder's own store) is read at the end
// of the day, as before, and simply lacks what the day removed.
type DayReader interface {
	LoadBoardOfDay(ctx context.Context, boardID string, from, to time.Time) (board.Board, bool, error)
}

// ErrNoHistory is a past day asked of storage that keeps none.
var ErrNoHistory = errors.New("this board's storage keeps no history")

// ErrHistoryTruncated is a past day the storage no longer reaches.
var ErrHistoryTruncated = errors.New("the history does not reach that day")

// boardOfDayTree is the tree side of BoardOfDay: the day's own board, from
// storage that knows what a day is when it does, and the day's last moment
// otherwise.
func (s *Service) boardOfDayTree(ctx context.Context, boardID, day string, at time.Time) (board.Board, error) {
	r, ok := s.backend.(DayReader)
	if !ok {
		return s.BoardAsOf(ctx, boardID, at)
	}
	from, err := board.EndOfDay(board.AddDays(day, -1))
	if err != nil {
		return board.Board{}, err
	}
	bd, held, err := r.LoadBoardOfDay(ctx, boardID, from, at)
	if err != nil {
		return board.Board{}, err
	}
	if !held {
		return board.Board{}, ErrHistoryTruncated
	}
	return bd, nil
}

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
