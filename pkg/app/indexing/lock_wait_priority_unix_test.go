//go:build !windows

package indexing

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestIndexingWaiterKeepsPriorityDuringGuardContention(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	writeHeldLock(t, lockPath, "runtime/embed-cycle")
	guard, err := os.OpenFile(indexlock.PriorityPath(lockPath)+".guard.lock", os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(guard.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	unlock := sync.OnceFunc(func() {
		require.NoError(t, syscall.Flock(int(guard.Fd()), syscall.LOCK_UN))
		require.NoError(t, guard.Close())
	})
	t.Cleanup(unlock)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		_, err := TryAcquireIndexLockWithOptions(ctx, lockPath, LockWaitOptions{
			RequestPriority: true, Out: io.Discard, Poll: time.Millisecond,
		})
		done <- err
	}()
	require.Eventually(t, func() bool {
		entries, err := os.ReadDir(indexlock.PriorityPath(lockPath))
		return err == nil && len(entries) == 1 && filepath.Ext(entries[0].Name()) == ".json"
	}, time.Second, time.Millisecond)
	require.True(t, indexlock.CheckPriority(lockPath), "the waiter must publish through a paused scanner")
	unlock()
	require.True(t, indexlock.CheckPriority(lockPath), "the active waiter retains priority after contention ends")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.False(t, indexlock.CheckPriority(lockPath))
}
