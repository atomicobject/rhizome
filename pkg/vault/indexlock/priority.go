package indexlock

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// PriorityPath returns the directory containing requests for this lock.
func PriorityPath(lockPath string) string {
	return strings.TrimSuffix(lockPath, ".lock") + ".priority"
}

// PriorityRequest owns one waiter's record and heartbeat. Call Close when the
// waiter acquires the lock or exits; Active checks only this owned request.
type PriorityRequest struct {
	path    string
	owner   LockData
	stop    <-chan struct{}
	cleanup func()
}

// Close joins the heartbeat and removes only this request's owner record.
// It is safe to call more than once or concurrently.
func (r *PriorityRequest) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// Active reports whether this request still owns a fresh, regular record.
// Missing, expired, replaced, or unreadable records require caller renewal;
// another waiter's record cannot establish this request's health.
func (r *PriorityRequest) Active() bool {
	if r == nil {
		return false
	}
	select {
	case <-r.stop:
		return false
	default:
	}
	if err := checkPriorityDirectory(filepath.Dir(r.path)); err != nil {
		return false
	}
	info, err := os.Lstat(r.path)
	if err != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) > PriorityStaleAfter {
		return false
	}
	owner, ok := ReadLockData(r.path)
	return ok && sameLockOwner(owner, r.owner)
}

// RequestPriority registers one independently owned waiter.
func RequestPriority(lockPath string) (*PriorityRequest, error) {
	return requestPriority(lockPath, PriorityStaleAfter/3)
}

func requestPriority(lockPath string, interval time.Duration) (*PriorityRequest, error) {
	priorityDir := PriorityPath(lockPath)
	if err := os.MkdirAll(filepath.Dir(priorityDir), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir priority parent: %w", err)
	}
	runtimeID, err := runtimeIdentity()
	if err != nil {
		return nil, fmt.Errorf("determine priority runtime identity: %w", err)
	}
	owner := LockData{
		PID:     os.Getpid(),
		Started: time.Now().UTC().Format(time.RFC3339Nano),
		Host:    getHostname(),
		Runtime: runtimeID,
		Token:   rand.Text(),
	}
	data, err := json.Marshal(owner)
	if err != nil {
		return nil, fmt.Errorf("marshal priority data: %w", err)
	}
	requestPath := filepath.Join(priorityDir, owner.Token+".json")
	if err := os.Mkdir(priorityDir, 0o755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("mkdir priority directory: %w", err)
	}
	if err := checkPriorityDirectory(priorityDir); err != nil {
		return nil, err
	}
	if err := publishLockMetadata(requestPath, data); err != nil {
		return nil, fmt.Errorf("publish priority request: %w", err)
	}

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// A ready tick must not start another guarded refresh after Close.
				select {
				case <-stop:
					return
				default:
				}
				if !touchPriorityIfOwner(requestPath, owner) {
					return
				}
			}
		}
	}()
	return &PriorityRequest{
		path: requestPath, owner: owner, stop: stop,
		cleanup: sync.OnceFunc(func() {
			close(stop)
			<-done
			_ = removePriorityIfOwner(requestPath, owner)
		}),
	}, nil
}

func touchPriorityIfOwner(requestPath string, owner LockData) bool {
	held := false
	err := withLockGuard(filepath.Dir(requestPath), func() error {
		if err := checkPriorityDirectory(filepath.Dir(requestPath)); err != nil {
			return err
		}
		current, ok := ReadLockData(requestPath)
		if !ok || !sameLockOwner(current, owner) {
			return nil
		}
		now := time.Now()
		if err := os.Chtimes(requestPath, now, now); err != nil {
			return err
		}
		held = true
		return nil
	})
	return held || errors.Is(err, errGuardBusy)
}

func removePriorityIfOwner(requestPath string, owner LockData) error {
	if err := checkPriorityDirectory(filepath.Dir(requestPath)); err != nil {
		return err
	}
	current, ok := ReadLockData(requestPath)
	if !ok || !sameLockOwner(current, owner) {
		return nil
	}
	return removeLockFile(requestPath)
}

// CheckPriority reports whether any live, fresh waiter requests this lock.
// Requests in this process count, including browser saves beside background work.
func CheckPriority(lockPath string) bool {
	priorityDir := PriorityPath(lockPath)
	if err := checkPriorityDirectory(priorityDir); err != nil {
		return false
	}
	runtimeID, _ := runtimeIdentity()
	active := false
	err := withLockGuard(priorityDir, func() error {
		var err error
		active, err = scanPriorityRequests(priorityDir, runtimeID, true)
		return err
	})
	if errors.Is(err, errGuardBusy) {
		active, _ = scanPriorityRequests(priorityDir, runtimeID, false)
	}
	return active
}

func checkPriorityDirectory(priorityDir string) error {
	info, err := os.Lstat(priorityDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &os.PathError{Op: "open priority directory", Path: priorityDir, Err: syscall.ENOTDIR}
	}
	return nil
}

func scanPriorityRequests(priorityDir, runtimeID string, prune bool) (bool, error) {
	if err := checkPriorityDirectory(priorityDir); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(priorityDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	active := false
	for _, entry := range entries {
		request := isPriorityRequestName(entry.Name())
		if !request && !isPriorityTemporaryName(entry.Name()) {
			continue
		}
		path := filepath.Join(priorityDir, entry.Name())
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if time.Since(info.ModTime()) > PriorityStaleAfter {
			if prune {
				_ = removeLockFile(path)
			}
			continue
		}
		if !request {
			continue
		}
		owner, ok := ReadLockData(path)
		if !ok {
			continue
		}
		if runtimeID != "" && owner.Runtime == runtimeID && !pidExists(owner.PID) {
			if prune {
				_ = removeLockFile(path)
			}
			continue
		}
		active = true
	}
	return active, nil
}

func isPriorityRequestName(name string) bool {
	token, ok := strings.CutSuffix(name, ".json")
	return ok && len(token) >= 26 && strings.Trim(token, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == ""
}

func isPriorityTemporaryName(name string) bool {
	if !strings.HasPrefix(name, ".") {
		return false
	}
	request, suffix, ok := strings.Cut(name[1:], ".tmp-")
	if !ok || !isPriorityRequestName(request) || len(suffix) != 32 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}
