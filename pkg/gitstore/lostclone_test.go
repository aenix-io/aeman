package gitstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

func cloneOnDisk(t *testing.T, remote Remote) (*Repo, string) {
	t.Helper()
	dir := t.TempDir()
	s := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	r, err := Clone(context.Background(), s, remote, Options{Committer: serverID, Dir: dir}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

// A clone whose directory is removed while the process runs has lost its
// refs and its shallow list. Fetching into it asks the forge for the whole
// history on every tick, so the sync refuses instead and creates nothing.
func TestSyncRefusesAStoreRemovedFromUnderIt(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	r, dir := cloneOnDisk(t, remote)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if _, moved, err := r.Fetch(context.Background(), remote); !errors.Is(err, ErrStoreLost) || moved {
		t.Fatalf("fetch: moved=%v err=%v, want ErrStoreLost", moved, err)
	}
	if err := r.DeepenSince(context.Background(), remote, time.Now().AddDate(0, 0, -14)); !errors.Is(err, ErrStoreLost) {
		t.Fatalf("deepen: %v, want ErrStoreLost", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the refused sync wrote into the removed store: %v", err)
	}
}

// A write into a removed store would land as a parentless commit holding
// one card, and every later sync would treat it as the board. It is refused.
func TestCommitRefusesAStoreRemovedFromUnderIt(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	r, dir := cloneOnDisk(t, remote)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	_, err := r.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "x"}, []FileWrite{
		{Path: "cards/a/1/A1.md", Data: []byte("---\ntitle: a\nprogress: 90\n---\n")},
	})
	if !errors.Is(err, ErrStoreLost) {
		t.Fatalf("commit: %v, want ErrStoreLost", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the refused commit wrote into the removed store: %v", err)
	}
}

// go-git leaves its temporary pack behind when the pack it receives does
// not parse (a transfer cut by the tick deadline, a dropped connection).
// One such file per failed tick filled the disk; a failed transfer must
// leave the pack directory as it found it.
func TestAFailedPackTransferLeavesNoTemporaryPack(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	r, dir := cloneOnDisk(t, remote)
	packDir := filepath.Join(dir, "objects", "pack")
	before, err := os.ReadDir(packDir)
	if err != nil {
		t.Fatal(err)
	}

	truncated := strings.NewReader("PACK\x00\x00\x00\x02\x00\x00\x00\x05\x95\x0a")
	if err := r.storePack(truncated); err == nil {
		t.Fatal("a truncated pack was accepted")
	}

	after, err := os.ReadDir(packDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		names := make([]string, 0, len(after))
		for _, e := range after {
			names = append(names, e.Name())
		}
		t.Fatalf("pack dir after a failed transfer: %v (had %d entries)", names, len(before))
	}
}

// The guard is for stores on disk only: an in-memory store has no
// directory to lose and syncs as before.
func TestAnInMemoryStoreStillSyncs(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	r := cloneFull(t, remote)
	if _, _, err := r.Fetch(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
}

// The system git names its repack output tmp_pack_* too, and a deepen runs
// outside the server's apply lock. A failed pack write must not remove the
// pack a repack is still writing, so the two never overlap.
func TestRepackWaitsForAPackWriteInFlight(t *testing.T) {
	requireGit(t)
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	r, _ := cloneOnDisk(t, remote)

	r.packMu.Lock()
	done := make(chan error, 1)
	go func() { done <- r.Maintain(context.Background()) }()
	select {
	case err := <-done:
		r.packMu.Unlock()
		t.Fatalf("repack ran during a pack write: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	r.packMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A store may be lost in a way HEAD cannot show: the directory removed and
// filled again by another process, whose clone has a HEAD of its own. The
// owner of the Repo knows (server: its data-directory lock was replaced) and
// says so through the guard; commit, fetch and deepen then refuse as for a
// missing HEAD, with the guard's error.
func TestAStoreGuardRefusesCommitAndSync(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	r, _ := cloneOnDisk(t, remote)
	errGone := errors.New("the directory belongs to someone else now")
	r.SetStoreGuard(func() error { return errGone })
	head := r.Head()

	if _, err := r.Commit(Action{Name: "progress", Actor: "kvaps", Summary: "x"}, []FileWrite{
		{Path: TeamPath("01JB4TEAM9"), Data: []byte("name: late\n")},
	}); !errors.Is(err, errGone) {
		t.Fatalf("commit: %v, want the guard's error", err)
	}
	if _, _, err := r.Fetch(context.Background(), remote); !errors.Is(err, errGone) {
		t.Fatalf("fetch: %v, want the guard's error", err)
	}
	if err := r.DeepenSince(context.Background(), remote, time.Now().AddDate(0, 0, -14)); !errors.Is(err, errGone) {
		t.Fatalf("deepen: %v, want the guard's error", err)
	}
	if r.Head() != head {
		t.Fatal("a guarded store moved its head")
	}

	r.SetStoreGuard(func() error { return nil })
	if err := r.CheckStore(); err != nil {
		t.Fatalf("a guard that passes still refuses: %v", err)
	}
}

// The data path comes from the operator and may hold a glob metacharacter;
// the cleanup after a failed pack write must not depend on it.
func TestAFailedPackTransferLeavesNoTemporaryPackUnderABracketedPath(t *testing.T) {
	remote, _ := newTestRemote(t)
	seedRemote(t, remote)
	dir := filepath.Join(t.TempDir(), "data[1]")
	s := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	r, err := Clone(context.Background(), s, remote, Options{Committer: serverID, Dir: dir}, 0)
	if err != nil {
		t.Fatal(err)
	}

	truncated := strings.NewReader("PACK\x00\x00\x00\x02\x00\x00\x00\x05\x95\x0a")
	if err := r.storePack(truncated); err == nil {
		t.Fatal("a truncated pack was accepted")
	}

	entries, err := os.ReadDir(filepath.Join(dir, "objects", "pack"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tmp_pack_") {
			t.Fatalf("a temporary pack was left under a bracketed path: %s", e.Name())
		}
	}
}
