package actions

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunFileMutationsAggregatesInCompletionOrder(t *testing.T) {
	slowStarted := make(chan struct{})
	afterFastStarted := make(chan struct{})
	afterSlowStarted := make(chan struct{})
	processor := func(_ context.Context, path string) fileMutation {
		switch path {
		case "slow":
			close(slowStarted)
			<-afterFastStarted
		case "fast":
			<-slowStarted
		case "after-fast":
			// This job can start only after the fast result has been sent.
			close(afterFastStarted)
			<-afterSlowStarted
		case "after-slow":
			close(afterSlowStarted)
			return fileMutation{}
		}
		return fileMutation{notesTouched: true, changes: map[string]int{"status": 1}, fileChanged: path}
	}
	done := make(chan struct{})
	var summary fileMutationSummary
	var err error
	go func() {
		summary, err = runFileMutations(t.Context(), []string{"slow", "fast", "after-fast", "after-slow"}, processor, 2)
		close(done)
	}()
	waitForMutationSignal(t, done)
	require.NoError(t, err)
	require.Equal(t, 3, summary.notesTouched)
	require.Equal(t, map[string]int{"status": 3}, summary.changes)
	require.Equal(t, []string{"fast", "slow", "after-fast"}, summary.filesChanged)
}

func TestRunFileMutationsCancelsAndWaitsForActiveWorkers(t *testing.T) {
	firstErr := errors.New("first failure")
	successSent := make(chan struct{})
	activeStarted := make(chan struct{})
	cancellationObserved := make(chan struct{})
	releaseActive := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseActive) }) }
	t.Cleanup(release)
	var activeFinished atomic.Bool
	processor := func(ctx context.Context, path string) fileMutation {
		switch path {
		case "success":
			<-activeStarted
			return fileMutation{notesTouched: true, changes: map[string]int{"kept": 1}, fileChanged: path}
		case "failure":
			<-successSent
			return fileMutation{notesTouched: true, changes: map[string]int{"failed": 1}, fileChanged: path, err: firstErr}
		case "active":
			close(activeStarted)
			<-ctx.Done()
			close(cancellationObserved)
			<-releaseActive
			activeFinished.Store(true)
			return fileMutation{err: errors.New("later failure")}
		case "after-success":
			// The other two workers are blocked, so this proves the success
			// result was sent before allowing the first error to be returned.
			close(successSent)
			<-ctx.Done()
			return fileMutation{err: ctx.Err()}
		default:
			panic("unexpected file")
		}
	}
	done := make(chan struct{})
	var summary fileMutationSummary
	var err error
	var waitedForActive bool
	go func() {
		summary, err = runFileMutations(t.Context(), []string{"success", "failure", "active", "after-success"}, processor, 3)
		waitedForActive = activeFinished.Load()
		close(done)
	}()
	waitForMutationSignal(t, cancellationObserved)
	release()
	waitForMutationSignal(t, done)
	require.True(t, waitedForActive, "return must wait for active processors")
	require.ErrorIs(t, err, firstErr)
	require.Equal(t, 1, summary.notesTouched)
	require.Equal(t, map[string]int{"kept": 1}, summary.changes)
	require.Equal(t, []string{"success"}, summary.filesChanged)
}

func TestRunFileMutationsWithNonpositiveWorkers(t *testing.T) {
	for _, workers := range []int{0, -1} {
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			var lastContext context.Context
			summary, err := runFileMutations(t.Context(), []string{"a", "b"}, func(ctx context.Context, path string) fileMutation {
				lastContext = ctx
				return fileMutation{notesTouched: true, changes: map[string]int{path: 1}, fileChanged: path}
			}, workers)
			require.NoError(t, err)
			require.Equal(t, 2, summary.notesTouched)
			require.Equal(t, map[string]int{"a": 1, "b": 1}, summary.changes)
			require.Equal(t, []string{"a", "b"}, summary.filesChanged)
			require.ErrorIs(t, lastContext.Err(), context.Canceled)
		})
	}
	called := false
	summary, err := runFileMutations(t.Context(), nil, func(context.Context, string) fileMutation {
		called = true
		return fileMutation{}
	}, 1)
	require.NoError(t, err)
	require.False(t, called)
	require.Equal(t, fileMutationSummary{changes: map[string]int{}, filesChanged: []string{}}, summary)
}

func waitForMutationSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mutation worker")
	}
}
