//go:build !windows

package sqliteutil

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes an exclusive flock without blocking. flock locks belong
// to the open file description, so two handles in one process contend the
// same way two processes do.
func tryLockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return errSchemaLockBusy
	}
	return err
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
