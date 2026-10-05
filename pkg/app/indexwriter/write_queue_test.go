package indexwriter

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
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
	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			once.Do(func() {
				close(started)
				<-release
			})
			return wantErr
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
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

func TestQueuedWriterPublishesNoteMetadataBeforeOntology(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}
	q := New(context.Background(), Handlers{
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error {
			record("metadata")
			return nil
		},
		ApplyOntologyDelta: func(context.Context, semdb.OntologyDelta) error {
			record("ontology")
			return nil
		},
	})
	defer func() { _ = q.Close() }()

	require.NoError(t, q.SubmitNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{{Path: "note.md"}},
	}))
	require.NoError(t, q.SubmitOntologyDelta(context.Background(), semdb.OntologyDelta{FullRebuild: true, DeletePaths: []string{"old.md"}}))
	require.NoError(t, q.FlushAndWait(context.Background()))
	require.Equal(t, []string{"metadata", "ontology"}, order)
}

func TestQueuedWriterNoteMetadataFailureReachesBarrier(t *testing.T) {
	want := errors.New("metadata write failed")
	q := New(context.Background(), Handlers{
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error { return want },
	})
	require.NoError(t, q.SubmitNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
	}))
	require.ErrorIs(t, q.FlushAndWait(context.Background()), want)
}

func TestQueuedWriterAcksPendingControlsOnContextExit(t *testing.T) {
	t.Parallel()

	q := NewWithConfig(context.Background(), Handlers{}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.StopAndWait() })

	ack := make(chan error, 1)
	require.NoError(t, q.sendControl(context.Background(), queueFlush{barrier: 999, ack: ack}))
	require.Eventually(t, func() bool { return len(q.ctrlCh) == 0 }, time.Second, time.Millisecond)
	q.cancel()

	select {
	case err := <-ack:
		require.ErrorIs(t, err, context.Canceled)
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
	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(context.Context, []codeanchor.NoteIndexWork) error {
			order = append(order, "payload")
			return nil
		},
		ApplyOwnershipTransitions: func(_ context.Context, transitions []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			order = append(order, "transition:"+transitions[0].Path)
			return want, nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
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
	q := NewWithConfig(context.Background(), Handlers{
		ApplyOwnershipTransitions: func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			close(transitionStarted)
			<-releaseTransition
			return semdb.OwnershipTransitionResult{}, nil
		},
		ApplyNoteIndexBatch: func(context.Context, []codeanchor.NoteIndexWork) error {
			payloadApplied <- struct{}{}
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
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
	q := NewWithConfig(context.Background(), Handlers{
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
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
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

func TestQueuedWriterContextCancellationAcknowledgesPendingOwnershipControls(t *testing.T) {
	t.Parallel()

	queueCtx, cancelQueue := context.WithCancel(context.Background())
	payloadStarted := make(chan struct{})
	ownershipHandlerRan := make(chan string, 2)
	q := NewWithConfig(queueCtx, Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, _ []codeanchor.NoteIndexWork) error {
			close(payloadStarted)
			<-ctx.Done()
			return ctx.Err()
		},
		ApplyOwnershipTransitions: func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error) {
			ownershipHandlerRan <- "transition"
			return semdb.OwnershipTransitionResult{}, nil
		},
		AcknowledgeOwnershipReconciliation: func(context.Context, int64) (bool, error) {
			ownershipHandlerRan <- "acknowledgement"
			return true, nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(cancelQueue)

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "before.md"}))
	select {
	case <-payloadStarted:
	case <-time.After(time.Second):
		t.Fatal("prior payload handler did not start")
	}

	transitionDone := make(chan error, 1)
	go func() {
		_, err := q.SubmitOwnershipTransitions(context.Background(), []semdb.OwnershipTransition{{Path: "note.md", Target: semdb.OwnershipTargetUnowned}})
		transitionDone <- err
	}()
	ackDone := make(chan error, 1)
	go func() { ackDone <- q.AcknowledgeOwnershipReconciliation(context.Background(), 7) }()
	require.Eventually(t, func() bool { return len(q.ctrlCh) == 2 }, time.Second, time.Millisecond)

	cancelQueue()
	select {
	case err := <-transitionDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("pending ownership transition was not acknowledged on queue cancellation")
	}
	select {
	case err := <-ackDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("pending ownership acknowledgement was not acknowledged on queue cancellation")
	}
	select {
	case handler := <-ownershipHandlerRan:
		t.Fatalf("ownership %s handler ran after queue cancellation", handler)
	default:
	}
}

func TestQueuedWriterFlushesValidationStateAfterIndexWrites(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "notes")
			return nil
		},
		ApplyValidationState: func(ctx context.Context, write ValidationStateWrite) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "validation:"+write.Snapshot.VaultIdentity)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "note.md"}))
	require.NoError(t, q.SubmitValidationSnapshot(context.Background(), semdb.ValidationSnapshot{VaultIdentity: "vault"}, "", 12))
	require.NoError(t, q.FlushAndWait(context.Background()))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"notes", "validation:vault"}, order)
}

func TestQueuedWriterAttributesFlushToProducerPhase(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	q := NewWithConfig(ctx, Handlers{
		ApplyIntelEmbeddings: func(ctx context.Context, rows map[string]embeddings.Embedding) error {
			indexingperf.ObserveDBWrite(ctx, "intel-store", 0, 2*time.Millisecond)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 500, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	producerCtx := indexingperf.WithPhase(ctx, "embed_code")
	require.NoError(t, q.SubmitIntelEmbeddings(producerCtx, map[string]embeddings.Embedding{
		"chunk-1": {1, 2, 3},
	}))
	require.NoError(t, q.Close())

	window := collector.RenderWindow(10 * time.Second)
	require.Contains(t, window, "phase=embed_code")
	require.Contains(t, window, "db_hold_cum_ms=2")

	summary := collector.RenderSummary()
	require.Contains(t, summary, "intel.upsert_embeddings")
	require.NotContains(t, summary, "unknown")
}

func TestQueuedWriterUsesEmbedFlushPolicyForItemEmbeddings(t *testing.T) {
	t.Parallel()

	flushes := make(chan int, 2)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			flushes <- len(batch)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	for i := 0; i < 5; i++ {
		require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
			AnchorID:  codeindex.AnchorID("a"),
			Hash:      "h",
			Embedding: embeddings.Embedding{float32(i + 1)},
		}))
	}
	require.NoError(t, q.Close())

	first := <-flushes
	second := <-flushes
	require.Equal(t, 3, first)
	require.Equal(t, 2, second)
}

func TestQueuedWriterAcceptsCodeItemEmbeddingBatchSubmission(t *testing.T) {
	t.Parallel()

	var (
		calls int
		rows  int
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			calls++
			rows += len(batch)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitCodeItemEmbeddingBatch(context.Background(), []codeindex.ItemEmbeddingUpsert{
		{AnchorID: "a", Hash: "h1", Embedding: embeddings.Embedding{1}},
		{AnchorID: "b", Hash: "h2", Embedding: embeddings.Embedding{2}},
	}))
	require.NoError(t, q.Close())

	require.Equal(t, 1, calls)
	require.Equal(t, 2, rows)
}

func TestQueuedWriterInvokesPostCodeBatchCallback(t *testing.T) {
	t.Parallel()

	called := make(chan []string, 1)
	q := New(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
	})
	q.SetAfterCodeIndexBatch(func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
		paths := make([]string, 0, len(batch))
		for _, work := range batch {
			paths = append(paths, work.Path)
		}
		called <- paths
		return nil
	})

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "a.go"}))
	require.NoError(t, q.Close())
	require.Equal(t, []string{"a.go"}, <-called)
}

func TestQueuedWriterUsesCodeIndexFlushPolicyForCodeWork(t *testing.T) {
	t.Parallel()

	flushes := make(chan int, 2)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			flushes <- len(batch)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	for i := 0; i < 5; i++ {
		require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{
			Path: "file.go",
			Summary: codeanchor.FileSummary{
				FilePath: "file.go",
			},
		}))
	}
	require.NoError(t, q.Close())

	first := <-flushes
	second := <-flushes
	require.Equal(t, 3, first)
	require.Equal(t, 2, second)
}

func TestQueuedWriterFlushAllCarriesExternalEvidenceWithCodeWork(t *testing.T) {
	t.Parallel()

	got := make(chan codeanchor.ExternalEvidenceBatch, 1)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(_ context.Context, batch []codeanchor.CodeIndexWork) error {
			require.Len(t, batch, 1)
			got <- batch[0].ExternalEvidence
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{
		Path: "component.tsx",
		ExternalEvidence: codeanchor.ExternalEvidenceBatch{Imports: []codeanchor.ExternalImportEvidenceInput{{
			Module: "react", Target: codeanchor.ExternalTarget{Handle: "npm:react#module", ExternalTargetIdentity: codeanchor.ExternalTargetIdentity{Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", Kind: codeanchor.ExternalTargetModule}},
		}}},
	}))
	require.NoError(t, q.FlushAndWait(context.Background()))
	require.Len(t, (<-got).Imports, 1)
	require.NoError(t, q.Close())
}

func TestQueuedWriterFlushExpiredCarriesExternalEvidenceWithCodeWork(t *testing.T) {
	t.Parallel()

	got := make(chan codeanchor.ExternalEvidenceBatch, 1)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(_ context.Context, batch []codeanchor.CodeIndexWork) error {
			got <- batch[0].ExternalEvidence
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		CodeIndex:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  5 * time.Millisecond,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{
		Path:             "component.tsx",
		ExternalEvidence: codeanchor.ExternalEvidenceBatch{Symbols: []codeanchor.ExternalSymbolEvidenceInput{{OwnerFQN: "Counter"}}},
	}))
	select {
	case evidence := <-got:
		require.Len(t, evidence.Symbols, 1)
	case <-time.After(time.Second):
		t.Fatal("idle flush did not persist external evidence")
	}
}

func TestQueuedWriterDefersSemanticFlushWhileCodeIndexIsHot(t *testing.T) {
	t.Parallel()

	flushed := make(chan int, 1)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			flushed <- len(batch)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		ItemEmbed:     FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  10 * time.Millisecond,
		CodePriority:  time.Hour,
		DeferScale:    4,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "a.go"}))
	require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
		AnchorID:  codeindex.AnchorID("a"),
		Hash:      "h",
		Embedding: embeddings.Embedding{1, 2, 3},
	}))

	select {
	case <-flushed:
		t.Fatal("semantic flush should defer while code indexing is hot")
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, q.Close())
	require.Equal(t, 1, <-flushed)
}

func TestQueuedWriterBatchesCodeChunkJobsWhenBatchHandlerIsAvailable(t *testing.T) {
	t.Parallel()

	var (
		calls int
		rows  int
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeItemChunkBatch: func(ctx context.Context, items []codeindex.ItemChunksUpsert) error {
			calls++
			for _, item := range items {
				rows += len(item.Chunks)
			}
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitCodeItemChunks(context.Background(), "a1", []codeindex.ChunkInput{{Index: 0}}, []string{"a"}, []embeddings.Embedding{{1}}))
	require.NoError(t, q.SubmitCodeItemChunks(context.Background(), "a2", []codeindex.ChunkInput{{Index: 0}}, []string{"b"}, []embeddings.Embedding{{2}}))
	require.NoError(t, q.Close())

	require.Equal(t, 1, calls)
	require.Equal(t, 2, rows)
}

func TestQueuedWriterAcceptsCodeChunkBatchSubmission(t *testing.T) {
	t.Parallel()

	var (
		calls int
		rows  int
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeItemChunkBatch: func(ctx context.Context, items []codeindex.ItemChunksUpsert) error {
			calls++
			for _, item := range items {
				rows += len(item.Chunks)
			}
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitCodeItemChunkBatch(context.Background(), []codeindex.ItemChunksUpsert{
		{AnchorID: "a1", Chunks: []codeindex.ChunkInput{{Index: 0}}, Texts: []string{"a"}, Embeddings: []embeddings.Embedding{{1}}},
		{AnchorID: "a2", Chunks: []codeindex.ChunkInput{{Index: 1}}, Texts: []string{"b"}, Embeddings: []embeddings.Embedding{{2}}},
	}))
	require.NoError(t, q.Close())

	require.Equal(t, 1, calls)
	require.Equal(t, 2, rows)
}

func TestQueuedWriterRecordsNoteWritebackMetrics(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	q := NewWithConfig(ctx, Handlers{
		ApplyNoteMetaBatch: func(ctx context.Context, batch []embeddings.NoteFileInfo) error {
			return nil
		},
		ApplyNoteChunkSyncBatch: func(ctx context.Context, batch []embeddings.NoteChunkSync) error {
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 2, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	planCtx := indexingperf.WithPhase(ctx, "plan_note_embeddings")
	embedCtx := indexingperf.WithPhase(ctx, "embed_notes")
	require.NoError(t, q.SubmitNoteMeta(planCtx, embeddings.NoteFileInfo{ID: "a.md", Path: "a.md", Title: "A"}))
	require.NoError(t, q.SubmitNoteMeta(planCtx, embeddings.NoteFileInfo{ID: "b.md", Path: "b.md", Title: "B"}))
	require.NoError(t, q.SubmitNoteChunkSync(embedCtx, embeddings.NoteChunkSync{
		NoteID:      "a.md",
		Chunks:      []embeddings.ChunkInput{{Index: 0}},
		Embeddings:  []embeddings.Embedding{{1}},
		KeepIndices: []int{0},
	}))
	require.NoError(t, q.SubmitNoteChunkSync(embedCtx, embeddings.NoteChunkSync{
		NoteID:      "a.md",
		Chunks:      []embeddings.ChunkInput{{Index: 1}},
		Embeddings:  []embeddings.Embedding{{2}},
		KeepIndices: []int{1},
	}))
	require.NoError(t, q.Close())
	indexingperf.FromContext(ctx).RecordSpan("plan_note_embeddings", 20*time.Millisecond, nil)
	indexingperf.FromContext(ctx).RecordSpan("embed_notes", 20*time.Millisecond, nil)

	summary := collector.RenderSummary()
	require.Contains(t, summary, "writeback_meta_flushes=1")
	require.Contains(t, summary, "writeback_meta_queue_depth_peak=2")
	require.Contains(t, summary, "writeback_note_chunk_flushes=1")
	require.Contains(t, summary, "writeback_note_chunk_queue_depth_peak=2")
}

func TestQueuedWriterRecordsValidationMetadataMetrics(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	q := NewWithConfig(ctx, Handlers{
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error { return nil },
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(ctx, "validation_projection_source")
	require.NoError(t, q.SubmitNoteMetadataDelta(phaseCtx, semdb.NoteMetadataDelta{
		Notes: []semdb.NoteMetadataRow{{Path: "a.md"}},
	}))
	require.NoError(t, q.FlushAndWait(phaseCtx))
	require.NoError(t, q.Close())
	indexingperf.FromContext(ctx).RecordSpan("validation_projection_source", 20*time.Millisecond, nil)

	summary := collector.RenderSummary()
	require.Contains(t, summary, "writeback_validation_metadata_flushes=1")
	require.Contains(t, summary, "writeback_validation_metadata_batch_rows_max=2")
	require.Contains(t, summary, "writeback_validation_metadata_queue_depth_peak=2")
}

func TestQueuedWriterRecordsValidationMetadataSubmitWaitUnderBackpressure(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWrite) }) }

	q := NewWithConfig(ctx, Handlers{
		ApplyCodeIndexBatch: func(context.Context, []codeanchor.CodeIndexWork) error {
			close(writeStarted)
			<-releaseWrite
			return nil
		},
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error { return nil },
		ApplyValidationState:   func(context.Context, ValidationStateWrite) error { return nil },
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() {
		release()
		_ = q.Close()
	})

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "blocked.go"}))
	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("writer never entered blocking code batch")
	}
	for i := 0; i < cap(q.otherCmdCh); i++ {
		q.otherCmdCh <- queuedPayload{
			value: ValidationStateWrite{Snapshot: semdb.ValidationSnapshot{VaultIdentity: fmt.Sprintf("queued-%d", i)}},
			bytes: 1,
			phase: "validation_projection_source",
		}
	}

	phaseCtx := indexingperf.WithPhase(ctx, "validation_projection_source")
	submitDone := make(chan error, 1)
	submitBarrier := q.currentSubmitBarrier()
	go func() {
		submitDone <- q.SubmitNoteMetadataDelta(phaseCtx, semdb.NoteMetadataDelta{
			Notes: []semdb.NoteMetadataRow{{Path: "a.md"}},
		})
	}()
	deadline := time.After(time.Second)
	for q.currentSubmitBarrier() == submitBarrier {
		select {
		case <-deadline:
			t.Fatal("metadata submit never entered the queue")
		default:
			runtime.Gosched()
		}
	}
	select {
	case err := <-submitDone:
		t.Fatalf("metadata submit returned before backpressure released: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	release()
	select {
	case err := <-submitDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("metadata submit did not resume after backpressure released")
	}
	require.NoError(t, q.Close())
	collector.RecordSpan("validation_projection_source", 20*time.Millisecond, nil)

	summary := collector.RenderSummary()
	require.Contains(t, summary, "writeback_validation_metadata_submit_wait_cum=")
}

func TestQueuedWriterBatchesNoteChunkJobsWhenBatchHandlerIsAvailable(t *testing.T) {
	t.Parallel()

	var (
		calls int
		rows  int
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteChunkSyncBatch: func(ctx context.Context, items []embeddings.NoteChunkSync) error {
			calls++
			for _, item := range items {
				rows += len(item.Chunks)
			}
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitNoteChunkSync(context.Background(), embeddings.NoteChunkSync{
		NoteID:      "n1",
		Chunks:      []embeddings.ChunkInput{{Index: 0}},
		Embeddings:  []embeddings.Embedding{{1}},
		KeepIndices: []int{0},
	}))
	require.NoError(t, q.SubmitNoteChunkSync(context.Background(), embeddings.NoteChunkSync{
		NoteID:      "n2",
		Chunks:      []embeddings.ChunkInput{{Index: 0}},
		Embeddings:  []embeddings.Embedding{{2}},
		KeepIndices: []int{0},
	}))
	require.NoError(t, q.Close())

	require.Equal(t, 1, calls)
	require.Equal(t, 2, rows)
}

func TestQueuedWriterFlushesCleanupOnlyNoteChunkSyncBatch(t *testing.T) {
	t.Parallel()

	var (
		calls int
		items []embeddings.NoteChunkSync
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteChunkSyncBatch: func(ctx context.Context, batch []embeddings.NoteChunkSync) error {
			calls++
			items = append(items, batch...)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitNoteChunkSync(context.Background(), embeddings.NoteChunkSync{
		NoteID:      "n1",
		KeepIndices: []int{2, 4},
	}))
	require.NoError(t, q.Close())

	require.Equal(t, 1, calls)
	require.Len(t, items, 1)
	require.Equal(t, []int{2, 4}, items[0].KeepIndices)
	require.Empty(t, items[0].Chunks)
}

func TestQueuedWriterFlushesEmptyNoteChunkSyncBatch(t *testing.T) {
	t.Parallel()

	var calls int
	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteChunkSyncBatch: func(ctx context.Context, batch []embeddings.NoteChunkSync) error {
			calls++
			require.Len(t, batch, 1)
			require.Equal(t, embeddings.NoteID("n-empty"), batch[0].NoteID)
			require.Empty(t, batch[0].Chunks)
			require.Empty(t, batch[0].KeepIndices)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitNoteChunkSync(context.Background(), embeddings.NoteChunkSync{
		NoteID: "n-empty",
	}))
	require.NoError(t, q.Close())
	require.Equal(t, 1, calls)
}

func TestQueuedWriterAllowsUrgentSemanticFlushWhileCodeIndexIsHot(t *testing.T) {
	t.Parallel()

	flushed := make(chan int, 1)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			flushed <- len(batch)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		ItemEmbed:     FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  10 * time.Millisecond,
		CodePriority:  time.Hour,
		DeferScale:    2,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "a.go"}))
	require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
		AnchorID:  codeindex.AnchorID("a"),
		Hash:      "h1",
		Embedding: embeddings.Embedding{1, 2, 3},
	}))
	require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
		AnchorID:  codeindex.AnchorID("b"),
		Hash:      "h2",
		Embedding: embeddings.Embedding{4, 5, 6},
	}))

	select {
	case n := <-flushed:
		require.Equal(t, 2, n)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("urgent semantic flush should bypass code-hot defer")
	}
}

func TestQueuedWriterAllowsCodeSubmitWhenOtherLaneIsBackedUp(t *testing.T) {
	t.Parallel()

	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var firstBatch sync.Once

	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			firstBatch.Do(func() {
				close(firstBatchStarted)
				<-releaseFirstBatch
			})
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "first.md"}))
	select {
	case <-firstBatchStarted:
	case <-time.After(time.Second):
		t.Fatal("writer never entered first note batch")
	}

	for i := 0; i < cap(q.otherCmdCh); i++ {
		q.otherCmdCh <- queuedPayload{
			value: codeanchor.NoteIndexWork{Path: fmt.Sprintf("queued-%03d.md", i)},
			bytes: 1,
			phase: "index_code",
		}
	}

	blockedDone := make(chan error, 1)
	go func() {
		blockedDone <- q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "blocked.md"})
	}()

	select {
	case err := <-blockedDone:
		t.Fatalf("blocked note submit returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	submitCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, q.SubmitCodeIndexWork(submitCtx, codeanchor.CodeIndexWork{Path: "file.go"}))

	close(releaseFirstBatch)

	select {
	case err := <-blockedDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("blocked note submit did not complete")
	}
}

func TestQueuedWriterPrefersCodeLaneWhileCodeIsQueued(t *testing.T) {
	t.Parallel()

	order := make(chan string, 2)
	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var noteCalls atomic.Int32
	q := NewWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			order <- "code"
			return nil
		},
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			if noteCalls.Add(1) == 1 {
				close(firstBatchStarted)
				<-releaseFirstBatch
				return nil
			}
			order <- "other"
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	q.otherCmdCh <- queuedPayload{
		value: codeanchor.NoteIndexWork{Path: "blocker.md"},
		bytes: 1,
		phase: "index_code",
	}
	select {
	case <-firstBatchStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked note batch did not start")
	}

	q.otherCmdCh <- queuedPayload{
		value: codeanchor.NoteIndexWork{Path: "note.md"},
		bytes: 1,
		phase: "index_code",
	}
	q.codeCmdCh <- queuedPayload{
		value: codeanchor.CodeIndexWork{Path: "file.go"},
		bytes: 1,
		phase: "index_code",
	}
	close(releaseFirstBatch)

	require.Equal(t, "code", <-order)
	require.Equal(t, "other", <-order)
}

func TestQueuedWriterFlushWaitIncludesBlockedSubmitter(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		processed []string
	)
	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var firstBatch sync.Once

	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			mu.Lock()
			for _, item := range batch {
				processed = append(processed, item.Path)
			}
			mu.Unlock()
			firstBatch.Do(func() {
				close(firstBatchStarted)
				<-releaseFirstBatch
			})
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "first.md"}))
	select {
	case <-firstBatchStarted:
	case <-time.After(time.Second):
		t.Fatal("writer never entered first note batch")
	}

	for i := 0; i < cap(q.otherCmdCh); i++ {
		q.otherCmdCh <- queuedPayload{
			value: codeanchor.NoteIndexWork{Path: fmt.Sprintf("queued-%03d.md", i)},
			bytes: 1,
			phase: "index_notes",
		}
	}

	submitDone := make(chan error, 1)
	go func() {
		submitDone <- q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "blocked.md"})
	}()

	select {
	case err := <-submitDone:
		t.Fatalf("blocked submit returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	flushDone := make(chan error, 1)
	go func() {
		flushDone <- q.FlushAndWait(context.Background())
	}()

	select {
	case err := <-flushDone:
		t.Fatalf("flush returned before releasing the first batch: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseFirstBatch)

	select {
	case err := <-flushDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("flush did not complete")
	}
	select {
	case err := <-submitDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("blocked submit did not complete")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, processed, "blocked.md")
}

func TestQueuedWriterCloseIncludesBlockedSubmitter(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		processed []string
	)
	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var firstBatch sync.Once

	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			mu.Lock()
			for _, item := range batch {
				processed = append(processed, item.Path)
			}
			mu.Unlock()
			firstBatch.Do(func() {
				close(firstBatchStarted)
				<-releaseFirstBatch
			})
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "first.md"}))
	select {
	case <-firstBatchStarted:
	case <-time.After(time.Second):
		t.Fatal("writer never entered first note batch")
	}

	for i := 0; i < cap(q.otherCmdCh); i++ {
		q.otherCmdCh <- queuedPayload{
			value: codeanchor.NoteIndexWork{Path: fmt.Sprintf("queued-%03d.md", i)},
			bytes: 1,
			phase: "index_notes",
		}
	}

	submitDone := make(chan error, 1)
	submitBarrier := q.currentSubmitBarrier()
	go func() {
		submitDone <- q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "blocked.md"})
	}()

	require.Eventually(t, func() bool {
		return q.currentSubmitBarrier() > submitBarrier
	}, time.Second, time.Millisecond, "blocked submit never entered the queue")

	select {
	case err := <-submitDone:
		t.Fatalf("blocked submit returned early: %v", err)
	default:
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- q.Close()
	}()

	require.Eventually(t, func() bool {
		return len(q.ctrlCh) > 0
	}, time.Second, time.Millisecond, "close never entered the control queue")

	select {
	case err := <-closeDone:
		t.Fatalf("close returned before releasing the first batch: %v", err)
	default:
	}

	close(releaseFirstBatch)

	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("close did not complete")
	}
	select {
	case err := <-submitDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("blocked submit did not complete")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, processed, "blocked.md")
}

func TestQueuedWriterFlushWaitIgnoresCanceledBlockedSubmitter(t *testing.T) {
	t.Parallel()

	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var firstBatch sync.Once

	q := NewWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			firstBatch.Do(func() {
				close(firstBatchStarted)
				<-releaseFirstBatch
			})
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "first.md"}))
	select {
	case <-firstBatchStarted:
	case <-time.After(time.Second):
		t.Fatal("writer never entered first note batch")
	}

	for i := 0; i < cap(q.otherCmdCh); i++ {
		q.otherCmdCh <- queuedPayload{
			value: codeanchor.NoteIndexWork{Path: fmt.Sprintf("queued-%03d.md", i)},
			bytes: 1,
			phase: "index_notes",
		}
	}

	submitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	canceledDone := make(chan error, 1)
	go func() {
		canceledDone <- q.SubmitNoteIndexWork(submitCtx, codeanchor.NoteIndexWork{Path: "canceled.md"})
	}()

	select {
	case err := <-canceledDone:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("canceled submit did not return")
	}

	flushDone := make(chan error, 1)
	go func() {
		flushDone <- q.FlushAndWait(context.Background())
	}()

	select {
	case err := <-flushDone:
		t.Fatalf("flush returned before releasing the first batch: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseFirstBatch)

	select {
	case err := <-flushDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("flush did not complete after canceled submit")
	}
}

func TestQueuedWriterBatchesOntologyDeltasByPhase(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls int
		last  semdb.OntologyDelta
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyOntologyDelta: func(ctx context.Context, delta semdb.OntologyDelta) error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			last = delta
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(context.Background(), "write_ontology")
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		ReplacePaths: []string{"notes/a.md"},
		EdgeSources:  []string{"notes/a.md"},
		Assessments: []semdb.OntologyNoteAssessmentRow{
			{NotePath: "notes/a.md", AssessmentJSON: "{}", SchemaHash: "abc"},
		},
	}))
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		ReplacePaths: []string{"notes/b.md"},
		EdgeSources:  []string{"notes/b.md"},
		Assessments: []semdb.OntologyNoteAssessmentRow{
			{NotePath: "notes/b.md", AssessmentJSON: "{}", SchemaHash: "abc"},
		},
	}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls)
	require.ElementsMatch(t, []string{"notes/a.md", "notes/b.md"}, last.ReplacePaths)
	require.Len(t, last.Assessments, 2)
}

func TestQueuedWriterFlushesOntologyNodeBodyWrites(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		nodePaths  []string
		nodeRows   []codeanchor.IntelOntologyNode
		fieldRows  []codeanchor.IntelOntologyNodeFieldValue
		linkDeps   []codeanchor.IntelOntologyNodeLinkDependency
		stateRows  []codeanchor.IntelOntologyNodeEmbeddingState
		embeddingN int
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyOntologyNodeReadModel: func(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
			mu.Lock()
			defer mu.Unlock()
			nodePaths = append(nodePaths, model.NotePaths...)
			nodeRows = append(nodeRows, model.Nodes...)
			fieldRows = append(fieldRows, model.FieldValues...)
			linkDeps = append(linkDeps, model.LinkDependencies...)
			return nil
		},
		ApplyOntologyNodeStates: func(ctx context.Context, states []codeanchor.IntelOntologyNodeEmbeddingState) error {
			mu.Lock()
			defer mu.Unlock()
			stateRows = append(stateRows, states...)
			return nil
		},
		ApplyIntelEmbeddings: func(ctx context.Context, rows map[string]embeddings.Embedding) error {
			mu.Lock()
			defer mu.Unlock()
			embeddingN += len(rows)
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	metrics := indexingperf.New()
	phaseCtx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), metrics), "sync_ontology_body")
	require.NoError(t, q.SubmitOntologyNodeReadModel(phaseCtx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"notes/a.md"},
		Nodes: []codeanchor.IntelOntologyNode{{
			NodeID:      "node:a",
			NotePath:    "notes/a.md",
			NodeRefJSON: "{}",
		}},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{{
			NodeID:    "node:a",
			NotePath:  "notes/a.md",
			TypeName:  "Spec",
			FieldName: "status",
			ValueNorm: "active",
		}},
		LinkDependencies: []codeanchor.IntelOntologyNodeLinkDependency{{
			SourceNotePath:         "notes/a.md",
			NodeID:                 "node:a",
			TypeName:               "Spec",
			FieldName:              "owner",
			TargetInput:            "Alice",
			TargetInputNorm:        "alice",
			ResolvedTargetNotePath: "people/alice.md",
		}},
	}))
	require.NoError(t, q.SubmitOntologyNodeEmbeddingStates(phaseCtx, []codeanchor.IntelOntologyNodeEmbeddingState{{
		ChunkID:  "chunk:a",
		NodeID:   "node:a",
		NotePath: "notes/a.md",
	}}))
	require.NoError(t, q.SubmitIntelEmbeddings(phaseCtx, map[string]embeddings.Embedding{"chunk:a": embeddings.Embedding{0.1}}))
	require.NoError(t, q.Close())
	metrics.RecordSpan("sync_ontology_body", time.Millisecond, nil)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"notes/a.md"}, nodePaths)
	require.Len(t, nodeRows, 1)
	require.Len(t, fieldRows, 1)
	require.Equal(t, "status", fieldRows[0].FieldName)
	require.Len(t, linkDeps, 1)
	require.Equal(t, "people/alice.md", linkDeps[0].ResolvedTargetNotePath)
	require.Len(t, stateRows, 1)
	require.Equal(t, 1, embeddingN)
	require.Contains(t, metrics.RenderSummary(), "field_rows_planned=1")
}

func TestQueuedWriterFlushesOntologyDeltaBeforeReadModel(t *testing.T) {
	// Regression: a FullRebuild ApplyOntologyDelta truncates ontology_nodes /
	// ontology_node_field_values. If the read-model batch flushed first, those
	// inserts would be wiped by the trailing delta. flushAll must drain
	// ontologyOps before ontologyNodes; this test pins that order so a
	// future reorder of flushAll cannot silently regress catalog writes.
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyOntologyDelta: func(ctx context.Context, delta semdb.OntologyDelta) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "delta")
			return nil
		},
		ApplyOntologyNodeReadModel: func(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "read-model")
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(context.Background(), "sync_ontology")
	// Submit read-model FIRST to make the order assertion meaningful: the
	// queue must still drain the delta before the read-model.
	require.NoError(t, q.SubmitOntologyNodeReadModel(phaseCtx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"notes/a.md"},
		Nodes: []codeanchor.IntelOntologyNode{{
			NodeID:      "node:a",
			NotePath:    "notes/a.md",
			NodeRefJSON: "{}",
		}},
	}))
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		FullRebuild: true,
		Assessments: []semdb.OntologyNoteAssessmentRow{{NotePath: "notes/a.md"}},
	}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"delta", "read-model"}, order, "ontology delta must drain before read-model so FullRebuild truncate cannot wipe freshly-inserted catalog rows")
}

func TestQueuedWriterDoesNotThresholdFlushOntologyReadModelBeforeFullRebuild(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	flushed := make(chan string, 2)
	q := NewWithConfig(context.Background(), Handlers{
		ApplyOntologyDelta: func(ctx context.Context, delta semdb.OntologyDelta) error {
			mu.Lock()
			order = append(order, "delta")
			mu.Unlock()
			flushed <- "delta"
			return nil
		},
		ApplyOntologyNodeReadModel: func(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
			mu.Lock()
			order = append(order, "read-model")
			mu.Unlock()
			flushed <- "read-model"
			return nil
		},
	}, Config{
		DefaultPolicy: FlushPolicy{Rows: 1, Bytes: 1, Idle: time.Hour},
		ItemEmbed:     FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(context.Background(), "sync_ontology")
	require.NoError(t, q.SubmitOntologyNodeReadModel(phaseCtx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"notes/a.md"},
		Nodes:     []codeanchor.IntelOntologyNode{{NodeID: "node:a", NotePath: "notes/a.md", NodeRefJSON: "{}"}},
	}))
	select {
	case got := <-flushed:
		t.Fatalf("ontology read-model flushed before FullRebuild delta was enqueued: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		FullRebuild: true,
		Assessments: []semdb.OntologyNoteAssessmentRow{{NotePath: "notes/a.md"}},
	}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"delta", "read-model"}, order)
}
