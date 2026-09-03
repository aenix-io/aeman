package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The data directory is not a cache: it holds the board's clones, and a
// clone is a set of files that a fetch, a rebase and a push all write into.
// Two processes pointed at one --data quietly ruin each other's state, so
// the PROCESS takes an exclusive claim on <data>/lock and holds it for as
// long as it runs; a second start is refused with the shape that shares one
// process instead.
//
// The process, not the store: a start whose board cannot be opened because
// the GitHub App is not installed yet stays up serving a setup page, having
// already made the clone directories, so the claim has to outlive a store
// that was never built. Server.New and OpenGitBackend take it; Close hands
// it back.

// errLockHeld is what the platform's acquireLock reports when the file is
// already claimed, as opposed to a directory that cannot be opened at all.
var errLockHeld = errors.New("data directory is already locked")

// dataLock is one process's claim on a data directory. Close releases it;
// so does the process exiting, however it exits.
type dataLock struct {
	once sync.Once
	f    *os.File
	err  error
}

// dataLockWait is how long a start gives a predecessor to let go. It exists
// because the holder may be leaving: `aeman mcp` drains its write queue
// after the client closes the pipe, and that drain fetches every domain, so
// the claim outlives the session by a network round trip. A client that
// restarts its server lands inside that window, and an immediate refusal
// answers a restart with advice about a shared daemon nobody is running.
//
// Bounded, or the rule stops being a rule: a process that is here to stay
// still gets refused, and the wait is short enough that nobody reads it as
// a hang. A variable so a test can spend it without spending the clock.
var dataLockWait = 5 * time.Second

// lockDataDir claims the directory now, for callers deciding whether it is
// free: an install that is about to write a unit wants today's answer, not
// one from five seconds hence.
func lockDataDir(dir string) (*dataLock, error) {
	return lockDataDirFor(dir, 0)
}

// lockDataDirWaiting claims it for a process that is starting up, giving a
// predecessor dataLockWait to finish leaving.
func lockDataDirWaiting(dir string) (*dataLock, error) {
	return lockDataDirFor(dir, dataLockWait)
}

// lockDataDirFor is both of those: one implementation, so the refusal and
// the error mapping cannot drift apart between them. wait of 0 refuses on
// the first attempt.
func lockDataDirFor(dir string, wait time.Duration) (*dataLock, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	path := filepath.Join(dir, "lock")
	deadline := time.Now().Add(wait)
	for {
		f, err := acquireLock(path)
		if err == nil {
			return &dataLock{f: f}, nil
		}
		if !errors.Is(err, errLockHeld) {
			return nil, fmt.Errorf("lock the data dir: %w", err)
		}
		if !time.Now().Before(deadline) {
			return nil, dataDirBusy(dir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Close releases the claim. It is safe on a nil lock and on one already
// closed, so a call site may both defer it and close explicitly, and it
// releases once however many callers reach it.
func (l *dataLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.f != nil {
			l.err = l.f.Close()
			l.f = nil
		}
	})
	return l.err
}

// DataDirHold takes the claim on a data directory and keeps it until the
// returned value is closed. A caller about to configure a long-running
// process holds it just long enough to learn the directory is free, then
// closes it; the answer is advisory, since it goes stale the moment it is
// given and the daemon takes the claim for real at its own start.
func DataDirHold(dir string) (io.Closer, error) {
	// Not `return lockDataDir(dir)`: that hands back a non-nil interface
	// wrapping a nil *dataLock on the error path, so a caller checking the
	// value instead of the error would be wrong.
	l, err := lockDataDir(dir)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// dataDirBusy is the refusal the second process gets. It is worded for
// either caller: `aeman serve` cannot take over an MCP daemon, so the way
// out it needs is a directory of its own, while a second MCP client wants
// the daemon that is already there.
func dataDirBusy(dir string) error {
	return fmt.Errorf("another aeman process is already using the data directory %s — one process owns a board's clones at a time; "+
		"if that process is the shared daemon (`aeman mcp --listen`), point this client at the address it listens on "+
		"(`claude mcp add --transport http aeman http://127.0.0.1:8766/mcp --scope user`, default port shown) rather than starting one of your own; "+
		"otherwise stop it, or give this process a directory of its own with --data; a process that has just exited may still be finishing its final push, in which case starting again in a moment succeeds", dir)
}
