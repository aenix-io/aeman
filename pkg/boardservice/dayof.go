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
// board: `records` is empty and `at` is zero. Otherwise the answer is one day
// on one screen with two moments in it (G60) — the teams the day is over for
// contribute what they held that evening, everyone else what they hold now.
//
// `records` names the cards that came FROM that evening, by id. Every reader
// takes it from here rather than deriving it again from team names: a card
// that moved between teams since, and every card of a team renamed since,
// carry one team in the evening's copy and another in today's, and the second
// derivation loses them — they come through live and are refused by every
// write door. It is what marks a listing (apiserver.MarkRecords), what gives
// back the cards the × took off (Selector.RecordCards), and what the write
// guard refuses.
func (s *Service) BoardOfDay(ctx context.Context, boardID, day string) (bd board.Board, records map[string]bool, at time.Time, err error) {
	live, err := s.Board(ctx, boardID)
	if err != nil || day == "" || day >= board.TodayIso() {
		return live, nil, time.Time{}, err
	}
	over := board.TeamsPast(live, day)
	if len(over) == 0 {
		return live, nil, time.Time{}, nil
	}
	if at, err = board.EndOfDay(day); err != nil {
		return board.Board{}, nil, time.Time{}, err
	}
	then, err := s.boardOfDayTree(ctx, boardID, day, at)
	if err != nil {
		return board.Board{}, nil, time.Time{}, err
	}
	bd, records = board.MergeAsOf(live, then, over)
	return bd, records, at, nil
}
