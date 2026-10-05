package lane

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// Timings the lane guarantees. The priority poll is one second so an external
// writer's request reaches a running background job within a second
// (SPEC-0104 US3, US6).
const (
	DefaultPriorityPoll = time.Second
	// DefaultHistory is the bounded per-job event history replayed to a late
	// subscriber. Newest events are kept.
	DefaultHistory = 512
	// DefaultFinishedJobs is how many finished handles Lookup keeps.
	DefaultFinishedJobs = 32

	acquireBackoffInit = 200 * time.Millisecond
	acquireBackoffMax  = 2 * time.Second
	// externalHoldReport is how often a waiting explicit job logs the holder.
	externalHoldReport = 5 * time.Second
)

// HeldExternalPriority is Status.Held while an external priority request keeps
// background scheduling paused.
const HeldExternalPriority = "external priority request"

// Options configure an Executor. Zero values take the defaults above.
type Options struct {
	// LockPath is the vault's .rhizome/index.lock.
	LockPath string
	Debug    bool
	// PriorityPoll overrides the one-second yield/priority poll.
	PriorityPoll time.Duration
	History      int
	FinishedJobs int

	// acquire and checkPriority are test seams; production uses indexlock.
	acquire       func(role string) (release func() error, acquired bool, err error)
	checkPriority func() bool
}

// Executor is the real Lane: one worker goroutine, one index lock per job.
//
// Ordering: an explicit index job preempts a running background job by
// cancelling its context and runs next; background jobs run FIFO. A cancelled
// background job's handle reports context.Canceled, and its submitter (the
// scheduler that owns the pending work) requeues it — the lane never retries a
// job body on its own, because only the submitter knows what still needs doing.
type Executor struct {
	opts Options
	// preflightFn runs before and after each job acquires the lock; an error
	// fails the job without running it. The runtime installs a database-
	// replacement check through SetPreflight.
	preflightFn func(kind Kind) error

	mu       sync.Mutex
	queue    []*jobHandle
	running  *jobHandle
	held     string
	lastErr  error
	closed   bool
	seq      int
	byID     map[string]*jobHandle
	finished []string

	wake           chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	workerWG       sync.WaitGroup
	queuedFinishWG sync.WaitGroup
	closeDone      chan struct{}
}

var _ Lane = (*Executor)(nil)

// New starts the lane's worker goroutine.
func New(opts Options) *Executor {
	if opts.PriorityPoll <= 0 {
		opts.PriorityPoll = DefaultPriorityPoll
	}
	if opts.History <= 0 {
		opts.History = DefaultHistory
	}
	if opts.FinishedJobs <= 0 {
		opts.FinishedJobs = DefaultFinishedJobs
	}
	e := &Executor{opts: opts, byID: map[string]*jobHandle{}, wake: make(chan struct{}, 1), closeDone: make(chan struct{})}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.workerWG.Add(1)
	go e.work()
	return e
}

func (e *Executor) acquire(role string) (func() error, bool, error) {
	if e.opts.acquire != nil {
		return e.opts.acquire(role)
	}
	return indexlock.TryAcquireWithOptions(e.opts.LockPath, indexlock.AcquireOptions{Role: role})
}

func (e *Executor) checkPriority() bool {
	if e.opts.checkPriority != nil {
		return e.opts.checkPriority()
	}
	return indexlock.CheckPriority(e.opts.LockPath)
}

func (e *Executor) debugf(format string, args ...any) {
	if e.opts.Debug {
		log.Printf("indexing lane: "+format, args...)
	}
}

// Submit queues or joins a job. See the Lane contract in lane.go.
func (e *Executor) Submit(ctx context.Context, req Request) (Handle, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	coalesce := req.Coalesce || req.Kind == KindExplicitIndex

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, false, ErrClosed
	}
	if coalesce {
		if existing := e.findJoinable(req.Kind); existing != nil {
			e.mu.Unlock()
			joining := diagnostics.OperationFromContext(ctx)
			existing.diagnosticEvent(slog.LevelInfo, "job.joined", slog.String("joining_operation_id", joining.ID), slog.String("joining_trace_id", joining.TraceID))
			return existing, true, nil
		}
	}
	e.seq++
	h := newJobHandle(fmt.Sprintf("job-%d", e.seq), req.Kind, req.Run, e.opts.History)
	h.admitDiagnostics(ctx, req.Trigger)
	h.summaryFn = req.Summary
	h.cancel = func() { e.cancelQueued(h) }
	e.byID[h.id] = h
	e.queue = append(e.queue, h)
	var preempt *jobHandle
	if req.Kind == KindExplicitIndex && e.running != nil && e.running.kind.Background() {
		preempt = e.running
	}
	e.mu.Unlock()

	if preempt != nil {
		e.debugf("explicit index preempts %s", preempt.kind)
		preempt.markPreempted(h)
		preempt.diagnosticEvent(slog.LevelInfo, "job.preemption_requested", slog.String("preempting_job_id", h.id))
		preempt.Cancel()
	}
	e.signal()
	return h, false, nil
}

// cancelQueued completes cancellation without waiting for an unrelated running
// job or priority hold. Once picked, run attaches its own cancellation function;
// cancelReq preserves cancellation racing with that handoff.
func (e *Executor) cancelQueued(h *jobHandle) {
	e.mu.Lock()
	for i, queued := range e.queue {
		if queued != h {
			continue
		}
		e.queue = append(e.queue[:i], e.queue[i+1:]...)
		e.retireLocked(h)
		// Close clears the queue under mu before waiting, so no finalizer can
		// register after shutdown starts waiting for these removed handles.
		e.queuedFinishWG.Add(1)
		e.mu.Unlock()
		defer e.queuedFinishWG.Done()
		h.finish(context.Canceled)
		e.signal()
		return
	}
	e.mu.Unlock()
}

// findJoinable returns a queued or running job of this kind. Callers hold mu.
//
// Only explicit index jobs join a *running* job: two `rzm index` invocations
// must be one job. A background submitter reports a change the running job may
// already have passed, so it always gets a pass that starts after its call.
func (e *Executor) findJoinable(kind Kind) *jobHandle {
	if kind == KindExplicitIndex && e.running != nil && e.running.kind == kind && !e.running.cancelled() {
		return e.running
	}
	for _, h := range e.queue {
		if h.kind == kind && !h.cancelled() {
			return h
		}
	}
	return nil
}

func (e *Executor) Lookup(id string) (Handle, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h, ok := e.byID[id]
	if !ok {
		return nil, false
	}
	return h, true
}

func (e *Executor) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	status := Status{Queued: len(e.queue), Held: e.held, LastError: e.lastErr}
	if e.running != nil {
		status.Busy = true
		status.JobID = e.running.id
		status.JobKind = e.running.kind
	}
	return status
}

// Close cancels all jobs and waits for their terminal reports and worker drain.
func (e *Executor) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		<-e.closeDone
		return
	}
	e.closed = true
	dropped := e.queue
	e.queue = nil
	e.mu.Unlock()

	for _, h := range dropped {
		h.setReason("runtime_shutdown")
		h.finish(ErrClosed)
	}
	e.mu.Lock()
	if e.running != nil {
		e.running.setReason("runtime_shutdown")
	}
	e.mu.Unlock()
	e.cancel()
	e.signal()
	e.workerWG.Wait()
	e.queuedFinishWG.Wait()
	close(e.closeDone)
}

func (e *Executor) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

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
	indexingperf.ObserveLatency(ctx, "lane.queue_wait", h.queueWait)
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
	release, err := e.acquireForJob(ctx, h)
	h.lockWait = time.Since(lockStarted)
	indexingperf.ObserveLatency(ctx, "lane.lock_wait", h.lockWait)
	if err != nil {
		if ctx.Err() == nil {
			h.setReason("lock_failed")
		}
		return err
	}
	defer func() {
		releaseStarted := time.Now()
		defer func() { indexingperf.ObserveLatency(ctx, "lane.lock_release", time.Since(releaseStarted)) }()
		if releaseErr := release(); releaseErr != nil {
			e.debugf("release index lock: %v", releaseErr)
			indexingperf.AddCount(ctx, "lane.lock_release_errors", 1)
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
		stopHeartbeat = indexlock.StartYieldingHeartbeat(ctx, e.opts.LockPath, e.opts.PriorityPoll, func() {
			e.debugf("%s yields to an external priority request", h.kind)
			e.setHeld(HeldExternalPriority)
			h.setReason("external_priority")
			h.diagnosticEvent(slog.LevelInfo, "job.yielded", slog.String("reason_code", "external_priority"))
			h.Cancel()
		})
	} else {
		// An explicit job is the priority; it is never interrupted by one.
		stopHeartbeat = indexlock.StartHeartbeat(ctx, e.opts.LockPath, e.opts.PriorityPoll)
	}
	defer stopHeartbeat()

	if h.run == nil {
		return nil
	}
	executionStarted := time.Now()
	defer func() {
		h.execution = time.Since(executionStarted)
		indexingperf.ObserveLatency(ctx, "lane.execution", h.execution)
	}()
	return h.run(ctx, h)
}

func (e *Executor) acquireForJob(ctx context.Context, h *jobHandle) (func() error, error) {
	backoff := acquireBackoffInit
	reported := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, acquired, err := e.acquire(h.kind.LockRole())
		if err != nil {
			return nil, fmt.Errorf("index lock: %w", err)
		}
		if acquired {
			return release, nil
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
