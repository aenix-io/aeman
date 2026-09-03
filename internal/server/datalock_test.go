package server

import (
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
func releaseDataDir(t *testing.T, srv *Server) {
	t.Helper()
	t.Cleanup(func() { _ = srv.Close() })
}
