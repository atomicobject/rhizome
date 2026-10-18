package serve

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/stretchr/testify/require"
)

type controlledValidationTimers struct {
	created chan *controlledValidationTimer
}

type controlledValidationTimer struct {
	mu     sync.Mutex
	active bool
	ticks  chan time.Time
	reset  chan time.Duration
}

func newControlledValidationTimers(c *ReadinessCoordinator) *controlledValidationTimers {
	scheduler := &controlledValidationTimers{created: make(chan *controlledValidationTimer, 8)}
	c.newValidationTimer = func(delay time.Duration) validationRefreshTimer {
		timer := &controlledValidationTimer{active: true, ticks: make(chan time.Time), reset: make(chan time.Duration, 8)}
		timer.reset <- delay
		scheduler.created <- timer
		return timer
	}
	return scheduler
}

func (s *controlledValidationTimers) next(t *testing.T) *controlledValidationTimer {
	t.Helper()
	select {
	case timer := <-s.created:
		timer.awaitReset(t)
		return timer
	case <-time.After(5 * time.Second):
		t.Fatal("validation did not schedule its debounce")
		return nil
	}
}

func (t *controlledValidationTimer) Ticks() <-chan time.Time { return t.ticks }

func (t *controlledValidationTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := t.active
	t.active = false
	return wasActive
}

func (t *controlledValidationTimer) Reset(delay time.Duration) bool {
	t.mu.Lock()
	wasActive := t.active
	t.active = true
	t.mu.Unlock()
	t.reset <- delay
	return wasActive
}

func (timer *controlledValidationTimer) awaitReset(t *testing.T) {
	t.Helper()
	select {
	case delay := <-timer.reset:
		require.Equal(t, validationRefreshDebounce, delay)
	case <-time.After(5 * time.Second):
		t.Fatal("validation did not reset its debounce")
	}
}

// fire returns only after the real worker consumes this expiration.
func (timer *controlledValidationTimer) fire(t *testing.T) {
	t.Helper()
	require.True(t, timer.Stop(), "only an active debounce can expire")
	select {
	case timer.ticks <- time.Time{}:
	case <-time.After(5 * time.Second):
		t.Fatal("validation did not consume its debounce expiration")
	}
}

func validationTestCoordinator(t *testing.T) (*ReadinessCoordinator, *controlledValidationTimers) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	c := NewReadinessCoordinator(nil)
	timers := newControlledValidationTimers(c)
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	c.BindLive(ctx, &bootstrap.LiveRuntime{})
	t.Cleanup(func() { cancel(); c.Drain() })
	return c, timers
}
