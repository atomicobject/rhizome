package semantic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
)

func TestCodeWritebackBatcherFlushesIdleLanesBeforeClose(t *testing.T) {
	policy := WritebackFlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: 10 * time.Millisecond}
	queue := &recordingWritebackQueue{}
	batcher := newCodeWritebackBatcher(context.Background(), queue, CodeWritebackBatchConfig{
		QueueCapacity: 8, ItemEmbed: policy, CodeChunks: policy, IntelEmbed: policy,
	})
	t.Cleanup(func() { require.NoError(t, batcher.Close()) })
	ctx := indexingperf.WithPhase(context.Background(), "embed_code")
	require.NoError(t, batcher.SubmitCodeItemEmbedding(ctx, "a", "hash", embeddings.Embedding{1}))
	require.NoError(t, batcher.SubmitCodeChunks(ctx, "a", []codeindex.ChunkInput{{Hash: "hash"}}, []string{"text"}, []embeddings.Embedding{{1}}))
	require.NoError(t, batcher.SubmitIntelEmbeddings(ctx, map[string]embeddings.Embedding{"hash": {1}}))

	require.Eventually(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		return len(queue.itemBatchSizes) == 1 && len(queue.chunkRowTotals) == 1 && len(queue.intelBatchSizes) == 1
	}, time.Second, time.Millisecond)
	queue.mu.Lock()
	defer queue.mu.Unlock()
	require.Equal(t, []string{"embed_code"}, queue.itemPhases)
	require.Equal(t, []string{"embed_code"}, queue.chunkPhases)
	require.Equal(t, []string{"embed_code"}, queue.intelPhases)
}

type rejectingWritebackQueue struct {
	recordingWritebackQueue
	err    error
	failed chan struct{}
}

func (q *rejectingWritebackQueue) SubmitCodeItemEmbeddingBatch(context.Context, []codeindex.ItemEmbeddingUpsert) error {
	err := q.err
	q.failed <- struct{}{}
	return err
}

func (q *rejectingWritebackQueue) SubmitCodeItemChunkBatch(context.Context, []codeindex.ItemChunksUpsert) error {
	err := q.err
	q.failed <- struct{}{}
	return err
}

func (q *rejectingWritebackQueue) SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error {
	err := q.err
	q.failed <- struct{}{}
	return err
}

func TestCodeWritebackBatcherRetainsFirstSubmissionError(t *testing.T) {
	for _, kind := range []string{"item", "chunk", "intel"} {
		t.Run(kind, func(t *testing.T) {
			first := errors.New("first write failed")
			queue := &rejectingWritebackQueue{err: first, failed: make(chan struct{}, 1)}
			policy := WritebackFlushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour}
			batcher := newCodeWritebackBatcher(context.Background(), queue, CodeWritebackBatchConfig{
				QueueCapacity: 8, ItemEmbed: policy, CodeChunks: policy, IntelEmbed: policy,
			})
			ctx := context.Background()
			var submit func() error
			var label string
			switch kind {
			case "item":
				submit = func() error { return batcher.SubmitCodeItemEmbedding(ctx, "a", "hash", embeddings.Embedding{1}) }
				label = "submit code item embedding batch items=1 rows=1"
			case "chunk":
				submit = func() error {
					return batcher.SubmitCodeChunks(ctx, "a", []codeindex.ChunkInput{{Hash: "hash"}}, []string{"text"}, []embeddings.Embedding{{1}})
				}
				label = "submit code chunk batch items=1 rows=1"
			case "intel":
				submit = func() error { return batcher.SubmitIntelEmbeddings(ctx, map[string]embeddings.Embedding{"hash": {1}}) }
				label = "submit intel embedding batch rows=1"
			}
			require.NoError(t, submit())
			select {
			case <-queue.failed:
			case <-time.After(time.Second):
				t.Fatal("writeback did not stop after submission failure")
			}
			var err error
			require.Eventually(t, func() bool { err = submit(); return errors.Is(err, first) }, time.Second, time.Millisecond)
			require.ErrorIs(t, err, first)
			require.ErrorContains(t, err, label)
			require.ErrorIs(t, batcher.Close(), first)
		})
	}
}

type capturingIntelWritebackQueue struct {
	recordingWritebackQueue
	rows chan map[string]embeddings.Embedding
}

func (q *capturingIntelWritebackQueue) SubmitIntelEmbeddings(_ context.Context, rows map[string]embeddings.Embedding) error {
	q.rows <- rows
	return nil
}

func TestCodeWritebackBatcherIntelDuplicatesKeepLatestOwnedVectorAndCountAllSubmissions(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_code")
	queue := &capturingIntelWritebackQueue{rows: make(chan map[string]embeddings.Embedding, 1)}
	batcher := newCodeWritebackBatcher(ctx, queue, CodeWritebackBatchConfig{
		QueueCapacity: 8,
		IntelEmbed:    WritebackFlushPolicy{Rows: 2, Bytes: 1 << 20, Idle: time.Hour},
	})
	first := map[string]embeddings.Embedding{"hash": {1, 2}, "empty": nil}
	require.NoError(t, batcher.SubmitIntelEmbeddings(ctx, first))
	first["hash"][0] = 99
	first["extra"] = embeddings.Embedding{99}
	last := map[string]embeddings.Embedding{"hash": {3, 4}}
	require.NoError(t, batcher.SubmitIntelEmbeddings(ctx, last))
	last["hash"][0] = 99
	delete(last, "hash")

	select {
	case rows := <-queue.rows:
		require.Equal(t, map[string]embeddings.Embedding{"hash": {3, 4}}, rows)
	case <-time.After(time.Second):
		t.Fatal("duplicate keys must count toward the flush threshold")
	}
	require.NoError(t, batcher.Close())
	collector.RecordSpan("embed_code", time.Millisecond, nil)
	summary := collector.RenderSummary()
	require.Contains(t, summary, "writeback_intel_batch_rows_p50=2")
	require.Regexp(t, `writeback_intel_batch_bytes_p50=[1-9][0-9]*B`, summary)
}
