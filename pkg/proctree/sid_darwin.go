package proctree

import "syscall"

// sessionOf returns pid's session, or -1. ps on macOS has no session
// column, but getsid answers for any pid.
func sessionOf(pid int) int {
	sid, err := syscall.Getsid(pid)
	if err != nil {
		return -1
	}
	return sid
}
