package userstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

func databasePath(root string) string {
	return filepath.Join(paths.ResolveSymlinks(root).String(), ".rhizome", "user-state.sqlite")
}

// SeedWorktree copies personal state once. Existing destination state wins;
// the initialization lock also protects against a concurrent Store.Open.
func SeedWorktree(ctx context.Context, sourceRoot, targetRoot string) (seeded bool, err error) {
	source, target := databasePath(sourceRoot), databasePath(targetRoot)
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("stat source user state: %w", err)
	}
	release, err := sqliteutil.LockSchemaInit(ctx, target)
	if err != nil {
		return false, err
	}
	defer release()
	files := []string{target, target + "-wal", target + "-shm"}
	for _, path := range files {
		if _, err := os.Lstat(path); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("stat target user state: %w", err)
		}
	}
	defer func() {
		if err != nil {
			for _, path := range files {
				if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					err = errors.Join(err, removeErr)
				}
			}
		}
	}()
	if err = sqliteutil.CloneInto(ctx, source, target); err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		err = sqliteutil.SnapshotInto(ctx, source, target)
	}
	if err != nil {
		return false, fmt.Errorf("seed worktree user state: %w", err)
	}
	return true, nil
}
