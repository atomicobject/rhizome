package actions

import (
	"context"
	"errors"
	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// acquireWriteLock serializes direct CLI mutations with the indexer and other
// vault writers. Callers must acquire it before reading content they will edit.
func acquireWriteLock(ctx context.Context, vaultPath string) (func() error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath := filepath.Join(vaultPath, ".rhizome", "index.lock")
	var priority *indexlock.PriorityRequest
	defer func() { priority.Close() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, indexlock.AcquireOptions{Role: "interactive/cli-mutation"})
		if err != nil {
			return nil, err
		}
		if acquired {
			if err := ctx.Err(); err != nil {
				_ = release()
				return nil, err
			}
			if err := namespaceadmission.Check(vaultPath); err != nil {
				return nil, errors.Join(err, release())
			}
			return release, nil
		}
		if !priority.Active() {
			priority.Close()
			priority, err = indexlock.RequestPriority(lockPath)
			if err != nil {
				return nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func acquireMutationLock(ctx context.Context, vaultPath string, dryRun bool) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if dryRun {
		return func() {}, ctx.Err()
	}
	release, err := acquireWriteLock(ctx, vaultPath)
	if err != nil {
		return nil, err
	}
	return func() { _ = release() }, nil
}
