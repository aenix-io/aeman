//go:build windows

package server

import (
	"errors"
	"os"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION: the file is open elsewhere
// under a share mode that forbids this open. The syscall package does not
// name it, and one integer is not worth importing x/sys/windows for.
const errSharingViolation = syscall.Errno(32)

// acquireLock opens the lock file with a share mode of zero, so Windows
// refuses every other open of it until this handle closes. That gives the
// unix build's semantics: a second open conflicts even inside one process,
// and the handle is released when the process ends, however it ends.
// OPEN_ALWAYS takes the file that is there and creates it otherwise, which
// is what makes a lock file left behind by a crash harmless.
func acquireLock(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		// Only a sharing violation means another handle holds it; access
		// denied is a permission problem and has to read as one.
		if errors.Is(err, errSharingViolation) {
			return nil, errLockHeld
		}
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
