//go:build !windows

package indexlock

import (
	"errors"
	"os"
	"syscall"
)

func tryLockGuardFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return errGuardBusy
	}
	return err
}

func unlockGuardFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
