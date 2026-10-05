package indexing

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

// writeHeldLock fakes a live holder: this process's own PID and runtime
// identity, so the wait loop treats the lock as held rather than stale.
func writeHeldLock(t *testing.T, lockPath, role string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, indexlock.AcquireOptions{Role: role})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
}

func TestTryAcquireIndexLockWaitsAndReportsTheHolder(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	writeHeldLock(t, lockPath, "runtime/embed-cycle")

	var out bytes.Buffer
	_, err := TryAcquireIndexLockWithOptions(context.Background(), lockPath, LockWaitOptions{
		Role:  LockRoleCLIIndex,
		Out:   &out,
		Poll:  10 * time.Millisecond,
		Limit: 60 * time.Millisecond,
	})
	require.Error(t, err)

	status := out.String()
	require.Contains(t, status, "Waiting for the index lock")
	require.Contains(t, status, "runtime/embed-cycle")
	require.Contains(t, status, "pid")
	require.Contains(t, status, "held ")
	require.Contains(t, status, "last heartbeat")
	require.GreaterOrEqual(t, strings.Count(status, "\r"), 2, "the status line refreshes while waiting")
}

func TestTryAcquireIndexLockExitsCleanlyOnCancel(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	writeHeldLock(t, lockPath, "cli/validate")

	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := TryAcquireIndexLockWithOptions(ctx, lockPath, LockWaitOptions{Out: &out, Poll: 10 * time.Millisecond})
		done <- err
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the wait did not return")
	}
	wg.Wait()
}

func TestTryAcquireIndexLockAcquiresOnceTheHolderReleases(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, indexlock.AcquireOptions{Role: "runtime/watcher-batch"})
	require.NoError(t, err)
	require.True(t, acquired)

	var out bytes.Buffer
	acquiredCh := make(chan func() error, 1)
	go func() {
		got, err := TryAcquireIndexLockWithOptions(context.Background(), lockPath, LockWaitOptions{
			Role: LockRoleCLIIndex,
			Out:  &out,
			Poll: 10 * time.Millisecond,
		})
		if err == nil {
			acquiredCh <- got
		}
	}()

	time.Sleep(40 * time.Millisecond)
	require.NoError(t, release())

	select {
	case cleanup := <-acquiredCh:
		data, ok := indexlock.ReadLockData(lockPath)
		require.True(t, ok)
		require.Equal(t, LockRoleCLIIndex, data.Role)
		require.NoError(t, cleanup())
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never acquired the released lock")
	}
}

func TestTryAcquireIndexLockRequestsPriority(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	writeHeldLock(t, lockPath, "runtime/embed-cycle")

	var out bytes.Buffer
	_, err := TryAcquireIndexLockWithOptions(context.Background(), lockPath, LockWaitOptions{
		RequestPriority: true,
		Out:             &out,
		Poll:            10 * time.Millisecond,
		Limit:           30 * time.Millisecond,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "runtime/embed-cycle")

	require.False(t, indexlock.CheckPriority(lockPath))
}

func TestCancelingOnePriorityWaiterPreservesTheOther(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	writeHeldLock(t, lockPath, "runtime/embed-cycle")
	firstCleanup, err := indexlock.RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(firstCleanup.Close)
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
		return err == nil && len(entries) == 2
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.True(t, indexlock.CheckPriority(lockPath))
	firstCleanup.Close()
	require.False(t, indexlock.CheckPriority(lockPath))
}

func TestIndexWaiterRetriesPriorityAfterPublicationFailure(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	writeHeldLock(t, lockPath, "runtime/embed-cycle")
	priorityPath := indexlock.PriorityPath(lockPath)
	require.NoError(t, os.WriteFile(priorityPath, []byte("temporary blocker"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	failed := make(chan struct{})
	var once sync.Once
	go func() {
		_, err := TryAcquireIndexLockWithOptions(ctx, lockPath, LockWaitOptions{
			RequestPriority: true, Debug: true, Poll: time.Millisecond,
			Out: priorityWarningWriter(func(text string) {
				if strings.Contains(text, "Warning: failed to request priority") {
					once.Do(func() { close(failed) })
				}
			}),
		})
		done <- err
	}()
	defer func() {
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	}()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("the first priority publication did not report its nonfatal failure")
	}
	require.NoError(t, os.Remove(priorityPath))
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, time.Millisecond,
		"the still-blocked index waiter must retry its own priority request")
}

type priorityWarningWriter func(string)

func (w priorityWarningWriter) Write(p []byte) (int, error) {
	w(string(p))
	return len(p), nil
}

func TestHolderSummaryHandlesUnreadableLockData(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte("{not json"), 0o644))
	require.Contains(t, holderSummary(lockPath), "another process")

	raw, err := json.Marshal(indexlock.LockData{PID: 42})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, raw, 0o644))
	require.Contains(t, holderSummary(lockPath), "unknown pid 42")
}
