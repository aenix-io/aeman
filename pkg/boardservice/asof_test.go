package boardservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
)

// pastBackend is a fake whose storage keeps history: one board per moment.
type pastBackend struct {
	*fakeBackend
	at        time.Time
	past      board.Board
	truncated bool
}

func (p *pastBackend) LoadBoardAsOf(_ context.Context, boardID string, at time.Time) (board.Board, bool, error) {
	if p.truncated {
		return board.Board{}, false, nil
	}
	p.at = at
	bd := p.past
	bd.Board = boardID
	return bd, true, nil
}

// Going back a day answers with the board of THAT day. The service asks its
// storage rather than reconstructing anything: what the day held is a fact
// the repository already keeps.
func TestTheBoardOfAPastDayComesFromTheStorage(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha", Progress: 100, Title: "today's copy"}}, nil)
	yesterday := board.Board{Cards: []board.Card{{ItemID: "c1", Team: "alpha", Progress: 40, Title: "as it stood"}}}
	p := &pastBackend{fakeBackend: f, past: yesterday}
	at := time.Date(2026, 8, 25, 23, 59, 59, 0, time.UTC)

	bd, err := New(p).BoardAsOf(context.Background(), "acme", at)
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Cards) != 1 || bd.Cards[0].Progress != 40 {
		t.Fatalf("the past board = %+v; the day's own values must survive", bd.Cards)
	}
	if bd.Board != "acme" {
		t.Fatalf("board id = %q", bd.Board)
	}
	if !p.at.Equal(at) {
		t.Fatalf("the storage was asked for %v, want %v", p.at, at)
	}
}

// A day the history no longer reaches is said so, never answered with the
// oldest state at hand: that would put a stranger's values on the day.
func TestAPastDayBeyondTheHistoryIsReported(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha"}}, nil)
	p := &pastBackend{fakeBackend: f, truncated: true}
	_, err := New(p).BoardAsOf(context.Background(), "acme", time.Now().Add(-500*24*time.Hour))
	if !errors.Is(err, ErrHistoryTruncated) {
		t.Fatalf("err = %v, want ErrHistoryTruncated", err)
	}
}

// Storage that keeps only the present says so — an embedder's backend need
// not have a history at all (docs/embedding.md), and a board served from one
// must fail plainly rather than pretend the day is empty.
func TestStorageWithoutAHistorySaysSo(t *testing.T) {
	f := newFake([]board.Card{{ItemID: "c1", Team: "alpha"}}, nil)
	_, err := New(f).BoardAsOf(context.Background(), "acme", time.Now().Add(-24*time.Hour))
	if !errors.Is(err, ErrNoHistory) {
		t.Fatalf("err = %v, want ErrNoHistory", err)
	}
}
