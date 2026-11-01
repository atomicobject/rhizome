package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const lockTimeout = 30 * time.Second

var errLockBusy = errors.New("team session lock held")

// lockFile takes the per-user session lock, polling like
// sqliteutil.LockSchemaInit. The OS releases it if the holder crashes.
func lockFile(ctx context.Context, path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.NewTimer(lockTimeout)
	defer deadline.Stop()
	for {
		err := tryLockFile(file)
		if err == nil {
			return func() {
				_ = unlockFile(file)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, errLockBusy) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = file.Close()
			return nil, ErrLockTimeout
		case <-time.After(10 * time.Millisecond):
		}
	}
}
