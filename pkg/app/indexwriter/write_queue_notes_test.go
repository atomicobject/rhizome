package indexwriter

import (
	"context"
	"errors"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"runtime"
	"sync"
	"testing"
	"time"
)

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

func TestQueuedWriterFlushesValidationStateAfterIndexWrites(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	q := newWithConfig(context.Background(), Handlers{
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
	}, config{
		DefaultPolicy: flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
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

func TestQueuedWriterRecordsNoteWritebackMetrics(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	q := newWithConfig(ctx, Handlers{
		ApplyNoteMetaBatch: func(ctx context.Context, batch []embeddings.NoteFileInfo) error {
			return nil
		},
		ApplyNoteChunkSyncBatch: func(ctx context.Context, batch []embeddings.NoteChunkSync) error {
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 2, Bytes: 1 << 20, Idle: time.Hour},
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
	q := newWithConfig(ctx, Handlers{
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error { return nil },
	}, config{
		DefaultPolicy: flushPolicy{Rows: 10, Bytes: 1 << 20, Idle: time.Hour},
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

	q := newWithConfig(ctx, Handlers{
		ApplyCodeIndexBatch: func(context.Context, []codeanchor.CodeIndexWork) error {
			close(writeStarted)
			<-releaseWrite
			return nil
		},
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error { return nil },
		ApplyValidationState:   func(context.Context, ValidationStateWrite) error { return nil },
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour},
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

func TestQueuedWriterBatchesNoteChunkSyncs(t *testing.T) {
	t.Parallel()
	cases := map[string][]embeddings.NoteChunkSync{
		"chunks":  {{NoteID: "one", Chunks: []embeddings.ChunkInput{{Index: 0}}, Embeddings: []embeddings.Embedding{{1}}, KeepIndices: []int{0}}, {NoteID: "two", Chunks: []embeddings.ChunkInput{{Index: 0}}, Embeddings: []embeddings.Embedding{{2}}, KeepIndices: []int{0}}},
		"cleanup": {{NoteID: "one", KeepIndices: []int{2, 4}}},
		"empty":   {{NoteID: "empty"}},
	}
	for name, items := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var batches int
			var delivered []embeddings.NoteChunkSync
			cfg := defaultConfig()
			cfg.DefaultPolicy = flushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour}
			cfg.PollInterval = time.Hour
			q := newWithConfig(context.Background(), Handlers{ApplyNoteChunkSyncBatch: func(_ context.Context, batch []embeddings.NoteChunkSync) error {
				batches++
				delivered = append(delivered, batch...)
				return nil
			}}, cfg)
			for _, item := range items {
				require.NoError(t, q.SubmitNoteChunkSync(context.Background(), item))
			}
			require.NoError(t, q.Close())
			require.Equal(t, 1, batches)
			require.Len(t, delivered, len(items), "cleanup-only and empty items must remain observable writes")
		})
	}
}
