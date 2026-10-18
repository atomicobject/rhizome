package lane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

func (e *Executor) work() {
	defer e.workerWG.Done()
	for {
		next, wait := e.next()
		switch {
		case next != nil:
			e.run(next)
			continue
		case wait > 0:
			// Background work is held by an external priority request. Wake
			// early if an explicit job arrives.
			timer := time.NewTimer(wait)
			select {
			case <-e.ctx.Done():
				timer.Stop()
				e.drain()
				return
			case <-e.wake:
				timer.Stop()
			case <-timer.C:
			}
		default:
			select {
			case <-e.ctx.Done():
				e.drain()
				return
			case <-e.wake:
			}
		}
	}
}

// drain finishes anything still queued after Close cancelled the lane.
func (e *Executor) drain() {
	e.mu.Lock()
	dropped := e.queue
	e.queue = nil
	e.mu.Unlock()
	for _, h := range dropped {
		h.finish(ErrClosed)
	}
}

// next picks the job to run: explicit first, then background FIFO. It returns
// a wait duration instead when background work is held by external priority.
func (e *Executor) next() (*jobHandle, time.Duration) {
	e.mu.Lock()
	if e.closed || len(e.queue) == 0 {
		e.held = ""
		e.mu.Unlock()
		return nil, 0
	}
	idx := 0
	for i, h := range e.queue {
		if h.kind == KindExplicitIndex {
			idx = i
			break
		}
	}
	candidate := e.queue[idx]
	e.mu.Unlock()

	if candidate.kind.Background() && e.checkPriority() {
		e.setHeld(HeldExternalPriority)
		return nil, e.opts.PriorityPoll
	}
	e.setHeld("")

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		// Close ran while the priority check did: its queue is already dropped.
		return nil, 0
	}
	// Re-pick under the lock: Submit may have queued an explicit job while the
	// priority check ran, and it must not wait behind the background candidate
	// (e.running is nil during that window, so Submit could not preempt).
	pick := -1
	for i, h := range e.queue {
		if h.kind == KindExplicitIndex {
			pick = i
			break
		}
		if h == candidate && pick < 0 {
			pick = i
		}
	}
	if pick < 0 {
		return nil, 0
	}
	h := e.queue[pick]
	e.queue = append(e.queue[:pick], e.queue[pick+1:]...)
	e.running = h
	return h, 0
}

func (e *Executor) setHeld(reason string) {
	e.mu.Lock()
	e.held = reason
	e.mu.Unlock()
}

func (e *Executor) run(h *jobHandle) {
	ctx, cancel := context.WithCancel(e.ctx)
	ctx = diagnostics.Propagate(h.diagnosticCtx, ctx)
	ctx = indexingperf.WithCollector(ctx, h.collector)
	h.pickedAt = time.Now()
	h.queueWait = h.pickedAt.Sub(h.started)
	indexingperf.ObserveLatency(ctx, indexingperf.LaneQueueWait, h.queueWait)
	h.diagnosticEvent(slog.LevelInfo, "job.started")
	// Publish cancellation before the job can be observed running, so a Cancel
	// racing with the start is never lost.
	h.attach(cancel)
	defer cancel()

	var err error
	if preflight := e.preflight(); preflight != nil {
		err = preflight(h.kind)
		if err != nil {
			h.setPreflightReason(err)
		}
	}
	if err == nil {
		err = e.runLocked(ctx, h)
	}

	e.mu.Lock()
	e.running = nil
	e.lastErr = err
	e.retireLocked(h)
	e.mu.Unlock()

	h.finish(err)
	e.signal()
}

// SetPreflight installs or replaces the per-job preflight after construction;
// the serve command wires it once the runtime's stores are open.
func (e *Executor) SetPreflight(fn func(kind Kind) error) {
	e.mu.Lock()
	e.preflightFn = fn
	e.mu.Unlock()
}

func (e *Executor) preflight() func(kind Kind) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.preflightFn
}

// runLocked acquires the index lock for this job, heartbeats it, and runs the
// body. Every job holds the lock exactly once, with its kind as the role.
func (e *Executor) runLocked(ctx context.Context, h *jobHandle) error {
	lockStarted := time.Now()
	lease, err := e.acquireForJob(ctx, h)
	h.lockWait = time.Since(lockStarted)
	indexingperf.ObserveLatency(ctx, indexingperf.LaneLockWait, h.lockWait)
	if err != nil {
		if ctx.Err() == nil {
			h.setReason("lock_failed")
		}
		return err
	}
	defer func() {
		releaseStarted := time.Now()
		defer func() { indexingperf.ObserveLatency(ctx, indexingperf.LaneLockRelease, time.Since(releaseStarted)) }()
		if releaseErr := lease.Release(); releaseErr != nil {
			e.debugf("release index lock: %v", releaseErr)
			indexingperf.AddCount(ctx, indexingperf.LaneLockReleaseErrors, 1)
			h.diagnosticEvent(slog.LevelWarn, "lock.release_failed")
		}
	}()

	// The database may have been replaced while an external writer held the
	// lease. Recheck under our lease before using any previously opened store.
	if preflight := e.preflight(); preflight != nil {
		if err := preflight(h.kind); err != nil {
			h.setPreflightReason(err)
			return err
		}
	}

	var stopHeartbeat func()
	if h.kind.Background() {
		stopHeartbeat = lease.StartYieldingHeartbeat(ctx, e.opts.PriorityPoll, func() {
			e.debugf("%s yields to an external priority request", h.kind)
			e.setHeld(HeldExternalPriority)
			h.setReason("external_priority")
			h.diagnosticEvent(slog.LevelInfo, "job.yielded", slog.String("reason_code", "external_priority"))
			h.Cancel()
		})
	} else {
		// An explicit job is the priority; it is never interrupted by one.
		stopHeartbeat = lease.StartHeartbeat(ctx, e.opts.PriorityPoll)
	}
	defer stopHeartbeat()

	if h.run == nil {
		return nil
	}
	executionStarted := time.Now()
	defer func() {
		h.execution = time.Since(executionStarted)
		indexingperf.ObserveLatency(ctx, indexingperf.LaneExecution, h.execution)
	}()
	return h.run(ctx, lease, h)
}

func (e *Executor) acquireForJob(ctx context.Context, h *jobHandle) (*indexlock.Lease, error) {
	backoff := acquireBackoffInit
	reported := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lease, acquired, err := e.acquire(h.kind.LockRole())
		if err != nil {
			return nil, fmt.Errorf("index lock: %w", err)
		}
		if acquired {
			return lease, nil
		}
		// Held by a process outside this runtime. Explicit jobs report who has
		// it so a waiting `rzm index` client sees a reason, not silence.
		if h.kind == KindExplicitIndex && time.Since(reported) >= externalHoldReport {
			reported = time.Now()
			h.Log(holderDescription(e.opts.LockPath))
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > acquireBackoffMax {
			backoff = acquireBackoffMax
		}
	}
}

func holderDescription(lockPath string) string {
	data, ok := indexlock.ReadLockData(lockPath)
	if !ok {
		return "Waiting for the index lock (held by another process)"
	}
	role := data.Role
	if role == "" {
		role = "unknown"
	}
	return fmt.Sprintf("Waiting for the index lock (held by %s, pid %d)", role, data.PID)
}

// retireLocked keeps a bounded set of finished handles for Lookup.
func (e *Executor) retireLocked(h *jobHandle) {
	e.finished = append(e.finished, h.id)
	for len(e.finished) > e.opts.FinishedJobs {
		delete(e.byID, e.finished[0])
		e.finished = e.finished[1:]
	}
}
