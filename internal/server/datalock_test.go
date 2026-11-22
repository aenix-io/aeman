package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A process on its way out still holds the claim: `aeman mcp` drains its
// write queue after the client closes the pipe, and that drain fetches every
// domain, so it can take a network round trip. A client restarting its
// server meets that predecessor, and refusing at once answers a restart with
// advice about a shared daemon nobody is running. So a start waits.
func TestDataDirLockWaitsForAPredecessorToLetGo(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = first.Close()
	}()

	start := time.Now()
	second, err := lockDataDirWaiting(dir)
	if err != nil {
		t.Fatalf("a claim released inside the wait was refused anyway: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if waited := time.Since(start); waited < 100*time.Millisecond {
		t.Errorf("granted after %v, which is before the first holder let go", waited)
	}
}

// The wait is bounded, or the rule stops being a refusal: a process that is
// here to stay still gets told, and still gets told which directory and what
// to do about it.
func TestDataDirLockStillRefusesAHolderThatStays(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })

	short := 120 * time.Millisecond
	restore := dataLockWait
	dataLockWait = short
	t.Cleanup(func() { dataLockWait = restore })

	start := time.Now()
	_, err = lockDataDirWaiting(dir)
	if err == nil {
		t.Fatal("a directory held for longer than the wait was granted")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("the refusal does not name the directory: %v", err)
	}
	if waited := time.Since(start); waited < short {
		t.Errorf("gave up after %v, before the wait was spent", waited)
	}
}

// The data directory holds the board's clones, and the clones are working
// files: two processes over one --data corrupt each other's fetch, rebase
// and push. The lock is what makes "one process per data directory" a
// refusal at start instead of a race hours later (G62).

func TestDataDirLockSecondOpenFails(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("lockDataDir: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	// One process is enough to prove it: the lock is per open file, so a
	// second claim from this very process conflicts exactly as another
	// process would.
	second, err := lockDataDir(dir)
	if err == nil {
		_ = second.Close()
		t.Fatal("a second claim on the held data directory was granted")
	}
}

func TestDataDirLockErrorNamesTheDirAndTheDaemon(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("lockDataDir: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	_, err = lockDataDir(dir)
	if err == nil {
		t.Fatal("the second claim was granted")
	}
	// The person who hits this is running a second MCP client against a
	// board that already has a process; the message has to carry both ways
	// out, not just the diagnosis.
	for _, want := range []string{dir, "aeman mcp --listen", "claude mcp add --transport http", "--data"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never mentions %q: %v", want, err)
		}
	}
}

func TestDataDirLockIsReleasedOnClose(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("lockDataDir: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("the directory is still claimed after Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// An explicit Close beside a deferred one is the normal shape at a
	// call site, so the second one has to be a no-op rather than an error.
	if err := second.Close(); err != nil {
		t.Fatalf("Close twice: %v", err)
	}
}

func TestDataDirLockCreatesTheDirAndReusesTheFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache", "aeman")
	first, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("lockDataDir on a directory that does not exist yet: %v", err)
	}
	path := filepath.Join(dir, "lock")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the lock file: %v", err)
	}
	if runtime.GOOS != "windows" {
		// NTFS does not carry Unix permission bits.
		if perm := before.Mode().Perm(); perm != 0o600 {
			t.Errorf("lock file mode = %v, want 0600", perm)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The file outlives the process that made it: a leftover lock file
	// locks nothing, and the next run takes the same one rather than
	// refusing on it or replacing it.
	second, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("the lock file left behind refused the next run: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the lock file again: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("the lock file was replaced instead of reused")
	}
}

// releaseDataDir hands the server's claim on <data>/lock back when the test
// ends: t.TempDir has to delete that directory, and on Windows an open
// handle makes the removal retry and then fail the test. Call it after the
// t.TempDir that made the directory, so this cleanup runs before that one.
//
// It also WAITS FOR THE WRITE QUEUE first. A request is answered from the
// cache and the commit for it is written afterwards by the queue's worker, so
// a test that asserts on the answer ends while its clone is still being
// written to — and t.TempDir removing the directory under that worker fails
// the test with "directory not empty", about a test with nothing wrong in it.
// It is a race, so it appears on a loaded machine and not on a quiet one: CI
// found it, twelve local runs did not.
func releaseDataDir(t *testing.T, srv *Server) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Not an assertion: a test may end with work deliberately stuck in the
		// queue (a backend that refuses), and that is its business.
		if left := srv.store.waitDrained(ctx); left > 0 {
			t.Logf("the write queue still held %d write(s) when the test ended", left)
		}
		_ = srv.Close()
	})
}

// The claim is on a file inside the directory it guards, so removing the
// directory removes the claim with it: the next start makes a fresh lock file
// and takes it. The holder can still tell — the file at the path is no longer
// the one it holds — and that is how it learns its directory is gone.
func TestADataLockKnowsItsFileWasReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the lock is opened without sharing: the file cannot be removed while held")
	}
	dir := t.TempDir()
	held, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	if err := held.Lost(); err != nil {
		t.Fatalf("an untouched lock reads as lost: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if err := held.Lost(); err == nil {
		t.Fatal("a removed lock file must read as lost")
	}
	next, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("a second start after the removal is refused, so the race cannot happen: %v", err)
	}
	t.Cleanup(func() { _ = next.Close() })
	if err := held.Lost(); err == nil {
		t.Fatal("a lock file replaced by another process's must read as lost")
	}
	if err := next.Lost(); err != nil {
		t.Fatalf("the new holder reads its own lock as lost: %v", err)
	}
}

// Lost answers only what it can be sure of. No claim and a released claim
// guard nothing to lose; a path it cannot read is not proof the directory is
// gone, and refusing every write on a passing EACCES would be worse than the
// next tick asking again. A data path that has become a file is gone.
func TestADataLockIsLostOnlyWhenItKnows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions and removal under a held lock")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads through chmod 0, so the unreadable case cannot be made")
	}
	var none *dataLock
	if err := none.Lost(); err != nil {
		t.Fatalf("a nil claim reads as lost: %v", err)
	}
	dir := t.TempDir()
	closed, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	if err := os.Remove(filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if err := closed.Lost(); err != nil {
		t.Fatalf("a released claim reads as lost: %v", err)
	}

	unreadable := filepath.Join(t.TempDir(), "data")
	held, err := lockDataDir(unreadable)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o750) })
	if err := held.Lost(); err != nil {
		t.Fatalf("an unreadable data dir reads as lost: %v", err)
	}

	filed := filepath.Join(t.TempDir(), "data")
	held2, err := lockDataDir(filed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held2.Close() })
	if err := os.RemoveAll(filed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := held2.Lost(); !errors.Is(err, errDataDirLost) {
		t.Fatalf("a data path turned into a file = %v, want errDataDirLost", err)
	}
}
