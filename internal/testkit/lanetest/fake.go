package lanetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
)

// Fake runs each submitted job synchronously in Submit's goroutine and keeps
// every handle. Use it in tests for components that submit to the lane.
type Fake struct {
	mu      sync.Mutex
	next    int
	handles map[string]*fakeHandle
	closed  bool
	// Submitted records requests in order.
	Submitted []lane.Request
}

func NewFake() *Fake { return &Fake{handles: map[string]*fakeHandle{}} }

func (f *Fake) Submit(ctx context.Context, req lane.Request) (lane.Handle, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, false, lane.ErrClosed
	}
	f.next++
	// The job outlives the submitting request: derive from Background, not ctx.
	jobCtx, cancel := context.WithCancel(context.Background())
	h := &fakeHandle{id: fmt.Sprintf("fake-%d", f.next), kind: req.Kind, done: make(chan struct{}), cancel: cancel}
	f.handles[h.id] = h
	f.Submitted = append(f.Submitted, req)
	f.mu.Unlock()

	var err error
	if req.Run != nil {
		err = req.Run(jobCtx, nil, h)
	}
	if req.Summary != nil {
		h.SetSummary(req.Summary(jobCtx))
	}
	cancel()
	h.finish(err)
	return h, false, nil
}

func (f *Fake) Lookup(id string) (lane.Handle, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.handles[id]
	return h, ok
}

func (f *Fake) Status() lane.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return lane.Status{}
}

func (f *Fake) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

type fakeHandle struct {
	id      string
	kind    lane.Kind
	done    chan struct{}
	cancel  context.CancelFunc
	mu      sync.Mutex
	err     error
	events  []lane.Event
	subs    []chan lane.Event
	summary json.RawMessage
	closed  bool
}

func (h *fakeHandle) ID() string            { return h.id }
func (h *fakeHandle) Kind() lane.Kind       { return h.kind }
func (h *fakeHandle) Done() <-chan struct{} { return h.done }
func (h *fakeHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}
func (h *fakeHandle) Cancel() {
	if h.cancel != nil {
		h.cancel()
	}
}

func (h *fakeHandle) Subscribe() (<-chan lane.Event, func()) {
	h.mu.Lock()
	ch := make(chan lane.Event, len(h.events)+64)
	for _, e := range h.events {
		ch <- e
	}
	if h.closed {
		close(ch)
		h.mu.Unlock()
		return ch, func() {}
	}
	h.subs = append(h.subs, ch)
	h.mu.Unlock()
	return ch, func() {}
}

func (h *fakeHandle) emit(e lane.Event) {
	e.JobID, e.Kind, e.At = h.id, h.kind, time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
	for _, s := range h.subs {
		select {
		case s <- e:
		default:
		}
	}
}

func (h *fakeHandle) Segment(label string, done, total int64) {
	h.emit(lane.Event{Type: lane.EventProgress, Label: label, Done: done, Total: total})
}

func (h *fakeHandle) Log(line string) { h.emit(lane.Event{Type: lane.EventLog, Line: line}) }

// SetSummary mirrors the real handle so a job body can attach its timings
// payload under the fake too.
func (h *fakeHandle) SetSummary(summary json.RawMessage) {
	h.mu.Lock()
	h.summary = summary
	h.mu.Unlock()
}

func (h *fakeHandle) finish(err error) {
	msg, outcome := "", lane.OutcomeOK
	if err != nil {
		msg, outcome = err.Error(), lane.OutcomeFailed
		if errors.Is(err, context.Canceled) {
			outcome = lane.OutcomeCancelled
		}
	}
	h.mu.Lock()
	summary := h.summary
	h.mu.Unlock()
	h.emit(lane.Event{Type: lane.EventDone, OK: err == nil, Outcome: outcome, Error: msg, Summary: summary})
	h.mu.Lock()
	h.err = err
	h.closed = true
	for _, s := range h.subs {
		close(s)
	}
	h.subs = nil
	h.mu.Unlock()
	close(h.done)
}
