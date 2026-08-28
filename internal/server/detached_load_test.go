package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/boardservice/boardservicetest"
)

// gatedLoads wraps the test backend so a full load blocks until released —
// the shape of a multi-page GitHub fetch on a big board.
type gatedLoads struct {
	boardservice.Backend
	gate  chan struct{}
	loads atomic.Int32
}

func (g *gatedLoads) LoadBoard(ctx context.Context, boardID string) (board.Board, error) {
	g.loads.Add(1)
	select {
	case <-g.gate:
	case <-ctx.Done():
		return board.Board{}, ctx.Err()
	}
	return g.Backend.LoadBoard(ctx, boardID)
}

// A cold load survives the request that started it: the user refreshing the
// page (their context dying) must not kill the minute-long fetch — the next
// request rides the load already in flight, and its result lands in the
// cache. Before this, every refresh restarted the fetch from zero and a
// refresh-happy user could keep a board cold forever.
func TestColdLoadSurvivesItsRequest(t *testing.T) {
	store := newBoardStore()
	fake := boardservicetest.New([]board.Card{{ItemID: "c1", Title: "one"}}, nil)
	gated := &gatedLoads{Backend: fake, gate: make(chan struct{})}
	be := &storeBackend{inner: gated, store: store}

	// The first request dies mid-load.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := be.LoadBoard(ctx, "o"); err == nil {
			t.Error("a canceled request should answer with its context error")
		}
	}()
	time.Sleep(50 * time.Millisecond) // let it reach the gate
	cancel()
	wg.Wait()

	// The fetch is still in flight; releasing it must fill the cache.
	close(gated.gate)
	deadline := time.Now().Add(2 * time.Second)
	for {
		bd, err := be.LoadBoard(context.Background(), "o")
		if err == nil && len(bd.Cards) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the detached load never landed in the cache")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The whole episode cost ONE backend fetch: the second request joined the
	// first one's load instead of restarting it.
	if n := gated.loads.Load(); n != 1 {
		t.Fatalf("backend was loaded %d times, want 1", n)
	}
}
