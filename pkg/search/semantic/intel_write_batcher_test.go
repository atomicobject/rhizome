package semantic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type recordingEmbeddingWriter struct {
	mu        sync.Mutex
	calls     int
	batchSize []int
	stored    map[string]embeddings.Embedding
	failCall  int
	failErr   error
}

type rejectCanceledEmbeddingWriter struct{ *recordingEmbeddingWriter }

func (w rejectCanceledEmbeddingWriter) UpsertEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return w.recordingEmbeddingWriter.UpsertEmbeddings(ctx, rows)
}

func (w *recordingEmbeddingWriter) UpsertEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.failCall > 0 && w.calls == w.failCall {
		if w.failErr != nil {
			return w.failErr
		}
		return errors.New("embedding write failed")
	}
	w.batchSize = append(w.batchSize, len(rows))
	if w.stored == nil {
		w.stored = make(map[string]embeddings.Embedding)
	}
	for chunkID, vec := range rows {
		cp := make(embeddings.Embedding, len(vec))
		copy(cp, vec)
		w.stored[chunkID] = cp
	}
	return nil
}

func (w *recordingEmbeddingWriter) CallCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func (w *recordingEmbeddingWriter) BatchSizes() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]int, len(w.batchSize))
	copy(out, w.batchSize)
	return out
}

func (w *recordingEmbeddingWriter) StoredCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.stored)
}

func TestBatchedIntelEmbeddingWriterFlushesBySizeAndClose(t *testing.T) {
	writer := &recordingEmbeddingWriter{}
	batcher := newBatchedIntelEmbeddingWriter(context.Background(), writer, intelEmbeddingBatchWriterOptions{
		FlushSize:     3,
		FlushInterval: time.Hour,
	})

	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c1": {1, 0, 0},
	}))
	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c2": {0, 1, 0},
		"c3": {0, 0, 1},
	}))
	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c4": {1, 1, 0},
	}))
	require.NoError(t, batcher.Close())

	require.Equal(t, []int{3, 1}, writer.BatchSizes())
	require.Equal(t, 4, writer.StoredCount())
}

func TestBatchedIntelEmbeddingWriterFlushesByInterval(t *testing.T) {
	writer := &recordingEmbeddingWriter{}
	batcher := newBatchedIntelEmbeddingWriter(context.Background(), writer, intelEmbeddingBatchWriterOptions{
		FlushSize:     100,
		FlushInterval: 10 * time.Millisecond,
	})

	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c1": {1, 0, 0},
	}))
	require.Eventually(t, func() bool {
		return writer.CallCount() == 1
	}, 300*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, batcher.Close())
	require.Equal(t, 1, writer.StoredCount())
}

func TestBatchedIntelEmbeddingWriterReturnsFlushError(t *testing.T) {
	wantErr := errors.New("boom")
	writer := &recordingEmbeddingWriter{failCall: 1, failErr: wantErr}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batcher := newBatchedIntelEmbeddingWriter(ctx, writer, intelEmbeddingBatchWriterOptions{
		FlushSize:     1,
		FlushInterval: time.Hour,
		OnError:       func(error) { cancel() },
	})

	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c1": {1, 0, 0},
	}))
	err := batcher.Close()
	require.ErrorIs(t, err, wantErr)
}

func TestBatchedIntelEmbeddingWriterFlushesPendingRowsAfterCancel(t *testing.T) {
	writer := rejectCanceledEmbeddingWriter{&recordingEmbeddingWriter{}}
	ctx, cancel := context.WithCancel(context.Background())
	batcher := newBatchedIntelEmbeddingWriter(ctx, writer, intelEmbeddingBatchWriterOptions{
		FlushSize:     100,
		FlushInterval: time.Hour,
	})

	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c1": {1, 0, 0},
	}))
	cancel()

	require.NoError(t, batcher.Close())
	require.Equal(t, 1, writer.StoredCount())
}

func TestBatchedIntelEmbeddingWriterRecordsNoteMetrics(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_notes")
	writer := &recordingEmbeddingWriter{}
	batcher := newBatchedIntelEmbeddingWriter(ctx, writer, intelEmbeddingBatchWriterOptions{
		Label:         "notes",
		FlushSize:     2,
		FlushInterval: time.Hour,
	})

	require.NoError(t, batcher.Submit(map[string]embeddings.Embedding{
		"c1": {1, 0, 0},
		"c2": {0, 1, 0},
	}))
	require.NoError(t, batcher.Close())
	indexingperf.FromContext(ctx).RecordSpan("embed_notes", 20*time.Millisecond, nil)

	summary := collector.RenderSummary()
	require.Contains(t, summary, "embed_notes")
	require.Contains(t, summary, "writeback_note_intel_flushes=1")
	require.Contains(t, summary, "writeback_note_intel_batch_rows_p50=2")
	require.Contains(t, summary, "writeback_note_intel_queue_depth_peak=2")
}
