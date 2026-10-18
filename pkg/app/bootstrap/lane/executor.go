package lane

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
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
	acquire       func(role string) (*indexlock.Lease, bool, error)
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

func (e *Executor) acquire(role string) (*indexlock.Lease, bool, error) {
	if e.opts.acquire != nil {
		return e.opts.acquire(role)
	}
	return indexlock.TryAcquireLease(filepath.Dir(filepath.Dir(e.opts.LockPath)), indexlock.AcquireOptions{Role: role})
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
