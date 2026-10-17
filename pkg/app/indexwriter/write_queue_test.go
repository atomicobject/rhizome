package indexwriter

import (
	"context"
	"errors"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestQueuedWriterFlushesOnClose(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		flushes  int
		rowTotal int
	)
	q := New(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			mu.Lock()
			defer mu.Unlock()
			flushes++
			rowTotal += len(batch)
			return nil
		},
	})

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "a.md"}))
	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "b.md"}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, flushes)
	require.Equal(t, 2, rowTotal)
}

func TestQueuedWriterFlushesOnInterval(t *testing.T) {
	t.Parallel()

	flushed := make(chan struct{}, 1)
	q := New(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			select {
			case flushed <- struct{}{}:
			default:
			}
			return nil
		},
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "tick.md"}))

	select {
	case <-flushed:
	case <-time.After(indexQueueFlushIdle * 4):
		t.Fatal("expected interval flush")
	}
}

func TestQueuedWriterReturnsHandlerError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	q := New(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			return wantErr
		},
	})

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "err.md"}))
	err := q.Close()
	require.ErrorIs(t, err, wantErr)
	require.ErrorIs(t, q.Err(), wantErr)
}

func TestQueuedWriterFlushAndWaitReturnsWriterErrorWhenWriterStopsBeforeAck(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("writer callback failed")
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	q := newWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			once.Do(func() {
				close(started)
				<-release
			})
			return wantErr
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "a.md"}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("writer never entered note batch")
	}

	flushDone := make(chan error, 1)
	go func() {
		flushDone <- q.FlushAndWait(context.Background())
	}()

	select {
	case err := <-flushDone:
		t.Fatalf("flush returned before writer failed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-flushDone:
		require.ErrorIs(t, err, wantErr)
	case <-time.After(2 * time.Second):
		t.Fatal("flush did not return after writer stopped")
	}
}

func TestQueuedWriterAcksPendingControlsOnContextExit(t *testing.T) {
	t.Parallel()

	q := newWithConfig(context.Background(), Handlers{}, config{
		DefaultPolicy: flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.StopAndWait() })

	ack := make(chan controlResult[struct{}], 1)
	require.NoError(t, q.sendControl(context.Background(), &orderedControl[struct{}]{sequence: 999, ack: ack}))
	require.Eventually(t, func() bool { return len(q.ctrlCh) == 0 }, time.Second, time.Millisecond)
	q.cancel()

	select {
	case result := <-ack:
		require.ErrorIs(t, result.err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("pending control was not acknowledged when writer exited")
	}
}

func TestQueuedWriterOwnershipTransitionsDrainPriorPayloadsAndReturnResult(t *testing.T) {
	t.Parallel()

	var order []string
	want := semdb.OwnershipTransitionResult{
		AffectedSourcePaths:      []string{"related.md"},
		AffectedAnchorIDs:        []int64{17},
		ReconciliationGeneration: 9,
	}
	q := newWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(context.Context, []codeanchor.NoteIndexWork) error {
			order = append(order, "payload")
			return nil
		},
		ApplyOwnershipTransitions: func(_ context.Context, transitions []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			order = append(order, "transition:"+transitions[0].Path)
			return want, nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "before.md"}))
	got, err := q.SubmitOwnershipTransitions(context.Background(), []semdb.OwnershipTransition{{
		Path:   "note.html",
		Target: semdb.OwnershipTargetNote,
		Note: &semdb.NoteSourceState{
			FormatID: "html", ContentHash: "hash", ProviderVersion: "v1", ProjectionVersion: "v1",
		},
	}})
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, []string{"payload", "transition:note.html"}, order)
}

func TestQueuedWriterOwnershipTransitionsCloneInput(t *testing.T) {
	t.Parallel()

	transitions := []semdb.OwnershipTransition{{
		Path: "before.html", Target: semdb.OwnershipTargetNote,
		Note: &semdb.NoteSourceState{Title: "before", FormatID: "html", ContentHash: "hash", ProviderVersion: "v1", ProjectionVersion: "v1"},
	}}
	cloned := CloneOwnershipTransitions(transitions)
	transitions[0].Path = "after.html"
	transitions[0].Note.Title = "after"

	require.Equal(t, "before.html", cloned[0].Path)
	require.Equal(t, "before", cloned[0].Note.Title)
}

func TestQueuedWriterOwnershipTransitionFailureIsTerminal(t *testing.T) {
	t.Parallel()

	want := errors.New("transition failed")
	q := New(context.Background(), Handlers{
		ApplyOwnershipTransitions: func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			return semdb.OwnershipTransitionResult{}, want
		},
	})

	_, err := q.SubmitOwnershipTransitions(context.Background(), []semdb.OwnershipTransition{{Path: "note.md", Target: semdb.OwnershipTargetUnowned}})
	require.ErrorIs(t, err, want)
	require.ErrorIs(t, q.Err(), want)
}

func TestQueuedWriterOwnershipReconciliationFalseAckIsTerminal(t *testing.T) {
	t.Parallel()

	q := New(context.Background(), Handlers{
		AcknowledgeOwnershipReconciliation: func(context.Context, int64) (bool, error) {
			return false, nil
		},
	})

	err := q.AcknowledgeOwnershipReconciliation(context.Background(), 42)
	require.Error(t, err)
	require.ErrorContains(t, err, "42")
	require.ErrorIs(t, q.Err(), err)
}

func TestQueuedWriterOwnershipControlsRespectCallerCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	q := New(context.Background(), Handlers{
		ApplyOwnershipTransitions: func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			called = true
			return semdb.OwnershipTransitionResult{}, nil
		},
	})
	t.Cleanup(func() { _ = q.Close() })

	_, err := q.SubmitOwnershipTransitions(ctx, []semdb.OwnershipTransition{{Path: "note.md", Target: semdb.OwnershipTargetUnowned}})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, called)
	require.ErrorIs(t, q.AcknowledgeOwnershipReconciliation(ctx, 1), context.Canceled)
}

func TestQueuedWriterPostTransitionPayloadWaitsForTransitionHandler(t *testing.T) {
	t.Parallel()

	transitionStarted := make(chan struct{})
	releaseTransition := make(chan struct{})
	payloadApplied := make(chan struct{}, 1)
	q := newWithConfig(context.Background(), Handlers{
		ApplyOwnershipTransitions: func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			close(transitionStarted)
			<-releaseTransition
			return semdb.OwnershipTransitionResult{}, nil
		},
		ApplyNoteIndexBatch: func(context.Context, []codeanchor.NoteIndexWork) error {
			payloadApplied <- struct{}{}
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.StopAndWait() })

	transitionDone := make(chan error, 1)
	go func() {
		_, err := q.SubmitOwnershipTransitions(context.Background(), []semdb.OwnershipTransition{{Path: "note.md", Target: semdb.OwnershipTargetUnowned}})
		transitionDone <- err
	}()
	select {
	case <-transitionStarted:
	case <-time.After(time.Second):
		t.Fatal("ownership transition handler did not start")
	}

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "after.md"}))
	select {
	case <-payloadApplied:
		t.Fatal("payload applied before ownership transition handler completed")
	case <-time.After(30 * time.Millisecond):
	}

	close(releaseTransition)
	require.NoError(t, <-transitionDone)
	select {
	case <-payloadApplied:
	case <-time.After(time.Second):
		t.Fatal("post-transition payload did not apply")
	}
	q.cancel()
}

func TestQueuedWriterTransitionCallerCancellationAfterEnqueueStillCommits(t *testing.T) {
	t.Parallel()

	payloadStarted := make(chan struct{})
	releasePayload := make(chan struct{})
	transitionApplied := make(chan struct{}, 1)
	q := newWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, _ []codeanchor.NoteIndexWork) error {
			close(payloadStarted)
			select {
			case <-releasePayload:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		ApplyOwnershipTransitions: func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			transitionApplied <- struct{}{}
			return semdb.OwnershipTransitionResult{}, nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.StopAndWait() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "before.md"}))
	select {
	case <-payloadStarted:
	case <-time.After(time.Second):
		t.Fatal("prior payload handler did not start")
	}

	ctx, cancel := context.WithCancel(context.Background())
	transitionDone := make(chan error, 1)
	go func() {
		_, err := q.SubmitOwnershipTransitions(ctx, []semdb.OwnershipTransition{{Path: "note.md", Target: semdb.OwnershipTargetUnowned}})
		transitionDone <- err
	}()
	require.Eventually(t, func() bool { return len(q.ctrlCh) == 1 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-transitionDone, context.Canceled)

	close(releasePayload)
	select {
	case <-transitionApplied:
	case <-time.After(time.Second):
		t.Fatal("enqueued transition did not commit after caller cancellation")
	}
	require.NoError(t, q.Close())
}
