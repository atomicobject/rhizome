package indexwriter

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestQueuedWriterContextCancellationAcknowledgesPendingOwnershipControls(t *testing.T) {
	t.Parallel()

	queueCtx, cancelQueue := context.WithCancel(context.Background())
	payloadStarted := make(chan struct{})
	ownershipHandlerRan := make(chan string, 2)
	q := newWithConfig(queueCtx, Handlers{
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
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
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

func TestQueuedWriterAttributesFlushToProducerPhase(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	q := newWithConfig(ctx, Handlers{
		ApplyIntelEmbeddings: func(ctx context.Context, rows map[string]embeddings.Embedding) error {
			indexingperf.ObserveDBWrite(ctx, "intel-store", 0, 2*time.Millisecond)
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 500, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
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

func TestQueuedWriterFlushWaitIncludesBlockedSubmitter(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		processed []string
	)
	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var firstBatch sync.Once

	q := newWithConfig(context.Background(), Handlers{
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
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
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

	q := newWithConfig(context.Background(), Handlers{
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
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
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

	q := newWithConfig(context.Background(), Handlers{
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			firstBatch.Do(func() {
				close(firstBatchStarted)
				<-releaseFirstBatch
			})
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
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
