package ontology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// waitEditRecoveryWriteLease lets a newly elected runtime wait for a writer
// from the previous runtime to finish before it reads or replays journals.
// Ordinary edit commits still use nonblocking acquisition below.
func waitEditRecoveryWriteLease(ctx context.Context, vaultPaths *paths.VaultPaths) (func() error, error) {
	lockPath := filepath.Join(vaultPaths.Root(), ".rhizome", "index.lock")
	var priority *indexlock.PriorityRequest
	defer func() { priority.Close() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, indexlock.AcquireOptions{Role: "startup/edit-recovery"})
		if err != nil {
			return nil, err
		}
		if acquired {
			if err := ctx.Err(); err != nil {
				_ = release()
				return nil, err
			}
			heartbeatCtx, stopHeartbeatCtx := context.WithCancel(context.Background())
			stopHeartbeat := indexlock.StartHeartbeat(heartbeatCtx, lockPath, 30*time.Second)
			return func() error {
				stopHeartbeat()
				stopHeartbeatCtx()
				return release()
			}, nil
		}
		if !priority.Active() {
			priority.Close()
			priority, err = indexlock.RequestPriority(lockPath)
			if err != nil {
				return nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func acquireEditWriteLocks(vaultPaths *paths.VaultPaths, states map[string]*documentState, vaultWriteLeaseHeld bool) (func() error, error) {
	canonical := make([]string, 0, len(states))
	seen := make(map[string]struct{}, len(states))
	for notePath := range states {
		path, err := paths.CleanNotePath(notePath)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[path.String()]; ok {
			continue
		}
		seen[path.String()] = struct{}{}
		canonical = append(canonical, path.String())
	}
	sort.Strings(canonical)
	releases := make([]func() error, 0, len(canonical)+1)
	// Validation repairs already serialize their journaled filesystem writes on
	// this vault-wide lease. Editing joins that same boundary before taking its
	// narrower per-path leases so the two writers cannot publish over each other.
	if !vaultWriteLeaseHeld {
		globalLockPath := filepath.Join(vaultPaths.Root(), ".rhizome", "index.lock")
		globalRelease, acquired, err := indexlock.TryAcquire(globalLockPath)
		if err != nil || !acquired {
			if err != nil {
				return nil, fmt.Errorf("acquire vault write lease: %w", err)
			}
			return nil, fmt.Errorf("another vault writer is active")
		}
		releases = append(releases, globalRelease)
	}
	if err := namespaceadmission.Check(vaultPaths.Root()); err != nil {
		return nil, errors.Join(err, releaseEditWriteLocks(releases))
	}
	for _, notePath := range canonical {
		digest := sha256.Sum256([]byte(notePath))
		lockPath := filepath.Join(vaultPaths.Root(), ".rhizome", "write-locks", hex.EncodeToString(digest[:])+".lock")
		release, acquired, err := indexlock.TryAcquire(lockPath)
		if err != nil || !acquired {
			_ = releaseEditWriteLocks(releases)
			if err != nil {
				return nil, fmt.Errorf("acquire edit lease for %s: %w", notePath, err)
			}
			return nil, fmt.Errorf("another writer is editing %s", notePath)
		}
		releases = append(releases, release)
	}
	return func() error { return releaseEditWriteLocks(releases) }, nil
}

func releaseEditWriteLocks(releases []func() error) error {
	var releaseErrs []error
	for index := len(releases) - 1; index >= 0; index-- {
		if releases[index] == nil {
			continue
		}
		if err := releases[index](); err != nil {
			releaseErrs = append(releaseErrs, err)
		}
	}
	return errors.Join(releaseErrs...)
}
