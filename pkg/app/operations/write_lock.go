package operations

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/sourcetxn/admission"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// acquireWriteLease serializes direct CLI mutations with the indexer and other
// vault writers. Callers must acquire it before reading content they will edit.
func acquireWriteLease(ctx context.Context, vaultPath string) (*indexlock.Lease, error) {
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
		lease, acquired, err := indexlock.TryAcquireLease(vaultPath, indexlock.AcquireOptions{Role: "interactive/cli-mutation"})
		if err != nil {
			return nil, err
		}
		if acquired {
			if err := ctx.Err(); err != nil {
				_ = lease.Release()
				return nil, err
			}
			if err := namespaceadmission.Check(vaultPath); err != nil {
				return nil, errors.Join(err, lease.Release())
			}
			return lease, nil
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
	lease, err := acquireWriteLease(ctx, vaultPath)
	if err != nil {
		return nil, err
	}
	return func() { _ = lease.Release() }, nil
}
