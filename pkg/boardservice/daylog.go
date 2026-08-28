package boardservice

import (
	"context"
	"fmt"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// The day feed — the notes and events of ONE day across the cards a board
// shows — is the boards' most-asked question and used to be answered a card
// at a time, each answer a full history read. DayLogs answers the whole
// screen at once and only for the day: the notes are already on the cards
// the board holds, and the events come from a walk that stops at the day's
// first moment. A card's own feed (Log) is the other question — one card,
// its whole history — and stays what it was.

// DayLog is what happened to one card on one day.
type DayLog struct {
	Notes  []board.Note
	Events []board.Event
}

// DayLogs answers for every id the visitor's board actually holds; an id it
// does not (unknown, or in a domain they cannot read) is absent from the
// answer rather than an error. A card that was simply quiet that day is
// present and empty — "asked and nothing happened" is not "not asked".
// An empty day means today.
func (s *Service) DayLogs(ctx context.Context, boardID string, uids []string, day string) (map[string]DayLog, error) {
	if len(uids) == 0 {
		return map[string]DayLog{}, nil
	}
	if day == "" {
		day = board.TodayIso()
	}
	since, err := time.ParseInLocation(dayLayout, day, board.Location())
	if err != nil {
		return nil, fmt.Errorf("day %q: %w", day, err)
	}
	b, err := s.backend.LoadBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]DayLog, len(uids))
	for _, id := range uids {
		card, ok := findCard(b, id)
		if !ok {
			continue
		}
		events, _, err := s.backend.CardLogSince(ctx, b, id, since)
		if err != nil {
			return nil, err
		}
		entry := DayLog{Events: onDay(events, day)}
		for _, n := range card.Notes {
			if board.LocalDateIso(n.CreatedAt) == day {
				entry.Notes = append(entry.Notes, n)
			}
		}
		out[id] = entry
	}
	return out, nil
}

// dayLayout is the ISO day the boards speak.
const dayLayout = "2006-01-02"

// onDay keeps the events of that board day. The walk stops at the day's
// start, so this only drops what came after it — the day the caller asked
// about is one day, not "since".
func onDay(events []board.Event, day string) []board.Event {
	var out []board.Event
	for _, e := range events {
		if board.LocalDateIso(e.At) == day {
			out = append(out, e)
		}
	}
	return out
}
