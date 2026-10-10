package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
)

type reloadProbe struct {
	boardservice.Backend
	entered chan string
	release chan struct{}
	board   board.Board
	err     error
}

func (p *reloadProbe) LoadBoard(ctx context.Context, boardID string) (board.Board, error) {
	p.entered <- boardID
	select {
	case <-p.release:
		return p.board, p.err
	case <-ctx.Done():
		return board.Board{}, ctx.Err()
	}
}

func TestReloadFromTipCapturesTheBoardUnderTheEntryLock(t *testing.T) {
	probe := &reloadProbe{
		entered: make(chan string, 1),
		release: make(chan struct{}),
		board:   board.Board{Board: "fresh", Title: "loaded"},
	}
	e := &boardEntry{board: board.Board{Board: "old"}}
	be := &storeBackend{inner: probe}
	done := make(chan error, 1)

	// Hold the entry lock while reload starts. It must wait to capture the
	// board identifier instead of reading the shared snapshot without its lock.
	e.mu.Lock()
	go func() { done <- be.reloadFromTip(context.Background(), e) }()
	select {
	case id := <-probe.entered:
		e.mu.Unlock()
		close(probe.release)
		<-done
		t.Fatalf("backend load started with %q while the entry lock was held", id)
	case <-time.After(50 * time.Millisecond):
	}
	e.board.Board = "captured"
	e.mu.Unlock()

	var id string
	select {
	case id = <-probe.entered:
	case <-time.After(time.Second):
		t.Fatal("reload did not reach the backend after the entry lock was released")
	}
	if id != "captured" {
		t.Fatalf("LoadBoard board ID = %q, want the value captured under the lock", id)
	}

	// Backend I/O must not retain e.mu. A concurrent replacement is allowed
	// while the load is blocked, and install later serializes normally.
	e.mu.Lock()
	e.board.Board = "replacement"
	e.mu.Unlock()
	close(probe.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	got := e.board
	e.mu.Unlock()
	if got.Board != "fresh" || got.Title != "loaded" {
		t.Fatalf("installed board = %+v", got)
	}
}

func TestReloadFromTipDoesNotInstallABoardAfterLoadFails(t *testing.T) {
	wantErr := errors.New("load failed")
	probe := &reloadProbe{
		entered: make(chan string, 1),
		release: make(chan struct{}),
		board:   board.Board{Board: "wrong"},
		err:     wantErr,
	}
	e := &boardEntry{board: board.Board{Board: "kept", Title: "before"}, loaded: true}
	be := &storeBackend{inner: probe}
	close(probe.release)

	if err := be.reloadFromTip(context.Background(), e); !errors.Is(err, wantErr) {
		t.Fatalf("reload error = %v, want %v", err, wantErr)
	}
	if e.board.Board != "kept" || e.board.Title != "before" {
		t.Fatalf("failed reload changed the cache to %+v", e.board)
	}
}
