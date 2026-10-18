package indexwriter

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func (q *Writer) beginSubmitAttempt() uint64 {
	if q == nil {
		return 0
	}
	return q.submitSeq.Add(1)
}

func (q *Writer) currentSubmitBarrier() uint64 {
	if q == nil {
		return 0
	}
	return q.submitSeq.Load()
}

func (q *Writer) settleSubmitAttempt(seq uint64) {
	if q == nil || seq == 0 {
		return
	}
	q.settleMu.Lock()
	defer q.settleMu.Unlock()
	if seq <= q.settledThrough {
		return
	}
	if seq == q.settledThrough+1 {
		q.settledThrough = seq
		for {
			next := q.settledThrough + 1
			if _, ok := q.settledPending[next]; !ok {
				return
			}
			delete(q.settledPending, next)
			q.settledThrough = next
		}
	}
	if q.settledPending == nil {
		q.settledPending = make(map[uint64]struct{})
	}
	q.settledPending[seq] = struct{}{}
}

func (q *Writer) barrierSettled(barrier uint64) bool {
	if q == nil {
		return true
	}
	q.settleMu.Lock()
	defer q.settleMu.Unlock()
	return q.settledThrough >= barrier
}

func (q *Writer) CodeWritebackBatchConfig() embeddings.CodeWritebackBatchConfig {
	if q == nil {
		return embeddings.CodeWritebackBatchConfig{}
	}
	return embeddings.CodeWritebackBatchConfig{
		QueueCapacity: cap(q.otherCmdCh),
		ItemEmbed: embeddings.WritebackFlushPolicy{
			Rows:  q.cfg.ItemEmbed.Rows,
			Bytes: q.cfg.ItemEmbed.Bytes,
			Idle:  q.cfg.ItemEmbed.Idle,
		},
		CodeChunks: embeddings.WritebackFlushPolicy{
			Rows:  q.cfg.DefaultPolicy.Rows,
			Bytes: q.cfg.DefaultPolicy.Bytes,
			Idle:  q.cfg.DefaultPolicy.Idle,
		},
		IntelEmbed: embeddings.WritebackFlushPolicy{
			Rows:  q.cfg.IntelEmbed.Rows,
			Bytes: q.cfg.IntelEmbed.Bytes,
			Idle:  q.cfg.IntelEmbed.Idle,
		},
	}
}

func observeNoteMetaQueueMetrics(ctx context.Context, batch *queueBatch[embeddings.NoteFileInfo]) {
	if batch == nil {
		return
	}
	indexingperf.SetGauge(ctx, indexingperf.NoteplanWritebackMetaQueueDepth, int64(batch.rows))
}

func observeNoteChunkSyncQueueMetrics(ctx context.Context, batch *queueBatch[embeddings.NoteChunkSync]) {
	if batch == nil {
		return
	}
	indexingperf.SetGauge(ctx, indexingperf.NoteembedWritebackChunkQueueDepth, int64(batch.rows))
}

func (q *Writer) SubmitCodeIndexWork(ctx context.Context, work codeanchor.CodeIndexWork) error {
	if err := q.Err(); err != nil {
		return err
	}
	return q.sendPayload(ctx, queuedPayload{
		value: work,
		bytes: estimateCodeIndexWork(work),
		phase: indexingperf.PhaseFromContext(ctx),
	}, true)
}

func (q *Writer) SetAfterCodeIndexBatch(fn func(context.Context, []codeanchor.CodeIndexWork) error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.afterCodeIndexBatch = fn
}

func (q *Writer) SubmitNoteIndexWork(ctx context.Context, work codeanchor.NoteIndexWork) error {
	return q.submit(ctx, work, estimateNoteIndexWork(work))
}

func (q *Writer) SubmitNoteMeta(ctx context.Context, info embeddings.NoteFileInfo) error {
	return q.submit(ctx, info, len(info.Path)+len(info.Title)+64)
}

func (q *Writer) SubmitCodeItemEmbedding(ctx context.Context, item codeindex.ItemEmbeddingUpsert) error {
	return q.submit(ctx, item, len(item.Hash)+len(item.Embedding)*4+64)
}

func (q *Writer) SubmitCodeItemEmbeddingBatch(ctx context.Context, items []codeindex.ItemEmbeddingUpsert) error {
	if len(items) == 0 {
		return nil
	}
	return q.submit(ctx, codeItemEmbeddingBatchWrite{items: append([]codeindex.ItemEmbeddingUpsert(nil), items...)}, estimateCodeItemEmbeddingBatch(items))
}

func (q *Writer) SubmitCodeItemChunks(ctx context.Context, anchorID codeindex.AnchorID, chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) error {
	return q.submit(ctx, codeindex.ItemChunksUpsert{AnchorID: anchorID, Chunks: chunks, Texts: texts, Embeddings: vecs}, estimateCodeChunkWrite(chunks, texts, vecs))
}

func (q *Writer) SubmitCodeItemChunkBatch(ctx context.Context, items []codeindex.ItemChunksUpsert) error {
	if len(items) == 0 {
		return nil
	}
	return q.submit(ctx, codeChunkBatchWrite{items: append([]codeindex.ItemChunksUpsert(nil), items...)}, estimateCodeChunkBatch(items))
}

func (q *Writer) SubmitNoteChunkSync(ctx context.Context, item embeddings.NoteChunkSync) error {
	return q.submit(ctx, noteChunkSyncWrite{item: cloneNoteChunkSync(item)}, estimateNoteChunkSyncWrite(item))
}

func (q *Writer) SubmitIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	return q.submit(ctx, intelChunkWrite{ownerIDs: ownerIDs, chunks: chunks}, len(ownerIDs)*48+len(chunks)*160)
}

func (q *Writer) SubmitIntelChunksByFamily(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error {
	return q.submit(ctx, intelChunkWrite{ownerIDs: ownerIDs, family: family, chunks: chunks}, len(ownerIDs)*48+len(chunks)*160)
}

func (q *Writer) SubmitIntelEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	// Intel embeddings include ontology body chunks. They use a larger
	// batch policy because provider output usually arrives in big waves.
	return q.submit(ctx, intelEmbeddingWrite{rows: rows}, estimateIntelEmbeddings(rows))
}

func (q *Writer) SubmitOntologyNodeReadModel(ctx context.Context, model readmodel.NodeReadModel) error {
	indexingperf.AddCount(ctx, indexingperf.OntologyFieldRowsPlanned, int64(len(model.FieldValues)))
	return q.submit(ctx, ontologyNodeWrite{
		model: readmodel.NodeReadModel{
			FullReplace:      model.FullReplace,
			NotePaths:        append([]string(nil), model.NotePaths...),
			Nodes:            append([]readmodel.NodeRow(nil), model.Nodes...),
			FieldValues:      append([]readmodel.FieldValueRow(nil), model.FieldValues...),
			LinkDependencies: append([]readmodel.LinkDependencyRow(nil), model.LinkDependencies...),
		},
	}, len(model.NotePaths)*48+len(model.Nodes)*256+len(model.FieldValues)*192)
}

func (q *Writer) SubmitOntologyNodes(ctx context.Context, notePaths []string, nodes []readmodel.NodeRow) error {
	return q.SubmitOntologyNodeReadModel(ctx, readmodel.NodeReadModel{NotePaths: notePaths, Nodes: nodes})
}

func (q *Writer) SubmitOntologyNodeEmbeddingStates(ctx context.Context, states []codeanchor.IntelOntologyNodeEmbeddingState) error {
	return q.submit(ctx, ontologyNodeStateWrite{states: append([]codeanchor.IntelOntologyNodeEmbeddingState(nil), states...)}, len(states)*192)
}

func (q *Writer) SubmitOntologyDelta(ctx context.Context, delta semdb.OntologyDelta) error {
	for _, model := range delta.ReadModels {
		indexingperf.AddCount(ctx, indexingperf.OntologyFieldRowsPlanned, int64(len(model.FieldValues)))
	}
	return q.submit(ctx, ontologyDeltaWrite{delta: delta}, estimateOntologyDelta(delta))
}

func (q *Writer) SubmitNoteMetadataDelta(ctx context.Context, delta semdb.NoteMetadataDelta) error {
	started := time.Now()
	err := q.submit(ctx, noteMetadataDeltaWrite{delta: delta}, estimateNoteMetadataDelta(delta))
	indexingperf.ObserveLatency(ctx, indexingperf.ValidationWritebackMetadataSubmitWait, time.Since(started))
	return err
}

func (q *Writer) SubmitValidationSnapshot(ctx context.Context, snapshot semdb.ValidationSnapshot, errorMessage string, durationMs int64) error {
	return q.submit(ctx, ValidationStateWrite{
		Snapshot:     snapshot,
		ErrorMessage: errorMessage,
		DurationMs:   durationMs,
	}, len(snapshot.Diagnostics)*256+len(snapshot.Actions)*192+len(errorMessage)+64)
}

// SubmitOwnershipTransitions durably switches the ownership of a complete
// discovery snapshot after all previously submitted payloads have settled.
// This is intentionally a synchronous control operation rather than a normal
// batch: callers need its committed result to schedule affected work.
func (q *Writer) SubmitOwnershipTransitions(ctx context.Context, transitions []semdb.OwnershipTransition) (result semdb.OwnershipTransitionResult, err error) {
	done := indexingperf.StartSpan(ctx, "ownership_transition_barrier")
	defer func() { done(err) }()
	if err := ctx.Err(); err != nil {
		return semdb.OwnershipTransitionResult{}, err
	}
	if err := q.Err(); err != nil {
		return semdb.OwnershipTransitionResult{}, err
	}
	cloned := CloneOwnershipTransitions(transitions)
	return submitControl(ctx, q, func(q *Writer) (semdb.OwnershipTransitionResult, error) {
		if q.handlers.ApplyOwnershipTransitions == nil {
			return semdb.OwnershipTransitionResult{}, errors.New("ownership transition queue not initialized")
		}
		return q.handlers.ApplyOwnershipTransitions(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.apply_ownership_transition"), cloned)
	}, false)
}

// AcknowledgeOwnershipReconciliation records successful convergence for
// exactly one durable ownership generation. A false store acknowledgement is
// treated as an error so a stale run cannot silently claim convergence.
func (q *Writer) AcknowledgeOwnershipReconciliation(ctx context.Context, generation int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := q.Err(); err != nil {
		return err
	}
	return submitErrorControl(ctx, q, func(q *Writer) error {
		if q.handlers.AcknowledgeOwnershipReconciliation == nil {
			return errors.New("ownership reconciliation queue not initialized")
		}
		acknowledged, err := q.handlers.AcknowledgeOwnershipReconciliation(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.acknowledge_ownership_reconciliation"), generation)
		if err != nil {
			return err
		}
		if !acknowledged {
			return fmt.Errorf("ownership reconciliation generation %d was not acknowledged", generation)
		}
		recordReconciliationAcknowledged(q.ctx, generation)
		return nil
	})
}

func (q *Writer) FlushAndWait(ctx context.Context) (err error) {
	done := indexingperf.StartSpan(ctx, "writer_flush_barrier")
	defer func() { done(err) }()
	indexingperf.AddCount(ctx, indexingperf.WriterFlushBarriers, 1)
	return submitErrorControl(ctx, q, func(*Writer) error { return nil })
}

func (q *Writer) Close() error {
	q.closeOnce.Do(func() { q.closeErr = q.closeAndWait() })
	return q.closeErr
}

func (q *Writer) closeAndWait() (err error) {
	done := indexingperf.StartSpan(q.ctx, "writer_close_barrier")
	defer func() { done(err) }()
	indexingperf.AddCount(q.ctx, indexingperf.WriterCloseBarriers, 1)
	_, err = submitControl(q.ctx, q, func(*Writer) (struct{}, error) { return struct{}{}, nil }, true)
	<-q.doneCh
	return errors.Join(err, q.Err())
}

// StopAndWait cancels pending work and waits for any in-flight handler to
// return. Failure cleanup uses this path so the store cannot close while the
// writer still owns it.
func (q *Writer) StopAndWait() error {
	q.cancel()
	<-q.doneCh
	return q.Err()
}

func (q *Writer) Err() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

func (q *Writer) submit(ctx context.Context, cmd any, bytes int) error {
	if err := q.Err(); err != nil {
		return err
	}
	return q.sendPayload(ctx, queuedPayload{value: cmd, bytes: bytes, phase: indexingperf.PhaseFromContext(ctx)}, false)
}

func (q *Writer) sendControl(ctx context.Context, cmd any) error {
	if err := q.ctx.Err(); err != nil {
		if failed := q.Err(); failed != nil {
			return failed
		}
		return errors.New("index write queue stopped")
	}
	started := time.Now()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.ctx.Done():
		if err := q.Err(); err != nil {
			return err
		}
		return q.ctx.Err()
	case q.ctrlCh <- cmd:
		indexingperf.ObserveLatency(ctx, indexingperf.QueueWait, time.Since(started))
		return nil
	}
}

func (q *Writer) sendPayload(ctx context.Context, cmd queuedPayload, codeLane bool) error {
	if err := q.ctx.Err(); err != nil {
		if failed := q.Err(); failed != nil {
			return failed
		}
		return errors.New("index write queue stopped")
	}
	seq := q.beginSubmitAttempt()
	defer q.settleSubmitAttempt(seq)
	started := time.Now()
	target := q.otherCmdCh
	waitMetric := indexQueueOtherWaitName
	if codeLane {
		target = q.codeCmdCh
		waitMetric = indexQueueCodeWaitName
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.ctx.Done():
		if err := q.Err(); err != nil {
			return err
		}
		return q.ctx.Err()
	case target <- cmd:
		waitDur := time.Since(started)
		indexingperf.ObserveLatency(ctx, indexingperf.QueueWait, waitDur)
		indexingperf.ObserveLatency(ctx, waitMetric, waitDur)
		q.snapshotQueueDepths(ctx)
		indexingperf.AddCount(ctx, indexingperf.NodeWriteIn, 1)
		if cmd.bytes > 0 {
			indexingperf.AddBytes(ctx, indexingperf.QueueBytesBytes, int64(cmd.bytes))
		}
		return nil
	}
}

func (q *Writer) snapshotQueueDepths(ctx context.Context) {
	codeDepth := len(q.codeCmdCh)
	otherDepth := len(q.otherCmdCh)
	indexingperf.SetGauge(ctx, indexQueueSnapshotName, int64(codeDepth+otherDepth))
	indexingperf.SetGauge(ctx, indexQueueCodeDepthName, int64(codeDepth))
	indexingperf.SetGauge(ctx, indexQueueOtherDepthName, int64(otherDepth))
}
