package indexwriter

import (
	"context"
	"fmt"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
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
	if q.handlers.ApplyCodeItemChunkBatch != nil {
		return q.flush(ctx, rows, bytes, func(ctx context.Context) error {
			return q.handlers.ApplyCodeItemChunkBatch(ctx, jobs)
		})
	}
	for _, job := range jobs {
		jobCopy := job
		if err := q.flush(ctx, len(jobCopy.Chunks), estimateCodeChunkWrite(jobCopy.Chunks, jobCopy.Texts, jobCopy.Embeddings), func(ctx context.Context) error {
			return q.handlers.ApplyCodeItemChunks(ctx, jobCopy.AnchorID, jobCopy.Chunks, jobCopy.Texts, jobCopy.Embeddings)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (q *Writer) flushNoteMetaBatch(ctx context.Context, batch *queueBatch[embeddings.NoteFileInfo]) error {
	if batch == nil || len(batch.items) == 0 {
		return nil
	}
	indexingperf.AddCount(ctx, "noteplan.writeback.meta.flush.count", 1)
	indexingperf.ObserveSample(ctx, "noteplan.writeback.meta.batch_rows", int64(batch.rows))
	indexingperf.ObserveSample(ctx, "noteplan.writeback.meta.batch_bytes", int64(batch.bytes))
	started := time.Now()
	err := q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error {
		return q.handlers.ApplyNoteMetaBatch(ctx, batch.items)
	})
	indexingperf.ObserveLatency(ctx, "noteplan.writeback.meta.flush", time.Since(started))
	indexingperf.SetGauge(ctx, "noteplan.writeback.meta.queue_depth", 0)
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

func (q *Writer) flushNoteChunkBatch(ctx context.Context, jobs []noteChunkWrite) error {
	if len(jobs) == 0 {
		return nil
	}
	rows := 0
	bytes := 0
	for _, job := range jobs {
		rows += len(job.chunks)
		bytes += estimateNoteChunkWrite(job.chunks, job.vecs)
	}
	indexingperf.AddCount(ctx, "noteembed.writeback.chunk.flush.count", 1)
	indexingperf.ObserveSample(ctx, "noteembed.writeback.chunk.batch_rows", int64(rows))
	indexingperf.ObserveSample(ctx, "noteembed.writeback.chunk.batch_bytes", int64(bytes))
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "noteembed.writeback.chunk.flush", time.Since(started))
		indexingperf.SetGauge(ctx, "noteembed.writeback.chunk.queue_depth", 0)
	}()
	if q.handlers.ApplyNoteChunkBatch != nil {
		items := make([]embeddings.NoteChunksUpsert, 0, len(jobs))
		for _, job := range jobs {
			items = append(items, embeddings.NoteChunksUpsert{
				NoteID:     job.noteID,
				Chunks:     job.chunks,
				Embeddings: job.vecs,
			})
		}
		return q.flush(ctx, rows, bytes, func(ctx context.Context) error {
			return q.handlers.ApplyNoteChunkBatch(ctx, items)
		})
	}
	for _, job := range jobs {
		jobCopy := job
		if err := q.flush(ctx, len(jobCopy.chunks), estimateNoteChunkWrite(jobCopy.chunks, jobCopy.vecs), func(ctx context.Context) error {
			return q.handlers.ApplyNoteChunks(ctx, jobCopy.noteID, jobCopy.chunks, jobCopy.vecs)
		}); err != nil {
			return err
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
	indexingperf.AddCount(ctx, "noteembed.writeback.chunk.flush.count", 1)
	indexingperf.ObserveSample(ctx, "noteembed.writeback.chunk.batch_rows", int64(rows))
	indexingperf.ObserveSample(ctx, "noteembed.writeback.chunk.batch_bytes", int64(bytes))
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "noteembed.writeback.chunk.flush", time.Since(started))
		indexingperf.SetGauge(ctx, "noteembed.writeback.chunk.queue_depth", 0)
	}()
	if q.handlers.ApplyNoteChunkSyncBatch != nil {
		return q.flush(ctx, rows, bytes, func(ctx context.Context) error {
			return q.handlers.ApplyNoteChunkSyncBatch(ctx, items)
		})
	}
	for _, item := range items {
		itemCopy := cloneNoteChunkSync(item)
		if err := q.flush(ctx, noteChunkSyncRows(itemCopy), estimateNoteChunkSyncWrite(itemCopy), func(ctx context.Context) error {
			return q.handlers.ApplyNoteChunkSync(ctx, itemCopy)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (q *Writer) applyCodeIndexBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	applyStarted := time.Now()
	if err := q.handlers.ApplyCodeIndexBatch(ctx, batch); err != nil {
		return err
	}
	indexingperf.ObserveLatency(ctx, "codepersist.writer_apply", time.Since(applyStarted))
	q.mu.Lock()
	fn := q.afterCodeIndexBatch
	q.mu.Unlock()
	if fn != nil {
		afterStarted := time.Now()
		err := fn(ctx, batch)
		indexingperf.ObserveLatency(ctx, "codepersist.writer_after", time.Since(afterStarted))
		return err
	}
	return nil
}

func (q *Writer) applyNoteIndexBatch(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
	if err := q.handlers.ApplyNoteIndexBatch(ctx, batch); err != nil {
		return err
	}
	q.mu.Lock()
	fn := q.afterNoteIndexBatch
	q.mu.Unlock()
	if fn != nil {
		return fn(ctx, batch)
	}
	return nil
}

func (q *Writer) flush(ctx context.Context, rows, bytes int, apply func(context.Context) error) error {
	if apply == nil || rows == 0 {
		return nil
	}
	indexingperf.AddCount(ctx, "queue.flush", 1)
	indexingperf.AddCount(ctx, "queue.rows", int64(rows))
	indexingperf.AddBytes(ctx, "queue.flush", int64(bytes))
	started := time.Now()
	err := apply(ctx)
	flushDur := time.Since(started)
	indexingperf.ObserveLatency(ctx, "queue.flush_latency", flushDur)
	indexingperf.ObserveLatency(ctx, "node.write.busy", flushDur)
	indexingperf.ObserveInterval(ctx, "node.write.busy", started, flushDur)
	indexingperf.AddCount(ctx, "queue.flush.outcome."+metricOutcome(err), 1)
	if err == nil {
		indexingperf.AddCount(ctx, "node.write.out", int64(rows))
		indexingperf.AddCount(ctx, "queue.rows.committed", int64(rows))
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
		if family != "" && q.handlers.ApplyIntelChunksByFamily != nil {
			return q.handlers.ApplyIntelChunksByFamily(ctx, owners, family, chunks)
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
