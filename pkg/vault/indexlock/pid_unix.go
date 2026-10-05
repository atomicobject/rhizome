//go:build !windows

package indexlock

import "syscall"

func pidExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Signal 0 checks existence without sending a real signal.
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if err == syscall.ESRCH {
		return false
	}
	// EPERM, etc: process exists but we can't signal it.
	return true
}
