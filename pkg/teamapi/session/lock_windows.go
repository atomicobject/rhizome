//go:build windows

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// LockFileEx locks belong to the file handle, so two handles in one process
// contend the same way two processes do, as flock does elsewhere. Managed
// sign-in is not offered on Windows yet (DefaultStore is unavailable), but the
// lock keeps refresh coordination correct for injected stores.
func tryLockFile(file *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLockBusy
	}
	return err
}

func unlockFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
