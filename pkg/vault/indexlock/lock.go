package indexlock

// Docs:
// - [[Index lock and background coordination runbook]]
// - [[Indexing pipeline (Hub)]]
// - [[Indexing pipeline - Concurrency + batching requirements]]

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

const releaseRemoveRetryWindow = 200 * time.Millisecond

// PriorityStaleAfter is how long a priority request is valid before being considered stale.
const PriorityStaleAfter = 90 * time.Second

// ErrForeignRuntime indicates that a lock belongs to a different host or PID
// namespace, or that a changed boot identity cannot be proven stale.
var ErrForeignRuntime = errors.New("index lock belongs to a different runtime")

// LockData holds the metadata stored in the lock file
type LockData struct {
	PID     int    `json:"pid"`
	Started string `json:"started"`
	Host    string `json:"host"`
	Runtime string `json:"runtime"`
	Token   string `json:"token,omitempty"`
	// Role names what the holder is doing (SPEC-0104 US6), for example
	// "cli/index" or "runtime/embed-cycle". Empty on locks written before the
	// field existed.
	Role string `json:"role,omitempty"`
	// ProcessLifetime marks a lock the owner holds for its whole process
	// lifetime (the runtime election lock). Such a lock is never reclaimed for
	// heartbeat age: a healthy runtime can sit idle for hours.
	ProcessLifetime bool `json:"processLifetime,omitempty"`
}

// AcquireOptions describes the lock a caller wants to take.
type AcquireOptions struct {
	// Role is recorded in the lock so a waiting process can name the holder.
	Role string
	// ProcessLifetime records that the owner intends to hold the lock for its
	// process lifetime. All locks protect live owners regardless of heartbeat
	// age; foreign runtime identities require explicit operator recovery.
	ProcessLifetime bool
}

// TryAcquire attempts to acquire an index lock non-blockingly.
// It returns a release function to call when done, whether the lock was acquired,
// and any error that occurred.
//
// A dead PID in this runtime or a proven earlier local boot, and old corrupt
// metadata, permit stale-lock recovery. Locks from another host or an uncertain
// runtime are never reclaimed automatically because
// PID identity and heartbeat liveness cannot safely fence a paused owner across
// host/PID namespace boundaries.
func TryAcquire(lockPath string) (release func() error, acquired bool, err error) {
	return TryAcquireWithOptions(lockPath, AcquireOptions{})
}

// TryAcquireWithOptions is TryAcquire with a recorded role and an optional
// process-lifetime mode (SPEC-0104 US6).
func TryAcquireWithOptions(lockPath string, opts AcquireOptions) (release func() error, acquired bool, err error) {
	// WHY: lock creation is intentionally atomic and non-blocking; callers own
	// wait/yield policy so interactive indexing and background refresh can share
	// the same primitive without surprising each other.
	// Docs: [[indexing-workflow#^spec-0036-us3-ac3]]
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("mkdir lock dir: %w", err)
	}

	// Prepare owner metadata before publishing the lock.
	runtimeID, err := runtimeIdentity()
	if err != nil {
		return nil, false, fmt.Errorf("determine lock runtime identity: %w", err)
	}
	lockData := LockData{
		PID:             os.Getpid(),
		Started:         time.Now().UTC().Format(time.RFC3339Nano),
		Host:            getHostname(),
		Runtime:         runtimeID,
		Role:            opts.Role,
		ProcessLifetime: opts.ProcessLifetime,
	}
	lockData.Token = rand.Text()
	lockDataJSON, err := json.Marshal(lockData)
	if err != nil {
		return nil, false, fmt.Errorf("marshal lock data: %w", err)
	}

	// Guard creation through metadata publication.
	release, acquired, err = createLockUnderGuard(lockPath, lockData, lockDataJSON)
	if acquired || err == nil {
		return release, acquired, err
	}

	if !os.IsExist(err) {
		// Unexpected error
		return nil, false, fmt.Errorf("create lock file: %w", err)
	}

	// Lock file exists; check if it's stale
	existing, err := fileio.ReadFile(lockPath)
	if err != nil {
		// If the lock was removed between IsExist and now, retry once.
		if os.IsNotExist(err) {
			release, acquired, createErr := createLockUnderGuard(lockPath, lockData, lockDataJSON)
			if os.IsExist(createErr) {
				return nil, false, nil
			}
			return release, acquired, createErr
		}
		// Don't delete an unreadable lock file; treat as held.
		return nil, false, nil
	}

	// Parse existing lock data
	var existingData LockData
	if err := json.Unmarshal(existing, &existingData); err != nil {
		// Treat fresh malformed metadata as held. Existing corrupt locks keep
		// their recovery grace even though new metadata is published whole.
		if info, statErr := os.Stat(lockPath); statErr == nil {
			age := time.Since(info.ModTime())
			if age < 2*time.Second {
				return nil, false, nil
			}
			// If it's been corrupted for a while, assume stale and try to recover.
			if age < 10*time.Minute {
				return nil, false, nil
			}
		} else {
			// Can't stat; treat as held.
			return nil, false, nil
		}
		return acquireAfterStale(lockPath, lockData, lockDataJSON, func(current []byte) bool {
			if !bytes.Equal(current, existing) {
				return false
			}
			info, statErr := os.Stat(lockPath)
			return statErr == nil && time.Since(info.ModTime()) >= 10*time.Minute
		})
	}

	if existingData.Runtime == "" || existingData.Runtime != runtimeID {
		if lockFromPreviousBoot(existingData, runtimeID) {
			return acquireAfterStale(lockPath, lockData, lockDataJSON, func(current []byte) bool {
				var owner LockData
				return json.Unmarshal(current, &owner) == nil && sameLockOwner(owner, existingData) && lockFromPreviousBoot(owner, runtimeID)
			})
		}
		return nil, false, fmt.Errorf(
			"%w: lock host=%q runtime=%q current host=%q runtime=%q pid=%d; cannot safely verify pid across host, boot, or container namespaces; verify no Rhizome process is active before removing %s",
			ErrForeignRuntime,
			existingData.Host,
			existingData.Runtime,
			lockData.Host,
			runtimeID,
			existingData.PID,
			lockPath,
		)
	}

	// Check if the PID in the lock file is still alive
	if !pidExists(existingData.PID) {
		return acquireAfterStale(lockPath, lockData, lockDataJSON, func(current []byte) bool {
			var currentData LockData
			return json.Unmarshal(current, &currentData) == nil && currentData.Runtime == runtimeID && !pidExists(currentData.PID)
		})
	}

	// Heartbeat age cannot fence a paused live owner. Only a dead PID proves
	// that the old writer cannot resume after another process takes this lock.
	return nil, false, nil
}

func createLockUnderGuard(lockPath string, owner LockData, ownerJSON []byte) (func() error, bool, error) {
	var createErr error
	created := false
	err := withLockGuard(lockPath, func() error {
		err := publishLockMetadata(lockPath, ownerJSON)
		if err != nil {
			createErr = err
			return nil
		}
		created = true
		return nil
	})
	if errors.Is(err, errGuardBusy) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if createErr != nil {
		return nil, false, createErr
	}
	if !created {
		return nil, false, nil
	}
	return func() error { return removeLockFileIfOwner(lockPath, owner) }, true, nil
}

// Callers guard a shared final path or supply an independent unique-token path.
// A crash before rename leaves an inert temporary file, never an empty lock.
func publishLockMetadata(lockPath string, ownerJSON []byte) error {
	if _, err := os.Lstat(lockPath); err == nil {
		return &os.PathError{Op: "create", Path: lockPath, Err: os.ErrExist}
	} else if !os.IsNotExist(err) {
		return err
	}
	tempPath, err := prepareLockMetadata(lockPath, ownerJSON)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	// Recheck absence before publishing the complete metadata.
	if _, err := os.Lstat(lockPath); err == nil {
		return &os.PathError{Op: "create", Path: lockPath, Err: os.ErrExist}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tempPath, lockPath)
}

// The caller has revalidated an existing stale owner under its guard. Replace
// its namespace entry directly so concurrent opens never cross a removal gap.
func replaceLockMetadata(lockPath string, ownerJSON []byte) error {
	tempPath, err := prepareLockMetadata(lockPath, ownerJSON)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	return fileio.Replace(tempPath, lockPath)
}

func prepareLockMetadata(lockPath string, ownerJSON []byte) (tempPath string, err error) {
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("name lock metadata temp: %w", err)
	}
	tempPath = filepath.Join(filepath.Dir(lockPath), "."+filepath.Base(lockPath)+".tmp-"+hex.EncodeToString(suffix[:]))
	// O_EXCL with 0644 retains the caller's umask, as the old final-path
	// creation did. CreateTemp followed by Chmod would bypass that mask.
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tempPath)
		}
	}()
	if n, writeErr := file.Write(ownerJSON); writeErr != nil || n != len(ownerJSON) {
		_ = file.Close()
		if writeErr == nil {
			writeErr = fmt.Errorf("short write: wrote %d of %d bytes", n, len(ownerJSON))
		}
		return tempPath, fmt.Errorf("write lock metadata: %w", writeErr)
	}
	if err := file.Close(); err != nil {
		return tempPath, fmt.Errorf("close lock metadata: %w", err)
	}
	return tempPath, nil
}

func removeLockFileIfOwner(lockPath string, owner LockData) error {
	for {
		err := withLockGuard(lockPath, func() error {
			current, err := fileio.ReadFile(lockPath)
			if err != nil {
				return err
			}
			var currentData LockData
			if err := json.Unmarshal(current, &currentData); err != nil {
				return err
			}
			if !sameLockOwner(currentData, owner) {
				return fmt.Errorf("index lock is now owned by pid=%d host=%s started=%s", currentData.PID, currentData.Host, currentData.Started)
			}
			return removeLockFile(lockPath)
		})
		if !errors.Is(err, errGuardBusy) {
			return err
		}
		// Release is mandatory. A bounded best-effort attempt could leave a
		// live owner's lock in place forever now that age never steals it.
		time.Sleep(10 * time.Millisecond)
	}
}

func acquireAfterStale(lockPath string, owner LockData, ownerJSON []byte, stillStale func([]byte) bool) (func() error, bool, error) {
	var release func() error
	var acquired bool
	err := withLockGuard(lockPath, func() error {
		current, err := fileio.ReadFile(lockPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		} else if !stillStale(current) {
			return nil
		}
		var publishErr error
		if err == nil {
			publishErr = replaceLockMetadata(lockPath, ownerJSON)
		} else {
			publishErr = publishLockMetadata(lockPath, ownerJSON)
		}
		if publishErr != nil {
			if os.IsExist(publishErr) {
				return nil
			}
			return publishErr
		}
		release = func() error { return removeLockFileIfOwner(lockPath, owner) }
		acquired = true
		return nil
	})
	if errors.Is(err, errGuardBusy) {
		return nil, false, nil
	}
	return release, acquired, err
}

func withLockGuard(lockPath string, fn func() error) error {
	// Keep the sidecar permanently. Unlinking a locked file lets another
	// process open a new inode and enter the critical section at the same time.
	guardPath := lockPath + ".guard.lock"
	file, err := os.OpenFile(guardPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	deadline := time.Now().Add(releaseRemoveRetryWindow)
	for {
		err := tryLockGuardFile(file)
		if err == nil {
			defer unlockGuardFile(file)
			return fn()
		}
		if errors.Is(err, errGuardBusy) {
			if time.Now().After(deadline) {
				return errGuardBusy
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		return err
	}
}

func sameLockOwner(a, b LockData) bool {
	return a.PID == b.PID && a.Host == b.Host && a.Runtime == b.Runtime && a.Started == b.Started && a.Token == b.Token
}

func removeLockFile(lockPath string) error {
	deadline := time.Now().Add(releaseRemoveRetryWindow)
	for {
		err := os.Remove(lockPath)
		if err == nil {
			return nil
		}
		if os.IsNotExist(err) {
			return err
		}
		if !isRetryableRemoveError(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isRetryableRemoveError(err error) bool {
	if err == nil {
		return false
	}
	if runtime.GOOS != "windows" {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "used by another process") ||
		strings.Contains(msg, "cannot access the file")
}

func getHostname() string {
	h, _ := os.Hostname()
	return h
}

// shortHostname is the lowercase first label of the hostname. Runtime identity
// uses it so that a DNS suffix appearing or disappearing ("mac" vs
// "mac.local") does not make this host's own locks look foreign.
func shortHostname() string {
	host := normalizeShortHostname(getHostname())
	return host
}

func normalizeShortHostname(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if idx := strings.IndexByte(host, '.'); idx > 0 {
		host = host[:idx]
	}
	return host
}

// ReadLockData reads the current holder's metadata, if any.
func ReadLockData(lockPath string) (LockData, bool) {
	raw, err := fileio.ReadFile(lockPath)
	if err != nil {
		return LockData{}, false
	}
	var data LockData
	if err := json.Unmarshal(raw, &data); err != nil {
		return LockData{}, false
	}
	return data, true
}

// LastHeartbeat is when the holder last touched the lock file.
func LastHeartbeat(lockPath string) (time.Time, bool) {
	info, err := os.Stat(lockPath)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// touchIfOwner refreshes the heartbeat only while this process still owns the
// lock. A reclaimed or replaced lock must never be kept alive by its previous
// owner. It reports whether ownership still holds.
func touchIfOwner(lockPath string, owner LockData) bool {
	held := false
	err := withLockGuard(lockPath, func() error {
		data, ok := ReadLockData(lockPath)
		if !ok || !sameLockOwner(data, owner) || data.PID != os.Getpid() {
			return nil
		}
		if id, err := runtimeIdentity(); err != nil || data.Runtime != id {
			return nil
		}
		now := time.Now()
		if err := os.Chtimes(lockPath, now, now); err != nil {
			return err
		}
		held = true
		return nil
	})
	// A busy guard means a short ownership transition is in progress. Try on
	// the next heartbeat rather than declaring this owner lost without reading it.
	return held || errors.Is(err, errGuardBusy)
}

// PIDExists reports whether pid names a live process using the platform's
// real liveness check (signal 0 on Unix, a process handle and exit code on
// Windows). Other packages use it instead of os.FindProcess, which always
// succeeds on Windows.
func PIDExists(pid int) bool { return pidExists(pid) }
