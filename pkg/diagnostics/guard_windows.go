//go:build windows

package diagnostics

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

const lockOffsetHigh = 1 << 30

func tryFileLock(file *os.File) error {
	// Exclusive byte locks deny reads through other handles in that region.
	// A sentinel beyond all segment data protects ownership without preventing
	// live offline readers. LockFileEx permits locks beyond EOF without growing
	// the file. All guard/segment contenders use this same offset.
	overlapped := windows.Overlapped{OffsetHigh: lockOffsetHigh}
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errGuardBusy
	}
	return err
}

func unlockFile(file *os.File) error {
	overlapped := windows.Overlapped{OffsetHigh: lockOffsetHigh}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
