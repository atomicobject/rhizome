package appbundle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func withPortableLock(ctx context.Context, root string, action func() error) error {
	if !portableRoot(root) {
		return errors.New("invalid desktop installation root")
	}
	return withInstallationLock(ctx, root, action)
}

func withPortableEntryLock(ctx context.Context, root string, action func() error) error {
	return withPortableLock(ctx, root, func() error {
		return withInstallationLock(ctx, filepath.Dir(root), action)
	})
}

func withInstallationLock(ctx context.Context, root string, action func() error) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(root, "installation.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		locked, err := tryPortableLock(file)
		if err != nil {
			return err
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer unlockPortable(file)
	return action()
}
