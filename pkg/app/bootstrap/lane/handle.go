package lane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// subscriberBuffer is the live-event headroom each subscriber gets on top of
// the replayed history. A slow subscriber loses its oldest events, never the
// job: fan-out must not block the worker.
const subscriberBuffer = 64

// SummaryReporter is the optional Reporter extension a job body uses to attach
// its timings/summary payload to the terminal event. Lane handles implement
// it; a body that cares type-asserts for it.
type SummaryReporter interface {
	Reporter
	SetSummary(summary json.RawMessage)
}

type subscriber struct {
	ch     chan Event
	closed bool
}

type jobHandle struct {
	id                             string
	kind                           Kind
	run                            func(context.Context, Reporter) error
	history                        int
	started                        time.Time
	done                           chan struct{}
	diagnosticCtx                  context.Context
	collector                      *indexingperf.Collector
	finishTotal                    func(error)
	pickedAt                       time.Time
	queueWait, lockWait, execution time.Duration
	reason                         string

	mu        sync.Mutex
	cancel    context.CancelFunc
	cancelReq bool
	preempted bool
	// preemptedBy is the explicit job that displaced this one.
	preemptedBy Handle
	events      []Event
	subs        []*subscriber
	summary     json.RawMessage
	summaryFn   func(context.Context) json.RawMessage
	err         error
	finishing   bool
	finished    bool
}

var _ Handle = (*jobHandle)(nil)
var _ SummaryReporter = (*jobHandle)(nil)

func newJobHandle(id string, kind Kind, run func(context.Context, Reporter) error, history int) *jobHandle {
	return &jobHandle{id: id, kind: kind, run: run, history: history, started: time.Now(), done: make(chan struct{})}
}

func (h *jobHandle) ID() string            { return h.id }
func (h *jobHandle) Kind() Kind            { return h.kind }
func (h *jobHandle) Done() <-chan struct{} { return h.done }

func (h *jobHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// attach installs the running job's cancel func. A Cancel that arrived while
// the job was still queued takes effect immediately.
func (h *jobHandle) attach(cancel context.CancelFunc) {
	h.mu.Lock()
	h.cancel = cancel
	requested := h.cancelReq
	h.mu.Unlock()
	if requested {
		cancel()
	}
}

// Cancel is job-wide and idempotent: any joiner cancels the job for all.
func (h *jobHandle) Cancel() {
	h.mu.Lock()
	if h.finished || h.finishing {
		h.mu.Unlock()
		return
	}
	h.cancelReq = true
	if h.reason == "" {
		h.reason = "explicit_cancel"
	}
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// markPreempted records that an explicit index job displaced this one, so
// its cancellation is reported as ErrPreempted.
func (h *jobHandle) markPreempted(by Handle) {
	h.mu.Lock()
	if h.finished || h.finishing {
		h.mu.Unlock()
		return
	}
	h.preempted = true
	h.reason = "explicit_index"
	h.preemptedBy = by
	h.mu.Unlock()
}

func (h *jobHandle) cancelled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancelReq
}

func (h *jobHandle) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan Event, len(h.events)+subscriberBuffer)
	for _, e := range h.events {
		ch <- e
	}
	if h.finished {
		close(ch)
		return ch, func() {}
	}
	sub := &subscriber{ch: ch}
	h.subs = append(h.subs, sub)
	return ch, func() { h.unsubscribe(sub) }
}

func (h *jobHandle) unsubscribe(target *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, sub := range h.subs {
		if sub != target {
			continue
		}
		h.subs = append(h.subs[:i], h.subs[i+1:]...)
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		return
	}
}

func (h *jobHandle) Segment(label string, done, total int64) {
	h.emit(Event{Type: EventProgress, Label: label, Done: done, Total: total})
}

func (h *jobHandle) Log(line string) { h.emit(Event{Type: EventLog, Line: line}) }

func (h *jobHandle) SetSummary(summary json.RawMessage) {
	h.mu.Lock()
	h.summary = summary
	h.mu.Unlock()
}

func (h *jobHandle) emit(event Event) {
	event.JobID, event.Kind, event.At = h.id, h.kind, time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return
	}
	h.appendHistoryLocked(event)
	h.fanOutLocked(event)
}

func (h *jobHandle) appendHistoryLocked(event Event) {
	h.events = append(h.events, event)
	if len(h.events) > h.history {
		h.events = h.events[len(h.events)-h.history:]
	}
}

// fanOutLocked never blocks: a full subscriber loses its oldest event so the
// newest — always including Done — still fits.
func (h *jobHandle) fanOutLocked(event Event) {
	for _, sub := range h.subs {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- event:
			default:
			}
		}
	}
}

func (h *jobHandle) finish(err error) {
	h.mu.Lock()
	if h.finished || h.finishing {
		h.mu.Unlock()
		return
	}
	h.finishing = true
	preempted, by := h.preempted, h.preemptedBy
	h.mu.Unlock()
	if preempted && errors.Is(err, context.Canceled) && !errors.Is(err, ErrPreempted) {
		err = &PreemptedError{By: by, err: fmt.Errorf("%w: %w", ErrPreempted, err)}
	}
	message, outcome := "", OutcomeOK
	if err != nil {
		message, outcome = err.Error(), OutcomeFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrClosed) {
			outcome = OutcomeCancelled
		}
	}
	if h.finishTotal != nil {
		h.finishTotal(err)
	}
	var summary json.RawMessage
	if h.summaryFn != nil {
		summary = h.summaryFn(h.diagnosticCtx)
	}
	h.publishDiagnostics(err)
	h.mu.Lock()
	if h.summaryFn != nil {
		h.summary = summary
	}
	event := Event{
		Type:    EventDone,
		JobID:   h.id,
		Kind:    h.kind,
		OK:      err == nil,
		Outcome: outcome,
		Error:   message,
		Summary: h.summary,
		Elapsed: time.Since(h.started).Seconds(),
		At:      time.Now(),
	}
	h.appendHistoryLocked(event)
	h.fanOutLocked(event)
	h.err = err
	h.finishing = false
	h.finished = true
	for _, sub := range h.subs {
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
	}
	h.subs = nil
	h.mu.Unlock()
	close(h.done)
}
