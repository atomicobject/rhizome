package semantic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
)

type recordingWritebackQueue struct {
	cfg CodeWritebackBatchConfig

	mu              sync.Mutex
	itemPhases      []string
	itemBatchSizes  []int
	chunkPhases     []string
	chunkRowTotals  []int
	intelPhases     []string
	intelBatchSizes []int

	itemSignal  chan struct{}
	intelSignal chan struct{}

	singleErr      error
	rejectCanceled bool
}

func (q *recordingWritebackQueue) CodeWritebackBatchConfig() CodeWritebackBatchConfig {
	return q.cfg
}

func (q *recordingWritebackQueue) SubmitCodeItemEmbedding(context.Context, codeidx.ItemEmbeddingUpsert) error {
	if q.singleErr != nil {
		return q.singleErr
	}
	return errors.New("unexpected single item write")
}

func (q *recordingWritebackQueue) SubmitCodeItemEmbeddingBatch(ctx context.Context, items []codeidx.ItemEmbeddingUpsert) error {
	if q.rejectCanceled && ctx.Err() != nil {
		return ctx.Err()
	}
	q.mu.Lock()
	q.itemPhases = append(q.itemPhases, indexingperf.PhaseFromContext(ctx))
	q.itemBatchSizes = append(q.itemBatchSizes, len(items))
	q.mu.Unlock()
	if q.itemSignal != nil {
		select {
		case q.itemSignal <- struct{}{}:
		default:
		}
	}
	return nil
}

func (q *recordingWritebackQueue) SubmitCodeItemChunks(context.Context, codeidx.AnchorID, []codeidx.ChunkInput, []string, []embeddings.Embedding) error {
	if q.singleErr != nil {
		return q.singleErr
	}
	return errors.New("unexpected single chunk write")
}

func (q *recordingWritebackQueue) SubmitCodeItemChunkBatch(ctx context.Context, items []codeidx.ItemChunksUpsert) error {
	if q.rejectCanceled && ctx.Err() != nil {
		return ctx.Err()
	}
	rows := 0
	for _, item := range items {
		rows += len(item.Chunks)
	}
	q.mu.Lock()
	q.chunkPhases = append(q.chunkPhases, indexingperf.PhaseFromContext(ctx))
	q.chunkRowTotals = append(q.chunkRowTotals, rows)
	q.mu.Unlock()
	return nil
}

func (q *recordingWritebackQueue) SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *recordingWritebackQueue) SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *recordingWritebackQueue) SubmitIntelEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	if q.rejectCanceled && ctx.Err() != nil {
		return ctx.Err()
	}
	q.mu.Lock()
	q.intelPhases = append(q.intelPhases, indexingperf.PhaseFromContext(ctx))
	q.intelBatchSizes = append(q.intelBatchSizes, len(rows))
	q.mu.Unlock()
	if q.intelSignal != nil {
		select {
		case q.intelSignal <- struct{}{}:
		default:
		}
	}
	return nil
}

func (q *recordingWritebackQueue) FlushAndWait(context.Context) error { return nil }

func TestCodeWritebackBatcherFlushesPendingKindsOnClose(t *testing.T) {
	t.Parallel()

	queue := &recordingWritebackQueue{
		cfg: CodeWritebackBatchConfig{
			QueueCapacity: 8,
			ItemEmbed:     WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			CodeChunks:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			IntelEmbed:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
		},
	}
	ctx := indexingperf.WithPhase(context.Background(), "embed_code")
	batcher := newCodeWritebackBatcher(ctx, queue, queue.cfg)

	require.NoError(t, batcher.SubmitCodeChunks(ctx, "a1", []codeidx.ChunkInput{{Index: 0, Hash: "h1"}}, []string{"body"}, []embeddings.Embedding{{1}}))
	require.NoError(t, batcher.SubmitCodeItemEmbedding(ctx, "a1", "h1", embeddings.Embedding{1, 2}))
	require.NoError(t, batcher.SubmitIntelEmbeddings(ctx, map[string]embeddings.Embedding{"chunk-1": {3, 4}}))
	require.NoError(t, batcher.Close())

	queue.mu.Lock()
	defer queue.mu.Unlock()
	require.Equal(t, []int{1}, queue.chunkRowTotals)
	require.Equal(t, []int{1}, queue.itemBatchSizes)
	require.Equal(t, []int{1}, queue.intelBatchSizes)
}

func TestCodeWritebackBatcherDrainsQueuedRowsAfterCancellation(t *testing.T) {
	t.Parallel()

	queue := &recordingWritebackQueue{
		cfg: CodeWritebackBatchConfig{
			QueueCapacity: 8,
			ItemEmbed:     WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			CodeChunks:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			IntelEmbed:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
		},
		rejectCanceled: true,
	}
	parent, cancel := context.WithCancel(indexingperf.WithPhase(context.Background(), "embed_code"))
	batcher := newCodeWritebackBatcher(parent, queue, queue.cfg)

	require.NoError(t, batcher.SubmitCodeChunks(parent, "a1", []codeidx.ChunkInput{{Index: 0, Hash: "h1"}}, []string{"body"}, []embeddings.Embedding{{1}}))
	require.NoError(t, batcher.SubmitCodeItemEmbedding(parent, "a1", "h1", embeddings.Embedding{1, 2}))
	require.NoError(t, batcher.SubmitIntelEmbeddings(parent, map[string]embeddings.Embedding{"chunk-1": {3, 4}}))
	cancel()

	require.NoError(t, batcher.Close())

	queue.mu.Lock()
	defer queue.mu.Unlock()
	require.Equal(t, []int{1}, queue.chunkRowTotals)
	require.Equal(t, []int{1}, queue.itemBatchSizes)
	require.Equal(t, []int{1}, queue.intelBatchSizes)
}

func TestCodeWritebackBatcherFlushesOnThresholdBeforeClose(t *testing.T) {
	t.Parallel()

	queue := &recordingWritebackQueue{
		cfg: CodeWritebackBatchConfig{
			QueueCapacity: 8,
			ItemEmbed:     WritebackFlushPolicy{Rows: 2, Bytes: 1 << 20, Idle: time.Hour},
			CodeChunks:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			IntelEmbed:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
		},
		itemSignal: make(chan struct{}, 1),
	}
	ctx := indexingperf.WithPhase(context.Background(), "embed_code")
	batcher := newCodeWritebackBatcher(ctx, queue, queue.cfg)
	defer func() { _ = batcher.Close() }()

	require.NoError(t, batcher.SubmitCodeItemEmbedding(ctx, "a1", "h1", embeddings.Embedding{1}))
	require.NoError(t, batcher.SubmitCodeItemEmbedding(ctx, "a2", "h2", embeddings.Embedding{2}))

	select {
	case <-queue.itemSignal:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected threshold flush before close")
	}
}

func TestCodeWritebackBatcherPreservesPhaseBoundaries(t *testing.T) {
	t.Parallel()

	queue := &recordingWritebackQueue{
		cfg: CodeWritebackBatchConfig{
			QueueCapacity: 8,
			ItemEmbed:     WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			CodeChunks:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
			IntelEmbed:    WritebackFlushPolicy{Rows: 8, Bytes: 1 << 20, Idle: time.Hour},
		},
	}
	batcher := newCodeWritebackBatcher(context.Background(), queue, queue.cfg)

	ctxA := indexingperf.WithPhase(context.Background(), "embed_code")
	ctxB := indexingperf.WithPhase(context.Background(), "embed_notes")
	require.NoError(t, batcher.SubmitCodeItemEmbedding(ctxA, "a1", "h1", embeddings.Embedding{1}))
	require.NoError(t, batcher.SubmitCodeItemEmbedding(ctxB, "a2", "h2", embeddings.Embedding{2}))
	require.NoError(t, batcher.Close())

	queue.mu.Lock()
	defer queue.mu.Unlock()
	require.ElementsMatch(t, []string{"embed_code", "embed_notes"}, queue.itemPhases)
	require.Len(t, queue.itemBatchSizes, 2)
}

func TestWritebackPressureControllerUsesHysteresisAndFloors(t *testing.T) {
	t.Parallel()

	controller := newWritebackPressureController(
		WritebackFlushPolicy{Rows: 512, Bytes: 1 << 20},
		writebackRowsFloor(512, 128),
		writebackBytesFloor(1<<20),
	)

	normal := controller.effectivePolicy(0, 8)
	require.Equal(t, 512, normal.Rows)
	require.Equal(t, 1<<20, normal.Bytes)

	high := controller.effectivePolicy(6, 8)
	require.Equal(t, 256, high.Rows)
	require.Equal(t, 512<<10, high.Bytes)

	stillHigh := controller.effectivePolicy(5, 8)
	require.Equal(t, 256, stillHigh.Rows)

	critical := controller.effectivePolicy(8, 8)
	require.Equal(t, 256, critical.Rows)
	require.Equal(t, 512<<10, critical.Bytes)

	backToHigh := controller.effectivePolicy(5, 8)
	require.Equal(t, 256, backToHigh.Rows)

	backToNormal := controller.effectivePolicy(4, 8)
	require.Equal(t, 512, backToNormal.Rows)

	for _, tc := range []struct {
		name                         string
		base                         WritebackFlushPolicy
		rowFloor, byteFloor, backlog int
		wantRows, wantBytes          int
	}{
		{"item critical", WritebackFlushPolicy{Rows: 256, Bytes: 1 << 20}, 128, 512 << 10, 8, 128, 512 << 10},
		{"intel critical", WritebackFlushPolicy{Rows: 256, Bytes: 1 << 20}, 256, 512 << 10, 8, 256, 512 << 10},
		{"chunk high", WritebackFlushPolicy{Rows: 512, Bytes: 2 << 20}, 128, 1 << 20, 6, 256, 1 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			floorRows := writebackRowsFloor(tc.base.Rows, tc.rowFloor)
			floorBytes := writebackBytesFloor(tc.base.Bytes)
			policy := newWritebackPressureController(tc.base, floorRows, floorBytes).effectivePolicy(tc.backlog, 8)
			require.Equal(t, tc.wantRows, policy.Rows)
			require.Equal(t, tc.wantBytes, policy.Bytes)
		})
	}
	pressure := newWritebackPressureController(WritebackFlushPolicy{Rows: 512, Bytes: 2 << 20}, writebackRowsFloor(512, 128), writebackBytesFloor(2<<20)).effectivePolicy(8, 8)
	for _, tc := range []struct {
		rows  int
		flush bool
	}{{255, false}, {256, true}} {
		batch := &writebackBatch[[]codeidx.ItemEmbeddingUpsert]{rows: tc.rows, bytes: 600 << 10}
		require.Equal(t, tc.flush, batch.shouldFlush(pressure), "rows=%d", tc.rows)
	}
}
