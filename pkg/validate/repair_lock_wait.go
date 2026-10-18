package validate

import (
	"context"
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// Interactive repair writers wait for the current owner and ask background
// work to yield. Startup stabilization keeps its nonblocking acquisition.
func waitRepairIndexLockLease(ctx context.Context, lockPath string) (*indexlock.Lease, func() error, error) {
	var priority *indexlock.PriorityRequest
	defer func() { priority.Close() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		lease, acquired, err := indexlock.TryAcquireLease(filepath.Dir(filepath.Dir(lockPath)), indexlock.AcquireOptions{Role: "interactive/repair"})
		if err != nil {
			return nil, nil, err
		}
		if acquired {
			if err := ctx.Err(); err != nil {
				_ = lease.Release()
				return nil, nil, err
			}
			return repairLeaseHeartbeat(lease)
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
