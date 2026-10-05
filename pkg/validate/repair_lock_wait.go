package validate

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// Interactive repair writers wait for the current owner and ask background
// work to yield. Startup stabilization keeps its nonblocking acquisition.
func waitRepairIndexLockLease(ctx context.Context, lockPath string) (*IndexLockLease, func() error, error) {
	var priority *indexlock.PriorityRequest
	defer func() { priority.Close() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, indexlock.AcquireOptions{Role: "interactive/repair"})
		if err != nil {
			return nil, nil, err
		}
		if acquired {
			if err := ctx.Err(); err != nil {
				_ = release()
				return nil, nil, err
			}
			return newRepairIndexLockLease(lockPath, release)
		}
		if !priority.Active() {
			priority.Close()
			priority, err = indexlock.RequestPriority(lockPath)
			if err != nil {
				return nil, nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
