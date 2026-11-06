//go:build windows

package jsonstdio

import (
	"errors"
	"syscall"
)

func reapedKillErrorIsBenign(exited <-chan struct{}, err error) bool {
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.EINVAL) {
		return false
	}
	// Windows reports access denied after exit, or EINVAL after Wait releases the handle.
	select {
	case <-exited:
		return true
	default:
		return false
	}
}
