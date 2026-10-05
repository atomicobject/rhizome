package indexlock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestStaleRecheckDoesNotRemoveSuccessor(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	successor := []byte(`{"pid":123,"token":"successor"}`)
	if err := os.WriteFile(lockPath, successor, 0o644); err != nil {
		t.Fatal(err)
	}
	owner := LockData{PID: os.Getpid(), Token: "new-owner"}
	ownerJSON, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte{}
	// The first read was empty. The guarded recheck must compare its bytes
	// with the current successor even when the first snapshot has length zero.
	_, acquired, err := acquireAfterStale(lockPath, owner, ownerJSON, func(current []byte) bool {
		return bytes.Equal(current, expected)
	})
	if err != nil || acquired {
		t.Fatalf("successor must block stale takeover: err=%v acquired=%v", err, acquired)
	}
	if raw, err := os.ReadFile(lockPath); err != nil || string(raw) != string(successor) {
		t.Fatalf("successor changed: %q, %v", raw, err)
	}
}

func TestExistingLockPathIsNotReplaced(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	if err := os.WriteFile(lockPath, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := TryAcquire(lockPath); err != nil || acquired {
		t.Fatalf("fresh corrupt lock must stay held: err=%v acquired=%v", err, acquired)
	}
	if raw, err := os.ReadFile(lockPath); err != nil || string(raw) != "corrupt" {
		t.Fatalf("existing lock changed: %q, %v", raw, err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", lockPath); err != nil {
		// Windows ERROR_PRIVILEGE_NOT_HELD is 1314 when symlinks are disabled.
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314))) {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, acquired, err := TryAcquire(lockPath); err != nil || acquired {
		t.Fatalf("dangling symlink must stay held: err=%v acquired=%v", err, acquired)
	}
	if target, err := os.Readlink(lockPath); err != nil || target != "missing-target" {
		t.Fatalf("existing symlink changed: %q, %v", target, err)
	}
}

func TestRuntimeIdentityUsesShortLowercaseHostname(t *testing.T) {
	cases := map[string]string{
		"Mac.local":            "mac",
		"HOST.example.com":     "host",
		"  Spaced.Domain.Net ": "spaced",
		"plain":                "plain",
		".leading":             ".leading",
	}
	for input, want := range cases {
		if got := normalizeShortHostname(input); got != want {
			t.Fatalf("normalizeShortHostname(%q) = %q, want %q", input, got, want)
		}
	}

	identity, err := runtimeIdentity()
	if err != nil {
		t.Fatalf("runtime identity: %v", err)
	}
	if identity == "" {
		t.Fatal("runtime identity must not be empty")
	}
	// Every supported platform folds the boot into the identity, so a lock from
	// before the last reboot is reclaimable.
	if identity == normalizeShortHostname(getHostname()) {
		t.Fatalf("runtime identity %q carries no boot component", identity)
	}
}

func TestTryAcquireWithRoleRoundTrips(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	release, acquired, err := TryAcquireWithOptions(lockPath, AcquireOptions{Role: "cli/index"})
	if err != nil || !acquired {
		t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
	}
	data, ok := ReadLockData(lockPath)
	if !ok {
		t.Fatal("lock data unreadable")
	}
	if data.Role != "cli/index" {
		t.Fatalf("role = %q, want cli/index", data.Role)
	}
	if data.ProcessLifetime {
		t.Fatal("an ordinary lock is not process-lifetime")
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestLiveLockIgnoresHeartbeatAge(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "runtime.lock")
	release, acquired, err := TryAcquireWithOptions(lockPath, AcquireOptions{Role: "runtime/election", ProcessLifetime: true})
	if err != nil || !acquired {
		t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
	}
	defer func() { _ = release() }()

	// Age the lock far past the heartbeat threshold. The owning PID is this
	// live process, so the lock must stay held.
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("age lock: %v", err)
	}
	if _, acquired, err := TryAcquireWithOptions(lockPath, AcquireOptions{ProcessLifetime: true}); err != nil || acquired {
		t.Fatalf("a live process-lifetime lock must not be reclaimed for age: err=%v acquired=%v", err, acquired)
	}
	// The flag is persisted, so even a caller that forgets it must not reclaim.
	if _, acquired, err := TryAcquire(lockPath); err != nil || acquired {
		t.Fatalf("persisted process-lifetime flag must protect the lock: err=%v acquired=%v", err, acquired)
	}

	// A paused ordinary writer can resume too, so age cannot reclaim its lock.
	ordinaryPath := filepath.Join(filepath.Dir(lockPath), "index.lock")
	ordinaryRelease, acquired, err := TryAcquire(ordinaryPath)
	if err != nil || !acquired {
		t.Fatalf("acquire ordinary: err=%v acquired=%v", err, acquired)
	}
	defer func() { _ = ordinaryRelease() }()
	if err := os.Chtimes(ordinaryPath, old, old); err != nil {
		t.Fatalf("age ordinary lock: %v", err)
	}
	_, acquired, err = TryAcquire(ordinaryPath)
	if err != nil || acquired {
		t.Fatalf("a paused ordinary lock must stay held: err=%v acquired=%v", err, acquired)
	}
}

func TestProcessLifetimeLockIsReclaimedFromADeadPID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "runtime.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	identity, err := runtimeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	dead := LockData{PID: deadPID(t), Started: time.Now().UTC().Format(time.RFC3339Nano), Host: getHostname(), Runtime: identity, ProcessLifetime: true}
	dead.Token = dead.Started
	raw, err := json.Marshal(dead)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	release, acquired, err := TryAcquireWithOptions(lockPath, AcquireOptions{ProcessLifetime: true})
	if err != nil || !acquired {
		t.Fatalf("a dead owner's process-lifetime lock must be reclaimed: err=%v acquired=%v", err, acquired)
	}
	_ = release()
}

func TestHeartbeatStopsTouchingALockItNoLongerOwns(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	release, acquired, err := TryAcquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
	}
	defer func() { _ = release() }()

	owner, ok := ReadLockData(lockPath)
	if !ok || !touchIfOwner(lockPath, owner) {
		t.Fatal("the owner must be able to heartbeat its own lock")
	}

	// Another process reclaimed the lock: the file no longer names this PID.
	foreign := LockData{PID: deadPID(t), Started: time.Now().UTC().Format(time.RFC3339Nano), Host: "other", Runtime: "other-runtime"}
	raw, err := json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if touchIfOwner(lockPath, owner) {
		t.Fatal("a former owner must not heartbeat a reclaimed lock")
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().After(old.Add(time.Minute)) {
		t.Fatal("the reclaimed lock's heartbeat was refreshed by its former owner")
	}
}

func TestHeartbeatDoesNotTouchSuccessorInSameProcess(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	release, acquired, err := TryAcquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
	}
	owner, ok := ReadLockData(lockPath)
	if !ok {
		t.Fatal("lock metadata unreadable")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	successorRelease, acquired, err := TryAcquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("successor acquire: err=%v acquired=%v", err, acquired)
	}
	defer successorRelease()
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if touchIfOwner(lockPath, owner) {
		t.Fatal("old owner touched its successor's lock")
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().After(old.Add(time.Second)) {
		t.Fatal("old heartbeat refreshed the successor lock")
	}
}

func TestGuardProtectsPausedCreatorAndRecoversAfterClose(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	guardPath := lockPath + ".guard.lock"
	file, err := os.OpenFile(guardPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := tryLockGuardFile(file); err != nil {
		t.Fatal(err)
	}
	// Simulate a creator paused while its metadata is still incomplete.
	if err := os.WriteFile(lockPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := TryAcquire(lockPath); err != nil || acquired {
		t.Fatalf("paused creator must not lose lock: err=%v acquired=%v", err, acquired)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	// Closing the handle, including after a process crash, drops the guard.
	release, acquired, err := TryAcquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("orphaned corrupt lock must recover: err=%v acquired=%v", err, acquired)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseWaitsForBusyGuardAndRemovesLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	release, acquired, err := TryAcquire(lockPath)
	if err != nil || !acquired {
		t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
	}
	file, err := os.OpenFile(lockPath+".guard.lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tryLockGuardFile(file); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() { released <- release() }()
	select {
	case err := <-released:
		t.Fatalf("release bypassed held guard: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("owner lock disappeared: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-released:
		if err != nil {
			t.Fatalf("release after guard clears: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("release stayed blocked after guard cleared")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("owner lock remained after release: %v", err)
	}
}

func TestYieldingHeartbeatYieldsWithinItsPollInterval(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	release, acquired, err := TryAcquireWithOptions(lockPath, AcquireOptions{Role: "runtime/embed-cycle"})
	if err != nil || !acquired {
		t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
	}
	defer func() { _ = release() }()

	priorityPath := priorityFixturePath(t, lockPath)
	foreign := LockData{PID: 999999, Host: "other", Runtime: "other-runtime", Started: time.Now().UTC().Format(time.RFC3339Nano)}
	raw, err := json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(priorityPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	yielded := make(chan time.Time, 1)
	started := time.Now()
	stop := StartYieldingHeartbeat(context.Background(), lockPath, time.Second, func() { yielded <- time.Now() })
	defer stop()

	select {
	case at := <-yielded:
		if elapsed := at.Sub(started); elapsed > 2*time.Second {
			t.Fatalf("yield took %s, want at most one poll interval plus slack", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("holder did not yield to a priority request")
	}
}

// deadPID returns a PID that is not running. Values this high are not in use
// on the platforms this repository supports.
func deadPID(t *testing.T) int {
	t.Helper()
	for pid := 4194303; pid > 100000; pid -= 7919 {
		if !pidExists(pid) {
			return pid
		}
	}
	t.Fatal("no dead PID available")
	return 0
}
