package indexwriter

import (
	"context"
	"errors"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
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

func (q *Writer) CodeWritebackBatchConfig() semantic.CodeWritebackBatchConfig {
	if q == nil {
		return semantic.CodeWritebackBatchConfig{}
	}
	return semantic.CodeWritebackBatchConfig{
		QueueCapacity: cap(q.otherCmdCh),
		ItemEmbed: semantic.WritebackFlushPolicy{
			Rows:  q.cfg.ItemEmbed.Rows,
			Bytes: q.cfg.ItemEmbed.Bytes,
			Idle:  q.cfg.ItemEmbed.Idle,
		},
		CodeChunks: semantic.WritebackFlushPolicy{
			Rows:  q.cfg.DefaultPolicy.Rows,
			Bytes: q.cfg.DefaultPolicy.Bytes,
			Idle:  q.cfg.DefaultPolicy.Idle,
		},
		IntelEmbed: semantic.WritebackFlushPolicy{
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
	indexingperf.SetGauge(ctx, "noteplan.writeback.meta.queue_depth", int64(batch.rows))
}

func observeNoteChunkSyncQueueMetrics(ctx context.Context, batch *queueBatch[embeddings.NoteChunkSync]) {
	if batch == nil {
		return
	}
	indexingperf.SetGauge(ctx, "noteembed.writeback.chunk.queue_depth", int64(batch.rows))
}

func (q *Writer) SubmitCodeIndexWork(ctx context.Context, work codeanchor.CodeIndexWork) error {
	if q == nil {
		return nil
	}
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

func (q *Writer) SetAfterNoteIndexBatch(fn func(context.Context, []codeanchor.NoteIndexWork) error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.afterNoteIndexBatch = fn
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

func (q *Writer) SubmitNoteChunks(ctx context.Context, noteID embeddings.NoteID, chunks []embeddings.ChunkInput, vecs []embeddings.Embedding) error {
	return q.submit(ctx, noteChunkWrite{noteID: noteID, chunks: chunks, vecs: vecs}, estimateNoteChunkWrite(chunks, vecs))
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

func (q *Writer) SubmitOntologyNodeReadModel(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
	indexingperf.AddCount(ctx, "ontology.field_rows_planned", int64(len(model.FieldValues)))
	return q.submit(ctx, ontologyNodeWrite{
		model: codeanchor.IntelOntologyNodeReadModel{
			FullReplace:      model.FullReplace,
			NotePaths:        append([]string(nil), model.NotePaths...),
			Nodes:            append([]codeanchor.IntelOntologyNode(nil), model.Nodes...),
			FieldValues:      append([]codeanchor.IntelOntologyNodeFieldValue(nil), model.FieldValues...),
			LinkDependencies: append([]codeanchor.IntelOntologyNodeLinkDependency(nil), model.LinkDependencies...),
		},
	}, len(model.NotePaths)*48+len(model.Nodes)*256+len(model.FieldValues)*192)
}

func (q *Writer) SubmitOntologyNodes(ctx context.Context, notePaths []string, nodes []codeanchor.IntelOntologyNode) error {
	return q.SubmitOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{NotePaths: notePaths, Nodes: nodes})
}

func (q *Writer) SubmitOntologyNodeEmbeddingStates(ctx context.Context, states []codeanchor.IntelOntologyNodeEmbeddingState) error {
	return q.submit(ctx, ontologyNodeStateWrite{states: append([]codeanchor.IntelOntologyNodeEmbeddingState(nil), states...)}, len(states)*192)
}

func (q *Writer) SubmitOntologyDelta(ctx context.Context, delta semdb.OntologyDelta) error {
	for _, model := range delta.ReadModels {
		indexingperf.AddCount(ctx, "ontology.field_rows_planned", int64(len(model.FieldValues)))
	}
	return q.submit(ctx, ontologyDeltaWrite{delta: delta}, estimateOntologyDelta(delta))
}

func (q *Writer) SubmitNoteMetadataDelta(ctx context.Context, delta semdb.NoteMetadataDelta) error {
	started := time.Now()
	err := q.submit(ctx, noteMetadataDeltaWrite{delta: delta}, estimateNoteMetadataDelta(delta))
	indexingperf.ObserveLatency(ctx, "validation.writeback.metadata.submit_wait", time.Since(started))
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
	if q == nil {
		return semdb.OwnershipTransitionResult{}, nil
	}
	done := indexingperf.StartSpan(ctx, "ownership_transition_barrier")
	defer func() { done(err) }()
	if err := ctx.Err(); err != nil {
		return semdb.OwnershipTransitionResult{}, err
	}
	if err := q.Err(); err != nil {
		return semdb.OwnershipTransitionResult{}, err
	}
	ack := make(chan ownershipTransitionAck, 1)
	cmd := queueOwnershipTransitions{
		barrier:     q.currentSubmitBarrier(),
		transitions: CloneOwnershipTransitions(transitions),
		ack:         ack,
	}
	if err := q.sendControl(ctx, cmd); err != nil {
		return semdb.OwnershipTransitionResult{}, err
	}
	select {
	case <-ctx.Done():
		return semdb.OwnershipTransitionResult{}, ctx.Err()
	case <-q.doneCh:
		if err := q.Err(); err != nil {
			return semdb.OwnershipTransitionResult{}, err
		}
		return semdb.OwnershipTransitionResult{}, errors.New("index write queue stopped before ownership transition completed")
	case outcome := <-ack:
		return outcome.result, outcome.err
	}
}

// AcknowledgeOwnershipReconciliation records successful convergence for
// exactly one durable ownership generation. A false store acknowledgement is
// treated as an error so a stale run cannot silently claim convergence.
func (q *Writer) AcknowledgeOwnershipReconciliation(ctx context.Context, generation int64) error {
	if q == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := q.Err(); err != nil {
		return err
	}
	ack := make(chan error, 1)
	if err := q.sendControl(ctx, queueAcknowledgeOwnershipReconciliation{
		barrier: q.currentSubmitBarrier(), generation: generation, ack: ack,
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.doneCh:
		if err := q.Err(); err != nil {
			return err
		}
		return errors.New("index write queue stopped before ownership reconciliation acknowledgement completed")
	case err := <-ack:
		return err
	}
}

func (q *Writer) FlushAndWait(ctx context.Context) (err error) {
	if q == nil {
		return nil
	}
	done := indexingperf.StartSpan(ctx, "writer_flush_barrier")
	defer func() { done(err) }()
	indexingperf.AddCount(ctx, "writer.flush_barriers", 1)
	ack := make(chan error, 1)
	if err := q.sendControl(ctx, queueFlush{barrier: q.currentSubmitBarrier(), ack: ack}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.doneCh:
		if err := q.Err(); err != nil {
			return err
		}
		return errors.New("index write queue stopped before flush completed")
	case err := <-ack:
		return err
	}
}

func (q *Writer) Close() (err error) {
	if q == nil {
		return nil
	}
	done := indexingperf.StartSpan(q.ctx, "writer_close_barrier")
	defer func() { done(err) }()
	indexingperf.AddCount(q.ctx, "writer.close_barriers", 1)
	ack := make(chan error, 1)
	if err := q.sendControl(q.ctx, queueClose{barrier: q.currentSubmitBarrier(), ack: ack}); err != nil {
		<-q.doneCh
		return errors.Join(err, q.Err())
	}
	<-q.doneCh
	select {
	case err := <-ack:
		return err
	default:
		return q.Err()
	}
}

// StopAndWait cancels pending work and waits for any in-flight handler to
// return. Failure cleanup uses this path so the store cannot close while the
// writer still owns it.
func (q *Writer) StopAndWait() error {
	if q == nil {
		return nil
	}
	q.cancel()
	<-q.doneCh
	return q.Err()
}

func (q *Writer) Err() error {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

func (q *Writer) submit(ctx context.Context, cmd any, bytes int) error {
	if q == nil {
		return nil
	}
	if err := q.Err(); err != nil {
		return err
	}
	return q.sendPayload(ctx, queuedPayload{value: cmd, bytes: bytes, phase: indexingperf.PhaseFromContext(ctx)}, false)
}

func (q *Writer) sendControl(ctx context.Context, cmd any) error {
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
		indexingperf.ObserveLatency(ctx, "queue.wait", time.Since(started))
		return nil
	}
}

func (q *Writer) sendPayload(ctx context.Context, cmd queuedPayload, codeLane bool) error {
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
		indexingperf.ObserveLatency(ctx, "queue.wait", waitDur)
		indexingperf.ObserveLatency(ctx, waitMetric, waitDur)
		q.snapshotQueueDepths(ctx)
		indexingperf.AddCount(ctx, "node.write.in", 1)
		if cmd.bytes > 0 {
			indexingperf.AddBytes(ctx, "queue.bytes", int64(cmd.bytes))
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
