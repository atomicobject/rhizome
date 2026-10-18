package indexwriter

import (
	"context"
	"fmt"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (q *Writer) flushCodeChunkBatch(ctx context.Context, jobs []codeindex.ItemChunksUpsert) error {
	if len(jobs) == 0 {
		return nil
	}
	rows := 0
	bytes := 0
	for _, job := range jobs {
		rows += len(job.Chunks)
		bytes += estimateCodeChunkWrite(job.Chunks, job.Texts, job.Embeddings)
	}
	return q.flush(ctx, rows, bytes, func(ctx context.Context) error {
		return q.handlers.ApplyCodeItemChunkBatch(ctx, jobs)
	})
}

func (q *Writer) flushNoteMetaBatch(ctx context.Context, batch *queueBatch[embeddings.NoteFileInfo]) error {
	if batch == nil || len(batch.items) == 0 {
		return nil
	}
	indexingperf.AddCount(ctx, indexingperf.NoteplanWritebackMetaFlushCount, 1)
	indexingperf.ObserveSample(ctx, indexingperf.NoteplanWritebackMetaBatchRows, int64(batch.rows))
	indexingperf.ObserveSample(ctx, indexingperf.NoteplanWritebackMetaBatchBytes, int64(batch.bytes))
	started := time.Now()
	err := q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error {
		return q.handlers.ApplyNoteMetaBatch(ctx, batch.items)
	})
	indexingperf.ObserveLatency(ctx, indexingperf.NoteplanWritebackMetaFlush, time.Since(started))
	indexingperf.SetGauge(ctx, indexingperf.NoteplanWritebackMetaQueueDepth, 0)
	return err
}

func (q *Writer) flushNoteMetadataDeltas(ctx context.Context, batch *queueBatch[noteMetadataDeltaWrite]) error {
	if batch == nil || len(batch.items) == 0 {
		return nil
	}
	if q.handlers.ApplyNoteMetadataDelta == nil {
		return fmt.Errorf("note metadata delta queue not initialized")
	}
	indexingperf.AddCount(ctx, noteMetadataFlushCountName, 1)
	indexingperf.ObserveSample(ctx, noteMetadataBatchRowsName, int64(batch.rows))
	indexingperf.ObserveSample(ctx, noteMetadataBatchBytesName, int64(batch.bytes))
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, noteMetadataFlushLatencyName, time.Since(started))
		indexingperf.SetGauge(ctx, noteMetadataQueueDepthName, 0)
	}()
	for _, item := range batch.items {
		item := item
		if err := q.flush(ctx, noteMetadataDeltaRows(item.delta), estimateNoteMetadataDelta(item.delta), func(ctx context.Context) error {
			return q.handlers.ApplyNoteMetadataDelta(ctx, item.delta)
		}); err != nil {
			return fmt.Errorf("flush note metadata delta: %w", err)
		}
	}
	return nil
}

func (q *Writer) flushNoteChunkSyncBatch(ctx context.Context, items []embeddings.NoteChunkSync) error {
	if len(items) == 0 {
		return nil
	}
	rows := 0
	bytes := 0
	for _, item := range items {
		rows += noteChunkSyncRows(item)
		bytes += estimateNoteChunkSyncWrite(item)
	}
	indexingperf.AddCount(ctx, indexingperf.NoteembedWritebackChunkFlushCount, 1)
	indexingperf.ObserveSample(ctx, indexingperf.NoteembedWritebackChunkBatchRows, int64(rows))
	indexingperf.ObserveSample(ctx, indexingperf.NoteembedWritebackChunkBatchBytes, int64(bytes))
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, indexingperf.NoteembedWritebackChunkFlush, time.Since(started))
		indexingperf.SetGauge(ctx, indexingperf.NoteembedWritebackChunkQueueDepth, 0)
	}()
	return q.flush(ctx, rows, bytes, func(ctx context.Context) error {
		return q.handlers.ApplyNoteChunkSyncBatch(ctx, items)
	})
}

func (q *Writer) applyCodeIndexBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	applyStarted := time.Now()
	if err := q.handlers.ApplyCodeIndexBatch(ctx, batch); err != nil {
		return err
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodepersistWriterApply, time.Since(applyStarted))
	q.mu.Lock()
	fn := q.afterCodeIndexBatch
	q.mu.Unlock()
	if fn != nil {
		afterStarted := time.Now()
		err := fn(ctx, batch)
		indexingperf.ObserveLatency(ctx, indexingperf.CodepersistWriterAfter, time.Since(afterStarted))
		return err
	}
	return nil
}

func (q *Writer) applyNoteIndexBatch(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
	if err := q.handlers.ApplyNoteIndexBatch(ctx, batch); err != nil {
		return err
	}
	return nil
}

func (q *Writer) flush(ctx context.Context, rows, bytes int, apply func(context.Context) error) error {
	if apply == nil || rows == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	indexingperf.AddCount(ctx, indexingperf.QueueFlush, 1)
	indexingperf.AddCount(ctx, indexingperf.QueueRows, int64(rows))
	indexingperf.AddBytes(ctx, indexingperf.QueueFlushBytes, int64(bytes))
	started := time.Now()
	err := apply(ctx)
	flushDur := time.Since(started)
	indexingperf.ObserveLatency(ctx, indexingperf.QueueFlushLatency, flushDur)
	indexingperf.ObserveLatency(ctx, indexingperf.NodeWriteBusy, flushDur)
	indexingperf.ObserveInterval(ctx, indexingperf.NodeWriteBusy, started, flushDur)
	indexingperf.AddCount(ctx, indexingperf.QueueFlushOutcome.Select(metricOutcome(err)), 1)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.NodeWriteOut, int64(rows))
		indexingperf.AddCount(ctx, indexingperf.QueueRowsCommitted, int64(rows))
	}
	return err
}

func (q *Writer) flushIntelChunkBatch(ctx context.Context, batch *intelChunkBatch) error {
	if batch == nil || batch.rows == 0 {
		return nil
	}
	owners := append([]string(nil), batch.ownerIDs...)
	chunks := append([]codeanchor.IntelChunk(nil), batch.chunks...)
	family := batch.family
	return q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error {
		if family != "" {
			if q.handlers.ApplyIntelChunksByFamily == nil {
				return fmt.Errorf("intel chunk family queue not initialized")
			}
			return q.handlers.ApplyIntelChunksByFamily(ctx, owners, family, chunks)
		}
		if q.handlers.ApplyIntelChunks == nil {
			return fmt.Errorf("intel chunk queue not initialized")
		}
		return q.handlers.ApplyIntelChunks(ctx, owners, chunks)
	})
}

func (q *Writer) flushOntologyNodeWrites(ctx context.Context, batch *queueBatch[ontologyNodeWrite]) error {
	if batch == nil || batch.rows == 0 {
		return nil
	}
	writes := append([]ontologyNodeWrite(nil), batch.items...)
	return q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error {
		for _, write := range writes {
			if err := ctx.Err(); err != nil {
				return err
			}
			if q.handlers.ApplyOntologyNodeReadModel == nil {
				return fmt.Errorf("ontology node queue not initialized")
			}
			if err := q.handlers.ApplyOntologyNodeReadModel(ctx, write.model); err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *Writer) flushValidationState(ctx context.Context, batch *queueBatch[ValidationStateWrite]) error {
	if batch == nil || batch.rows == 0 {
		return nil
	}
	writes := append([]ValidationStateWrite(nil), batch.items...)
	return q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error {
		if q.handlers.ApplyValidationState == nil {
			return fmt.Errorf("validation state queue not initialized")
		}
		for _, write := range writes {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := q.handlers.ApplyValidationState(ctx, write); err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *Writer) fail(err error) {
	if err == nil {
		return
	}
	q.mu.Lock()
	if q.err == nil {
		q.err = err
	}
	q.mu.Unlock()
	q.cancel()
}
