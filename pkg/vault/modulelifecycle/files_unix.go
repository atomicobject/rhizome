//go:build !windows

package modulelifecycle

import "syscall"

const safeReadFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
