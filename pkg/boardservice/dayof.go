package boardservice

import (
	"context"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// BoardOfDay is the board a reader of `day` must see, whichever door they
// came through — the HTTP API, an agent over MCP, an embedder. There is ONE
// answer to "what did that day look like", and it is here rather than in each
// caller: two copies of it drifted apart the moment one of them was written.
//
// A day that is today or later, or one no team has moved past, is the live
// board: `over` is empty and `at` is zero. Otherwise the answer is one day on
// one screen with two moments in it (G60) — the teams the day is over for
// contribute what they held that evening, everyone else what they hold now —
// and `over` names those teams, which is what marks the records in a listing
// (apiserver.MarkRecords) and what gives back the cards their × took off
// (Selector.RecordTeams).
func (s *Service) BoardOfDay(ctx context.Context, boardID, day string) (bd board.Board, over map[string]bool, at time.Time, err error) {
	live, err := s.Board(ctx, boardID)
	if err != nil || day == "" || day >= board.TodayIso() {
		return live, nil, time.Time{}, err
	}
	over = board.TeamsPast(live, day)
	if len(over) == 0 {
		return live, nil, time.Time{}, nil
	}
	if at, err = board.EndOfDay(day); err != nil {
		return board.Board{}, nil, time.Time{}, err
	}
	then, err := s.BoardAsOf(ctx, boardID, at)
	if err != nil {
		return board.Board{}, nil, time.Time{}, err
	}
	bd, _ = board.MergeAsOf(live, then, over)
	return bd, over, at, nil
}
