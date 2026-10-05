//go:build windows

package indexlock

import "golang.org/x/sys/windows"

const STILL_ACTIVE = 259 // Windows constant: process is still running

func pidExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Prefer a real process handle check on Windows so stale locks can be recovered.
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied still implies the process likely exists.
		if err == windows.ERROR_ACCESS_DENIED {
			return true
		}
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		// If we can't query, be conservative and assume it exists.
		return true
	}
	return code == STILL_ACTIVE
}
