//go:build windows

package session

import (
	"errors"
	"os"
)

// ponytail: no Windows lock yet. DefaultStore is unavailable there, so no
// production session reaches it; add LockFileEx (see pkg/vault/indexlock)
// together with the Credential Manager adapter.
func tryLockFile(*os.File) error {
	return errors.New("team session lock is not supported on Windows yet")
}

func unlockFile(*os.File) error { return nil }
