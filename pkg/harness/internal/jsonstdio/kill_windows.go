//go:build windows

package jsonstdio

import (
	"errors"
	"syscall"
)

func reapedKillErrorIsBenign(exited <-chan struct{}, err error) bool {
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return false
	}
	// TerminateProcess reports access denied when the process has already exited.
	select {
	case <-exited:
		return true
	default:
		return false
	}
}
