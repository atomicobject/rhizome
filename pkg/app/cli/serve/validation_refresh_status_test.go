package serve

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/stretchr/testify/require"
)

type canceledButFinishingRefresher struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (r *canceledButFinishingRefresher) Refresh(ctx context.Context) error {
	close(r.started)
	<-ctx.Done()
	<-r.release
	close(r.finished)
	return ctx.Err()
}

func TestReadinessDrainWaitsForCanceledValidationPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := NewReadinessCoordinator(nil)
	c.ctx = ctx
	c.live = &bootstrap.LiveRuntime{}
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	refresher := &canceledButFinishingRefresher{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(refresher.release) }) }
	defer func() { cancel(); unblock(); c.Drain() }()
	c.validation = refresher
	c.startWorker(func() { c.runValidationRefreshRequests(ctx) })
	c.RequestValidationRefresh()
	select {
	case <-refresher.started:
	case <-time.After(time.Second):
		t.Fatal("validation refresh did not start")
	}

	cancel()
	drained := make(chan struct{})
	go func() { c.Drain(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("readiness drained before the canceled refresh finished")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("readiness did not drain the completed refresh")
	}
	select {
	case <-refresher.finished:
	default:
		t.Fatal("the refresh was still able to publish after Drain returned")
	}
}

type heldStatusRefresher struct{ started chan chan struct{} }

func (r *heldStatusRefresher) Refresh(ctx context.Context) error {
	release := make(chan struct{})
	select {
	case r.started <- release:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func awaitStatusRefresh(t *testing.T, r *heldStatusRefresher) chan struct{} {
	t.Helper()
	select {
	case release := <-r.started:
		return release
	case <-time.After(time.Second):
		t.Fatal("validation refresh did not start")
		return nil
	}
}

func TestValidationRefreshPendingSurvivesOlderPublicationAndQueuedReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := NewReadinessCoordinator(nil)
	c.ctx = ctx
	c.live = &bootstrap.LiveRuntime{}
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	r := &heldStatusRefresher{started: make(chan chan struct{}, 3)}
	c.validation = r
	require.False(t, c.ValidationRefreshPending())

	// Startup validation can finish after a manual request was accepted, but
	// its completion says nothing about the request still waiting in the queue.
	olderDone := make(chan struct{})
	go func() { defer close(olderDone); c.refreshValidation(ctx, c.live) }()
	older := awaitStatusRefresh(t, r)
	c.RequestValidationRefresh()
	require.True(t, c.ValidationRefreshPending())
	close(older)
	<-olderDone
	require.True(t, c.ValidationRefreshPending())

	workerDone := make(chan struct{})
	go func() { defer close(workerDone); c.runValidationRefreshRequests(ctx) }()
	defer func() { cancel(); <-workerDone }()
	first := awaitStatusRefresh(t, r)
	require.True(t, c.ValidationRefreshPending())
	c.RequestValidationRefresh()
	close(first)
	second := awaitStatusRefresh(t, r)
	require.True(t, c.ValidationRefreshPending(), "completion must retain the queued replacement")
	close(second)
	require.Eventually(t, func() bool { return !c.ValidationRefreshPending() }, time.Second, time.Millisecond)
}

func TestValidationRefreshPendingClearsOnCancellation(t *testing.T) {
	for _, phase := range []string{"debounce", "startup", "running", "invalidation"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			c := NewReadinessCoordinator(nil)
			c.ctx = ctx
			c.live = &bootstrap.LiveRuntime{}
			r := &heldStatusRefresher{started: make(chan chan struct{}, 1)}
			c.validation = r
			if phase != "startup" {
				c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
			}
			c.RequestValidationRefresh()
			require.True(t, c.ValidationRefreshPending())
			done := make(chan struct{})
			go func() { defer close(done); c.runValidationRefreshRequests(ctx) }()
			defer func() { cancel(); <-done }()
			if phase == "running" || phase == "invalidation" {
				_ = awaitStatusRefresh(t, r)
			}
			if phase == "startup" {
				time.Sleep(2 * validationRefreshDebounce)
				require.True(t, c.ValidationRefreshPending())
			}
			if phase == "invalidation" {
				c.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{"reason": web.GlobalEventReasonReconcileFailed})
			} else {
				cancel()
			}
			require.Eventually(t, func() bool { return !c.ValidationRefreshPending() }, time.Second, time.Millisecond)
			if phase != "invalidation" {
				c.RequestValidationRefresh()
				require.False(t, c.ValidationRefreshPending(), "a stopped runtime cannot accumulate pending requests")
			}
		})
	}
}
