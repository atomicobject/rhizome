package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestRepairWaiterRecoversExpiredPriority(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, cleanup, err := waitRepairIndexLockLease(ctx, lockPath)
		if cleanup != nil {
			_ = cleanup()
		}
		done <- err
	}()
	defer func() {
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	}()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, time.Millisecond)
	dir := indexlock.PriorityPath(lockPath)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	requestPath := filepath.Join(dir, entries[0].Name())
	peer, err := indexlock.RequestPriority(lockPath)
	require.NoError(t, err)
	defer peer.Close()
	// Reproduce the expired record left after a stopped priority heartbeat
	// without waiting for the normal 90-second recovery grace.
	expired := time.Now().Add(-indexlock.PriorityStaleAfter - time.Second)
	require.NoError(t, os.Chtimes(requestPath, expired, expired))
	_ = indexlock.CheckPriority(lockPath)
	require.Eventually(t, func() bool {
		if !indexlock.CheckPriority(lockPath) {
			return false
		}
		requests, err := os.ReadDir(dir)
		if err != nil || len(requests) != 2 {
			return false
		}
		for _, request := range requests {
			if request.Name() == entries[0].Name() {
				return false
			}
		}
		return true
	}, time.Second, 5*time.Millisecond, "the blocked repair must renew its own priority while the peer remains live")
	require.NoFileExists(t, requestPath)
	peer.Close()
	require.True(t, indexlock.CheckPriority(lockPath), "the renewed repair request must survive the peer exiting")
}
