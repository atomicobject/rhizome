package indexlock

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPriorityCleanupPreservesEveryActiveRequester(t *testing.T) {
	for _, cleanupFirst := range []int{0, 1} {
		t.Run([]string{"older-first", "newer-first"}[cleanupFirst], func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "index.lock")
			requests := make([]*PriorityRequest, 2)
			for i := range requests {
				var err error
				requests[i], err = RequestPriority(lockPath)
				require.NoError(t, err)
				t.Cleanup(requests[i].Close)
			}
			require.True(t, CheckPriority(lockPath))
			requests[cleanupFirst].Close()
			require.True(t, CheckPriority(lockPath), "the remaining active waiter must keep priority")
			requests[1-cleanupFirst].Close()
			require.False(t, CheckPriority(lockPath))
		})
	}
}

func TestPriorityRequestActiveChecksItsOwnRecord(t *testing.T) {
	for _, state := range []string{"fresh", "expired", "missing", "replaced", "unreadable"} {
		t.Run(state, func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "index.lock")
			request, err := RequestPriority(lockPath)
			require.NoError(t, err)
			t.Cleanup(request.Close)
			peer, err := RequestPriority(lockPath)
			require.NoError(t, err)
			t.Cleanup(peer.Close)
			switch state {
			case "expired":
				expired := time.Now().Add(-PriorityStaleAfter - time.Second)
				require.NoError(t, os.Chtimes(request.path, expired, expired))
			case "missing":
				require.NoError(t, os.Remove(request.path))
			case "replaced":
				owner := request.owner
				owner.Token = "replacement"
				data, err := json.Marshal(owner)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(request.path, data, 0o644))
			case "unreadable":
				require.NoError(t, os.WriteFile(request.path, []byte("{"), 0o644))
			}
			require.Equal(t, state == "fresh", request.Active())
			require.True(t, peer.Active())
			require.True(t, CheckPriority(lockPath), "a peer cannot establish the missing owner's health")
			request.Close()
			require.False(t, request.Active())
			require.True(t, peer.Active(), "closing one owner cannot clear its peer")
		})
	}
	var absent *PriorityRequest
	require.False(t, absent.Active())
	absent.Close()
}

func priorityFixturePath(t *testing.T, lockPath string) string {
	t.Helper()
	dir := PriorityPath(lockPath)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return filepath.Join(dir, rand.Text()+".json")
}

func onlyPriorityRequestPath(t *testing.T, lockPath string) string {
	t.Helper()
	entries, err := os.ReadDir(PriorityPath(lockPath))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	return filepath.Join(PriorityPath(lockPath), entries[0].Name())
}

func TestPriorityCleanupJoinsHeartbeatAndIsIdempotent(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := requestPriority(lockPath, time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	requestPath := onlyPriorityRequestPath(t, lockPath)
	old := time.Now().Add(-time.Minute)
	require.NoError(t, os.Chtimes(requestPath, old, old))
	require.Eventually(t, func() bool {
		info, err := os.Stat(requestPath)
		return err == nil && info.ModTime().After(old)
	}, time.Second, time.Millisecond)
	require.NoError(t, withLockGuard(PriorityPath(lockPath), func() error {
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cleanup.Close()
			}()
		}
		joined := make(chan struct{})
		go func() { wg.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Fatal("cleanup kept refreshing after stop while the guard was busy")
		}
		return nil
	}))
	require.False(t, CheckPriority(lockPath))
	require.NoFileExists(t, requestPath)
}

func TestPriorityHeartbeatAndCleanupFenceReplacedToken(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	path := onlyPriorityRequestPath(t, lockPath)
	owner, ok := ReadLockData(path)
	require.True(t, ok)
	replacement := owner
	replacement.Token = "replacement"
	raw, err := json.Marshal(replacement)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))
	old := time.Now().Add(-time.Minute)
	require.NoError(t, os.Chtimes(path, old, old))
	require.False(t, touchPriorityIfOwner(path, owner))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.WithinDuration(t, old, info.ModTime(), time.Second)
	cleanup.Close()
	got, ok := ReadLockData(path)
	require.True(t, ok)
	require.Equal(t, replacement, got)
}

func TestPriorityPruningKeepsActiveOwnersAndReapsAbandonedRecords(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	activePath := onlyPriorityRequestPath(t, lockPath)
	dir := PriorityPath(lockPath)
	stale := filepath.Join(dir, rand.Text()+".json")
	temp := filepath.Join(dir, "."+rand.Text()+".json.tmp-0123456789abcdef0123456789abcdef")
	malformed := filepath.Join(dir, rand.Text()+".json")
	for _, path := range []string{stale, temp, malformed} {
		require.NoError(t, os.WriteFile(path, []byte("{"), 0o644))
	}
	old := time.Now().Add(-PriorityStaleAfter - time.Second)
	for _, path := range []string{stale, temp} {
		require.NoError(t, os.Chtimes(path, old, old))
	}
	dead := filepath.Join(dir, rand.Text()+".json")
	raw, err := json.Marshal(LockData{PID: deadPID(t), Runtime: testRuntimeIdentity(t)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dead, raw, 0o644))
	require.True(t, CheckPriority(lockPath))
	for _, path := range []string{stale, temp, dead} {
		require.NoFileExists(t, path)
	}
	require.FileExists(t, activePath)
	require.FileExists(t, malformed)
}

func TestConcurrentPriorityLifecyclePreservesAnActiveWaiter(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := requestPriority(lockPath, time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 12 {
				otherCleanup, err := requestPriority(lockPath, time.Millisecond)
				if err != nil {
					t.Error(err)
					return
				}
				if !CheckPriority(lockPath) {
					t.Error("active waiter lost priority")
				}
				otherCleanup.Close()
			}
		}()
	}
	wg.Wait()
	require.True(t, CheckPriority(lockPath))
	cleanup.Close()
	require.False(t, CheckPriority(lockPath))
	entries, err := os.ReadDir(PriorityPath(lockPath))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestPriorityPruningWaitsForAnInFlightRefresh(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	path := onlyPriorityRequestPath(t, lockPath)
	old := time.Now().Add(-PriorityStaleAfter - time.Second)
	require.NoError(t, os.Chtimes(path, old, old))
	checked := make(chan bool, 1)
	require.NoError(t, withLockGuard(PriorityPath(lockPath), func() error {
		go func() { checked <- CheckPriority(lockPath) }()
		select {
		case <-checked:
			t.Fatal("priority scan bypassed the refresh guard")
		case <-time.After(20 * time.Millisecond):
		}
		now := time.Now()
		return os.Chtimes(path, now, now)
	}))
	require.True(t, <-checked)
	require.FileExists(t, path)
}

func TestPriorityCleanupRemainsBoundedWhenTheGuardIsBusy(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	path := onlyPriorityRequestPath(t, lockPath)
	unlock := holdPriorityGuard(t, lockPath)
	done := make(chan struct{})
	go func() { cleanup.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("priority cleanup blocked cancellation behind a paused guard owner")
	}
	unlock()
	require.False(t, CheckPriority(lockPath))
	require.NoFileExists(t, path)
}
