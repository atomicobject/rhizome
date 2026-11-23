//go:build !windows

package tasks

import "syscall"

// safeOpenFlags keep openRegular from following a final symlink or
// blocking on a FIFO.
const safeOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
