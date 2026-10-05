package indexwriter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
	"github.com/stretchr/testify/require"
)

func TestIntentSnapshotUsesQueueBarrierAndOwnsVectors(t *testing.T) {
	ctx := indexingperf.WithCollector(context.Background(), indexingperf.New())
	var got intentstore.Snapshot
	var phase string
	cfg := DefaultConfig()
	cfg.DefaultPolicy.Idle = time.Hour
	q := NewWithConfig(ctx, Handlers{ApplyIntentEmbeddingSnapshot: func(ctx context.Context, snapshot intentstore.Snapshot) error {
		got = snapshot
		phase = indexingperf.PhaseFromContext(ctx)
		return nil
	}}, cfg)
	t.Cleanup(func() { _ = q.Close() })
	snapshot := intentstore.Snapshot{ProviderFingerprint: "voyage-model", Rows: []intentstore.EmbeddingRecord{{Intent: "find", Exemplar: "find symbol", Dimensions: 2, Embedding: embeddings.Embedding{1, 2}}}}
	require.NoError(t, q.SubmitIntentEmbeddingSnapshot(indexingperf.WithPhase(ctx, "sync_intent_embeddings"), snapshot))
	require.Eventually(t, func() bool {
		return strings.Contains(indexingperf.FromContext(ctx).RenderWindow(time.Second), "intent_pending_rows=1")
	}, time.Second, time.Millisecond)
	snapshot.Rows[0].Embedding[0] = 99
	require.NoError(t, q.FlushAndWait(ctx))
	require.Equal(t, "voyage-model", got.ProviderFingerprint)
	require.Equal(t, float32(1), got.Rows[0].Embedding[0])
	require.Equal(t, "sync_intent_embeddings", phase)
	summary := indexingperf.FromContext(ctx).RenderSummary()
	require.Contains(t, summary, "intent_pending_rows_peak=1")
	require.NotContains(t, summary, "intent_pending_rows=1")
}

func TestIntentSnapshotQueuePropagatesWriteFailure(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("intent snapshot write failed")
	q := New(ctx, Handlers{ApplyIntentEmbeddingSnapshot: func(context.Context, intentstore.Snapshot) error { return failure }})
	t.Cleanup(func() { _ = q.Close() })
	require.NoError(t, q.SubmitIntentEmbeddingSnapshot(ctx, intentstore.Snapshot{ProviderFingerprint: "empty snapshot"}))
	require.ErrorIs(t, q.FlushAndWait(ctx), failure)
}
