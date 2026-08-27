package gitstore

import (
	"context"
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/storage/memory"
)

// G24 — a board begins with an explicit init: board.yaml and the no-team
// group in one commit, pushed. Running it again changes nothing, and a
// remote that already holds a board is left alone.

func TestInitBootstrapsAnEmptyRemote(t *testing.T) {
	remote, st := newTestRemote(t)
	ctx := context.Background()
	if err := InitBoard(ctx, memory.NewStorage(), remote, Options{Committer: serverID}, "aeman board"); err != nil {
		t.Fatal(err)
	}
	r := cloneFull(t, remote)
	s, err := Load(r)
	if err != nil {
		t.Fatal(err)
	}
	if s.Board.Schema != SchemaVersion || s.Board.Title != "aeman board" {
		t.Fatalf("board = %+v", s.Board)
	}
	if len(s.Teams) != 1 || s.Teams[0].ID != "_" || s.Teams[0].Name != "" {
		t.Fatalf("teams = %+v, want the no-team group only", s.Teams)
	}
	if n := walkCount(t, r); n != 1 {
		t.Fatalf("init made %d commits, want 1", n)
	}
	_ = st

	// A second init is a no-op: nothing new on the remote.
	if err := InitBoard(ctx, memory.NewStorage(), remote, Options{Committer: serverID}, "aeman board"); err != nil {
		t.Fatalf("second init: %v", err)
	}
	if n := walkCount(t, cloneFull(t, remote)); n != 1 {
		t.Fatalf("second init added commits: %d", n)
	}
}

// An unborn remote is what Clone reports as ErrEmptyRepository — the
// condition `serve` turns into "run aeman init".
func TestCloneOfUnbornRemoteIsNamed(t *testing.T) {
	remote, _ := newTestRemote(t)
	_, err := Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, 1)
	if !errors.Is(err, ErrEmptyRepository) {
		t.Fatalf("err = %v, want ErrEmptyRepository", err)
	}
}

// A remote that has commits on OTHER branches but no board branch is, for
// the board, just as unborn: init creates the branch, serve asks for init.
func TestCloneWithoutBoardBranchIsUnborn(t *testing.T) {
	remote, _ := newTestRemote(t)
	other, err := Init(memory.NewStorage(), Options{Committer: serverID, Branch: "refs/heads/spike"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Commit(Action{Name: "x", Summary: "spike"}, []FileWrite{{Path: "notes.txt", Data: []byte("hi\n")}}); err != nil {
		t.Fatal(err)
	}
	if err := other.Push(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	_, err = Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, 0)
	if !errors.Is(err, ErrEmptyRepository) {
		t.Fatalf("err = %v, want ErrEmptyRepository for a remote without the board branch", err)
	}
	// And init then creates the board branch beside the other one.
	if err := InitBoard(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, "b"); err != nil {
		t.Fatalf("init beside another branch: %v", err)
	}
	if _, err := Clone(context.Background(), memory.NewStorage(), remote, Options{Committer: serverID}, 0); err != nil {
		t.Fatalf("clone after init: %v", err)
	}
}
