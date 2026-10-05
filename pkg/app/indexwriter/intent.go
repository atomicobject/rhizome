package indexwriter

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

const intentSnapshotPendingRows = "intent.writeback.pending_rows"

func (q *Writer) SubmitIntentEmbeddingSnapshot(ctx context.Context, snapshot intentstore.Snapshot) error {
	cloned := intentstore.Snapshot{ProviderFingerprint: snapshot.ProviderFingerprint, Rows: append([]intentstore.EmbeddingRecord(nil), snapshot.Rows...)}
	size := len(snapshot.ProviderFingerprint)
	for i := range cloned.Rows {
		row := &cloned.Rows[i]
		row.Embedding = append(embeddings.Embedding(nil), row.Embedding...)
		size += len(row.Intent) + len(row.Exemplar) + len(row.Embedding)*4 + 32
	}
	return q.submit(ctx, cloned, size)
}

func (q *Writer) flushIntentSnapshots(ctx context.Context, batch *queueBatch[intentstore.Snapshot]) error {
	if batch == nil || len(batch.items) == 0 {
		return nil
	}
	defer indexingperf.SetGauge(ctx, intentSnapshotPendingRows, 0)
	if q.handlers.ApplyIntentEmbeddingSnapshot == nil {
		return fmt.Errorf("intent embedding snapshot queue not initialized")
	}
	return q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error {
		for _, snapshot := range batch.items {
			if err := q.handlers.ApplyIntentEmbeddingSnapshot(ctx, snapshot); err != nil {
				return err
			}
		}
		return nil
	})
}
