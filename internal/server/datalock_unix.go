//go:build unix && !solaris && !aix

// syscall.Flock is absent on solaris and aix, so `unix` alone claims two
// platforms this file cannot compile on. Excluding them says what is true;
// making them work would mean x/sys/unix.Flock, which neither shipped image
// needs.
package server

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquireLock opens the lock file and takes an exclusive, non-blocking flock
// on it. flock belongs to the open file description rather than the process,
// so a second open conflicts even inside one process — which is what lets a
// single test prove the rule — and the kernel drops it when the descriptor
// closes, however the process ends.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		// Only a would-block means another descriptor holds it. A file
		// system that does not implement flock at all — 9p, some NFS and
		// FUSE mounts — fails here too, and reporting that as a process
		// that does not exist sends the reader hunting for one on a machine
		// where aeman could then never start.
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLockHeld
		}
		return nil, fmt.Errorf("flock %s: %w — this file system does not support locking; put --data on a local one", path, err)
	}
	return f, nil
}
