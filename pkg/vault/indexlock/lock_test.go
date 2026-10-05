package indexlock

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testRuntimeIdentity(t *testing.T) string {
	t.Helper()
	id, err := runtimeIdentity()
	require.NoError(t, err)
	return id
}

func TestTryAcquire_FirstAcquire(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// First acquire should succeed
	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)

	// Lock file should exist
	require.FileExists(t, lockPath)

	// Read and verify lock data
	data, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	var lockData LockData
	err = json.Unmarshal(data, &lockData)
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), lockData.PID)
	require.NotEmpty(t, lockData.Started)

	// Release the lock
	err = release()
	require.NoError(t, err)
	require.NoFileExists(t, lockPath)
}

func TestTryAcquire_Blocked(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// First acquire succeeds
	release1, acquired1, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired1)

	// Second acquire should fail (lock held by our PID)
	release2, acquired2, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.False(t, acquired2, "second acquire should fail - lock is held by our PID")
	require.Nil(t, release2)

	// Release first lock
	err = release1()
	require.NoError(t, err)

	// Now we should be able to acquire
	release3, acquired3, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired3)
	_ = release3()
}

func TestTryAcquire_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	const workers = 8
	var acquiredCount int32
	errCh := make(chan error, workers)
	releaseCh := make(chan func() error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			release, acquired, err := TryAcquire(lockPath)
			if err != nil {
				errCh <- err
				return
			}
			if acquired {
				atomic.AddInt32(&acquiredCount, 1)
				releaseCh <- release
			}
			errCh <- nil
		}()
	}

	wg.Wait()
	close(errCh)
	close(releaseCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), acquiredCount)
	for release := range releaseCh {
		require.NoError(t, release())
	}

	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, release())
}

func TestTryAcquire_StaleLockDetection(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// Create a stale lock file with a dead PID.
	// Use a very high PID (999999) that's extremely unlikely to exist.
	// PID 2 might exist on some Unix systems (kernel threads, init), so we use a high number.
	stalePID := 999999

	staleLockData := LockData{
		PID:     stalePID,
		Started: time.Now().UTC().Format(time.RFC3339),
		Host:    getHostname(),
		Runtime: testRuntimeIdentity(t),
	}
	data, err := json.Marshal(staleLockData)
	require.NoError(t, err)
	err = os.WriteFile(lockPath, data, 0o644)
	require.NoError(t, err)

	// Try to acquire; should detect stale lock and succeed
	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired, "should acquire stale lock")
	require.NotNil(t, release)

	// Verify the lock file has our PID now
	newData, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	var newLockData LockData
	err = json.Unmarshal(newData, &newLockData)
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), newLockData.PID)

	_ = release()
}

func TestTryAcquire_DoesNotProbeOrReclaimForeignRuntimeLock(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	foreignLock := LockData{
		PID:     999999,
		Started: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Runtime: testRuntimeIdentity(t) + "-foreign",
		Token:   "foreign-owner",
	}
	data, err := json.Marshal(foreignLock)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, data, 0o644))
	release, acquired, err := TryAcquire(lockPath)
	require.ErrorIs(t, err, ErrForeignRuntime)
	require.ErrorContains(t, err, "cannot safely verify pid")
	require.False(t, acquired)
	require.Nil(t, release)

	got, readErr := os.ReadFile(lockPath)
	require.NoError(t, readErr)
	require.JSONEq(t, string(data), string(got))
}

func TestTryAcquire_DoesNotReclaimStaleLegacyLockWithoutRuntimeIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	legacyLock := LockData{
		PID:     999999,
		Started: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Token:   "legacy-owner",
	}
	data, err := json.Marshal(legacyLock)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, data, 0o644))
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, os.Chtimes(lockPath, old, old))

	release, acquired, err := TryAcquire(lockPath)
	require.ErrorIs(t, err, ErrForeignRuntime)
	require.False(t, acquired)
	require.Nil(t, release)
	require.FileExists(t, lockPath)
}

func TestTryAcquire_DoesNotReclaimPausedForeignRuntimeOwner(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	foreignLock := LockData{
		PID:     os.Getpid(),
		Started: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Runtime: testRuntimeIdentity(t) + "-foreign",
		Token:   "foreign-owner",
	}
	data, err := json.Marshal(foreignLock)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, data, 0o644))
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, os.Chtimes(lockPath, old, old))

	release, acquired, err := TryAcquire(lockPath)
	require.ErrorIs(t, err, ErrForeignRuntime)
	require.False(t, acquired)
	require.Nil(t, release)
	require.FileExists(t, lockPath)
}

func TestTryAcquire_CorruptedLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// Create a corrupted lock file
	err := os.WriteFile(lockPath, []byte("not valid json"), 0o644)
	require.NoError(t, err)
	// Make it "old" so TryAcquire treats it as stale/corrupt and recovers.
	old := time.Now().Add(-11 * time.Minute)
	require.NoError(t, os.Chtimes(lockPath, old, old))

	// Try to acquire; should detect corrupted lock and succeed
	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)

	_ = release()
}

func TestTryAcquire_CreatesMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "nested", "dir", "index.lock")

	// Try to acquire; should create the directory
	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)

	require.FileExists(t, lockPath)
	_ = release()
}

func TestTryAcquire_MultipleReleases(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)

	// First release should succeed
	err = release()
	require.NoError(t, err)

	// Second release should fail gracefully (file already deleted)
	err = release()
	require.Error(t, err) // os.Remove on non-existent file returns an error
}

func TestReleaseDoesNotRemoveLockAfterStaleTakeover(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	oldOwner := LockData{
		PID:     os.Getpid(),
		Started: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Token:   "old-owner",
	}
	newOwner := LockData{
		PID:     os.Getpid(),
		Started: time.Now().UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Token:   "new-owner",
	}
	newData, err := json.Marshal(newOwner)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, newData, 0o644))

	err = removeLockFileIfOwner(lockPath, oldOwner)
	require.Error(t, err)
	require.FileExists(t, lockPath)

	gotData, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	var got LockData
	require.NoError(t, json.Unmarshal(gotData, &got))
	require.Equal(t, newOwner, got)
}

func TestPriorityPath(t *testing.T) {
	require.Equal(t, "/path/to/index.priority", PriorityPath("/path/to/index.lock"))
	require.Equal(t, "foo.priority", PriorityPath("foo.lock"))
	require.Equal(t, "nolock.priority", PriorityPath("nolock"))
}

func TestRequestPriority(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup.Close)

	priorityPath := onlyPriorityRequestPath(t, lockPath)
	require.FileExists(t, priorityPath)

	// Verify priority data
	data, err := os.ReadFile(priorityPath)
	require.NoError(t, err)
	var priorityData LockData
	err = json.Unmarshal(data, &priorityData)
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), priorityData.PID)
	require.NotEmpty(t, priorityData.Started)

	// Cleanup should remove the file
	cleanup.Close()
	require.NoFileExists(t, priorityPath)
}

func TestCheckPriority_NoPriorityFile(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// No priority file exists
	require.False(t, CheckPriority(lockPath))
}

func TestCheckPriority_StalePriorityFile(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	priorityPath := priorityFixturePath(t, lockPath)

	// Create a stale priority file (old mtime)
	data := LockData{
		PID:     999999, // Non-existent PID
		Started: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		Host:    "test-host",
	}
	dataJSON, _ := json.Marshal(data)
	err := os.WriteFile(priorityPath, dataJSON, 0o644)
	require.NoError(t, err)

	// Make the file stale
	old := time.Now().Add(-PriorityStaleAfter - time.Second)
	require.NoError(t, os.Chtimes(priorityPath, old, old))

	// CheckPriority should return false and clean up the stale file
	require.False(t, CheckPriority(lockPath))
	require.NoFileExists(t, priorityPath)
}

func TestCheckPriority_RequestSurvivesSingleHeartbeatInterval(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	priorityPath := priorityFixturePath(t, lockPath)

	data := LockData{
		PID:     os.Getppid(),
		Started: time.Now().UTC().Format(time.RFC3339Nano),
		Host:    "test-host",
	}
	dataJSON, _ := json.Marshal(data)
	err := os.WriteFile(priorityPath, dataJSON, 0o644)
	require.NoError(t, err)

	old := time.Now().Add(-31 * time.Second)
	require.NoError(t, os.Chtimes(priorityPath, old, old))

	require.True(t, CheckPriority(lockPath))
	require.FileExists(t, priorityPath)
}

func TestCheckPriority_OwnPID(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// Create a priority file for our own PID
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	defer cleanup.Close()

	// Interactive work in this process must also preempt background jobs.
	require.True(t, CheckPriority(lockPath))
}

func TestCheckPriority_DeadPID(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	priorityPath := priorityFixturePath(t, lockPath)

	// Create a priority file with a dead PID
	data := LockData{
		PID:     999999, // Non-existent PID
		Started: time.Now().UTC().Format(time.RFC3339),
		Host:    getHostname(),
		Runtime: testRuntimeIdentity(t),
	}
	dataJSON, _ := json.Marshal(data)
	err := os.WriteFile(priorityPath, dataJSON, 0o644)
	require.NoError(t, err)

	// CheckPriority should return false and clean up the dead PID file
	require.False(t, CheckPriority(lockPath))
	require.NoFileExists(t, priorityPath)
}

func TestCheckPriority_ForeignHostDoesNotProbePIDNamespace(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	priorityPath := priorityFixturePath(t, lockPath)

	data := LockData{
		PID:     os.Getpid(),
		Started: time.Now().UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Runtime: testRuntimeIdentity(t) + "-foreign",
	}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(priorityPath, dataJSON, 0o644))

	require.True(t, CheckPriority(lockPath))
	require.FileExists(t, priorityPath)
}

func TestStartYieldingHeartbeat_YieldsOnPriority(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// Acquire the lock first
	release, acquired, err := TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()

	// Start yielding heartbeat with short interval for testing.
	// Use 100ms interval to balance test speed with CI reliability.
	yielded := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const heartbeatInterval = 100 * time.Millisecond
	stop := StartYieldingHeartbeat(ctx, lockPath, heartbeatInterval, func() {
		close(yielded)
	})
	defer stop()

	// Simulate another process requesting priority (use a fake PID that "exists")
	// Use our parent process PID - it's guaranteed to exist while we're running
	// and works across all platforms (Unix and Windows)
	priorityPath := priorityFixturePath(t, lockPath)
	data := LockData{
		PID:     os.Getppid(), // Parent process - guaranteed to exist
		Started: time.Now().UTC().Format(time.RFC3339),
		Host:    "test-host",
	}
	dataJSON, _ := json.Marshal(data)
	err = os.WriteFile(priorityPath, dataJSON, 0o644)
	require.NoError(t, err)

	// Wait for the yield callback with generous timeout for slow CI runners.
	// The heartbeat should detect the priority file within a few intervals.
	select {
	case <-yielded:
		// Success - yield was called
	case <-time.After(5 * time.Second):
		t.Fatal("yield callback was not called within timeout")
	}
}
