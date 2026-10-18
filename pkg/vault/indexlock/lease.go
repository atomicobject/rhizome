package indexlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

var ErrLeaseNotHeld = errors.New("vault index lock lease is not held")

// Lease proves this acquisition owns a vault's writer token. Copies share release state.
type Lease struct{ state *leaseState }

type leaseState struct {
	mu        sync.RWMutex
	root      string
	lockPath  string
	owner     LockData
	release   func() error
	released  bool
	err       error
	closeOnce sync.Once
	stops     []func()
}

func TryAcquireLease(vaultRoot string, opts AcquireOptions) (*Lease, bool, error) {
	vault, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return nil, false, err
	}
	if vault.Root() == "" {
		return nil, false, fmt.Errorf("vault root is required for an index lock lease")
	}
	lockPath := filepath.Join(vault.Root(), ".rhizome", "index.lock")
	var owner LockData
	release, acquired, err := tryAcquireOwned(lockPath, opts, &owner)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return &Lease{state: &leaseState{root: vault.Root(), lockPath: lockPath, owner: owner, release: release}}, true, nil
}

func (l *Lease) Validate(vaultRoot string) error {
	if l == nil || l.state == nil {
		return ErrLeaseNotHeld
	}
	vault, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return err
	}
	s := l.state
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.released || s.owner.PID != os.Getpid() {
		return ErrLeaseNotHeld
	}
	if vault.Root() != s.root {
		return fmt.Errorf("%w: lease belongs to a different vault", ErrLeaseNotHeld)
	}
	owner, ok := ReadLockData(s.lockPath)
	if !ok || !sameLockOwner(owner, s.owner) {
		return ErrLeaseNotHeld
	}
	return nil
}

func (l *Lease) Release() error {
	if l == nil || l.state == nil {
		return nil
	}
	s := l.state
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.released = true
		stops := append([]func(){}, s.stops...)
		s.mu.Unlock()
		for _, stop := range stops {
			stop()
		}
		s.err = s.release()
	})
	return s.err
}

func (l *Lease) StartHeartbeat(ctx context.Context, interval time.Duration) func() {
	return l.startHeartbeat(ctx, interval, nil)
}

// StartYieldingHeartbeat requests job cancellation on priority. The job releases
// its lease after active work drains; onYield must not release it synchronously.
func (l *Lease) StartYieldingHeartbeat(ctx context.Context, interval time.Duration, onYield func()) func() {
	return l.startHeartbeat(ctx, interval, onYield)
}

func (l *Lease) startHeartbeat(ctx context.Context, interval time.Duration, onYield func()) func() {
	if l == nil || l.state == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s := l.state
	if err := l.Validate(s.root); err != nil {
		return func() {}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if l.Validate(s.root) != nil {
					return
				}
				if onYield != nil && CheckPriority(s.lockPath) {
					onYield()
					return
				}
				if !touchIfOwner(s.lockPath, s.owner) {
					return
				}
			}
		}
	}()
	var once sync.Once
	finish := func() { once.Do(func() { close(stop) }); <-done }
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		finish()
		return func() {}
	}
	s.stops = append(s.stops, finish)
	s.mu.Unlock()
	return finish
}
