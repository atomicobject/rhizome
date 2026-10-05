package agentapi

import (
	"context"
	"sync"
)

// CodeAccess declares the shared vault resources an operation can touch. The
// default is exclusive: note runtime construction can refresh metadata even for
// a read-shaped API. Pure provider calls and existing-index readers are audited
// separately from that live runtime. This is per connection; store/source owners
// continue to enforce their cross-process write contracts.
type CodeAccess int

const (
	CodeVaultExclusive CodeAccess = iota
	CodeVaultRead
	CodeIndependent
)

// CodeResources protects a connection's live vault runtime from overlapping
// refresh/mutation, while allowing existing-index readers and unrelated provider
// calls to overlap. Waiting for ownership honors the call's deadline.
type CodeResources struct {
	mu             sync.Mutex
	changed        chan struct{}
	readers        int
	writer         bool
	waitingWriters int
}

func NewCodeResources() *CodeResources { return &CodeResources{changed: make(chan struct{})} }
func (r *CodeResources) notify()       { close(r.changed); r.changed = make(chan struct{}) }
func (r *CodeResources) Acquire(ctx context.Context, name string) (func(), error) {
	descriptor, ok := CodeOperationDescriptor(name)
	access := CodeVaultExclusive
	if ok {
		access = descriptor.CodeAccess
	}
	return r.AcquireAccess(ctx, access)
}

// AcquireAccess takes ownership for one call whose access class the caller
// decided, such as a read the vault runtime serves for an exclusive local
// operation.
func (r *CodeResources) AcquireAccess(ctx context.Context, access CodeAccess) (func(), error) {
	if access == CodeIndependent {
		return func() {}, ctx.Err()
	}
	write := access == CodeVaultExclusive
	r.mu.Lock()
	if write {
		r.waitingWriters++
	}
	for {
		if err := ctx.Err(); err != nil {
			if write {
				r.waitingWriters--
				r.notify()
			}
			r.mu.Unlock()
			return nil, err
		}
		if !r.writer && ((write && r.readers == 0) || (!write && r.waitingWriters == 0)) {
			if write {
				r.waitingWriters--
				r.writer = true
			} else {
				r.readers++
			}
			r.mu.Unlock()
			return func() {
				r.mu.Lock()
				if write {
					r.writer = false
				} else {
					r.readers--
				}
				r.notify()
				r.mu.Unlock()
			}, nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		r.mu.Lock()
	}
}
