package indexwriter

import (
	"context"
	"fmt"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

func (s *queueState) handleCommand(q *Writer, raw any, drainPendingPayloads func() error, ackControl func(pendingControl, semdb.OwnershipTransitionResult, error)) bool {
	switch cmd := raw.(type) {
	case queuedPayload:
		now := time.Now()
		phase := normalizeQueuePhase(cmd.phase)
		switch v := cmd.value.(type) {
		case codeanchor.CodeIndexWork:
			s.lastCodeAt = now
			batch := ensureQueueBatch(s.codeWorks, phase)
			batch.add(v, 1, cmd.bytes, now)
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.CodeIndex, true, true) {
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.replace_code_file"), batch.rows, batch.bytes, func(ctx context.Context) error {
					return q.applyCodeIndexBatch(ctx, batch.items)
				}); err != nil {
					q.fail(err)
					return false
				}
				delete(s.codeWorks, phase)
			}
		case codeanchor.NoteIndexWork:
			batch := ensureQueueBatch(s.noteWorks, phase)
			batch.add(v, 1, cmd.bytes, now)
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.replace_doc_sections"), batch.rows, batch.bytes, func(ctx context.Context) error {
					return q.applyNoteIndexBatch(ctx, batch.items)
				}); err != nil {
					q.fail(err)
					return false
				}
				delete(s.noteWorks, phase)
			}
		case embeddings.NoteFileInfo:
			batch := ensureQueueBatch(s.noteMetas, phase)
			batch.add(v, 1, cmd.bytes, now)
			observeNoteMetaQueueMetrics(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_meta"), batch)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_meta"), "node.write.deferred", 0)
					break
				}
				if err := q.flushNoteMetaBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_meta"), batch); err != nil {
					q.fail(err)
					return false
				}
				delete(s.noteMetas, phase)
			}
		case codeindex.ItemEmbeddingUpsert:
			batch := ensureQueueBatch(s.itemEmbeds, phase)
			batch.add(v, 1, cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.ItemEmbed, true, true) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.ItemEmbed, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), "node.write.deferred", 0)
					break
				}
				rows, bytes := batch.rows, batch.bytes
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), rows, bytes, func(ctx context.Context) error {
					return q.handlers.ApplyCodeItemEmbeddingBatch(ctx, batch.items)
				}); err != nil {
					q.fail(fmt.Errorf("flush code item embeddings rows=%d bytes=%d: %w", rows, bytes, err))
					return false
				}
				delete(s.itemEmbeds, phase)
			}
		case codeItemEmbeddingBatchWrite:
			if len(v.items) == 0 {
				break
			}
			batch := ensureQueueBatch(s.itemEmbeds, phase)
			batch.addAll(v.items, len(v.items), cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.ItemEmbed, true, true) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.ItemEmbed, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), "node.write.deferred", 0)
					break
				}
				rows, bytes := batch.rows, batch.bytes
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), rows, bytes, func(ctx context.Context) error {
					return q.handlers.ApplyCodeItemEmbeddingBatch(ctx, batch.items)
				}); err != nil {
					q.fail(fmt.Errorf("flush code item embedding batch rows=%d bytes=%d: %w", rows, bytes, err))
					return false
				}
				delete(s.itemEmbeds, phase)
			}
		case codeindex.ItemChunksUpsert:
			batch := ensureQueueBatch(s.codeChunkJobs, phase)
			batch.add(v, len(v.Chunks), cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), "node.write.deferred", 0)
					break
				}
				rows, bytes := batch.rows, batch.bytes
				if err := q.flushCodeChunkBatch(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), batch.items); err != nil {
					q.fail(fmt.Errorf("flush code chunk batch rows=%d bytes=%d: %w", rows, bytes, err))
					return false
				}
				delete(s.codeChunkJobs, phase)
			}
		case codeChunkBatchWrite:
			if len(v.items) == 0 {
				break
			}
			batch := ensureQueueBatch(s.codeChunkJobs, phase)
			batch.addAll(v.items, CountCodeChunkRows(v.items), cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), "node.write.deferred", 0)
					break
				}
				rows, bytes := batch.rows, batch.bytes
				if err := q.flushCodeChunkBatch(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), batch.items); err != nil {
					q.fail(fmt.Errorf("flush code chunk batch rows=%d bytes=%d: %w", rows, bytes, err))
					return false
				}
				delete(s.codeChunkJobs, phase)
			}
		case noteChunkSyncWrite:
			batch := ensureQueueBatch(s.noteChunkSyncs, phase)
			batch.add(v.item, noteChunkSyncRows(v.item), cmd.bytes, now)
			observeNoteChunkSyncQueueMetrics(ctxWithPhaseOp(q.ctx, phase, "noteemb.sync_note_chunks"), batch)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "noteemb.sync_note_chunks"), "node.write.deferred", 0)
					break
				}
				if err := q.flushNoteChunkSyncBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.sync_note_chunks"), batch.items); err != nil {
					q.fail(err)
					return false
				}
				delete(s.noteChunkSyncs, phase)
			}
		case noteChunkWrite:
			batch := ensureQueueBatch(s.noteChunkJobs, phase)
			batch.add(v, len(v.chunks), cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_chunks"), "node.write.deferred", 0)
					break
				}
				if err := q.flushNoteChunkBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_chunks"), batch.items); err != nil {
					q.fail(err)
					return false
				}
				delete(s.noteChunkJobs, phase)
			}
		case intelChunkWrite:
			batch := ensureIntelChunkBatch(s.intelChunks, phase, v.family)
			batch.add(v.ownerIDs, v.chunks, cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.replace_chunks"), "node.write.deferred", 0)
					break
				}
				rowsCount, bytes := batch.rows, batch.bytes
				if err := q.flushIntelChunkBatch(ctxWithPhaseOp(q.ctx, phase, "intel.replace_chunks"), batch); err != nil {
					q.fail(fmt.Errorf("flush intel chunks rows=%d bytes=%d: %w", rowsCount, bytes, err))
					return false
				}
				delete(s.intelChunks, phase+"\x00"+v.family)
			}
		case intelEmbeddingWrite:
			batch := ensureIntelEmbeddingBatch(s.intelEmbeds, phase)
			batch.add(v.rows, cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.IntelEmbed, true, true) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.IntelEmbed, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_embeddings"), "node.write.deferred", 0)
					break
				}
				rows := make(map[string]embeddings.Embedding, len(batch.rowsMap))
				for k, row := range batch.rowsMap {
					rows[k] = row
				}
				rowsCount, bytes := batch.rows, batch.bytes
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_embeddings"), rowsCount, bytes, func(ctx context.Context) error {
					return q.handlers.ApplyIntelEmbeddings(ctx, rows)
				}); err != nil {
					q.fail(fmt.Errorf("flush intel embeddings rows=%d bytes=%d: %w", rowsCount, bytes, err))
					return false
				}
				delete(s.intelEmbeds, phase)
			}
		case ontologyNodeWrite:
			batch := ensureQueueBatch(s.ontologyNodes, phase)
			batch.add(v, len(v.model.NotePaths)+len(v.model.Nodes)+len(v.model.FieldValues), cmd.bytes, now)
		case ontologyNodeStateWrite:
			batch := ensureQueueBatch(s.ontologyStates, phase)
			batch.addAll(v.states, len(v.states), cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_ontology_node_embedding_state"), "node.write.deferred", 0)
					break
				}
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_ontology_node_embedding_state"), batch.rows, batch.bytes, func(ctx context.Context) error {
					if q.handlers.ApplyOntologyNodeStates == nil {
						return fmt.Errorf("ontology node state queue not initialized")
					}
					return q.handlers.ApplyOntologyNodeStates(ctx, batch.items)
				}); err != nil {
					q.fail(err)
					return false
				}
				delete(s.ontologyStates, phase)
			}
		case ontologyDeltaWrite:
			batch := ensureOntologyDeltaBatch(s.ontologyOps, phase)
			batch.add(v.delta, cmd.bytes, now)
			codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks)+len(q.codeCmdCh))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, false) {
				if codeHot && !q.cfg.semanticUrgent(q.cfg.DefaultPolicy, batch.rows, batch.bytes) {
					indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.apply_ontology_delta"), "node.write.deferred", 0)
					break
				}
				delta := semdb.NormalizeOntologyDelta(batch.delta)
				if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.apply_ontology_delta"), batch.rows, batch.bytes, func(ctx context.Context) error {
					return q.handlers.ApplyOntologyDelta(ctx, delta)
				}); err != nil {
					q.fail(err)
					return false
				}
				delete(s.ontologyOps, phase)
			}
		case noteMetadataDeltaWrite:
			batch := ensureQueueBatch(s.noteMetadata, phase)
			batch.add(v, noteMetadataDeltaRows(v.delta), cmd.bytes, now)
			indexingperf.SetGauge(indexingperf.WithPhase(q.ctx, phase), noteMetadataQueueDepthName, int64(batch.rows))
		case intentstore.Snapshot:
			batch := ensureQueueBatch(s.intentSnapshots, phase)
			batch.add(v, max(1, len(v.Rows)), cmd.bytes, now)
			indexingperf.SetGauge(indexingperf.WithPhase(q.ctx, phase), intentSnapshotPendingRows, int64(batch.rows))
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, true) {
				if err := q.flushIntentSnapshots(ctxWithPhaseOp(q.ctx, phase, "intel.replace_intent_embeddings"), batch); err != nil {
					q.fail(err)
					return false
				}
				delete(s.intentSnapshots, phase)
			}
		case ValidationStateWrite:
			batch := ensureQueueBatch(s.validation, phase)
			batch.add(v, 1, cmd.bytes, now)
			if s.shouldFlushNow(batch.rows, batch.bytes, q.cfg.DefaultPolicy, false, true) {
				if err := q.flushValidationState(ctxWithPhaseOp(q.ctx, phase, "intel.validation_state"), batch); err != nil {
					q.fail(err)
					return false
				}
				delete(s.validation, phase)
			}
		default:
			q.fail(fmt.Errorf("unsupported queue payload %T", v))
			return false
		}
		q.snapshotQueueDepths(q.ctx)
		return true
	case queueGraphScores:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{barrier: cmd.barrier, graph: &cmd})
		return true
	case queueStructuralFinalize:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{barrier: cmd.barrier, structural: &cmd})
		return true
	case queueDerived:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{barrier: cmd.barrier, derived: &cmd})
		return true
	case queueFlush:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{barrier: cmd.barrier, ack: cmd.ack})
		return true
	case queueClose:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{barrier: cmd.barrier, close: true, ack: cmd.ack})
		return true
	case queueOwnershipTransitions:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{
			barrier:       cmd.barrier,
			transitions:   cmd.transitions,
			transitionAck: cmd.ack,
		})
		return true
	case queueAcknowledgeOwnershipReconciliation:
		s.pendingCtrls = append(s.pendingCtrls, pendingControl{
			barrier:                  cmd.barrier,
			reconciliationGeneration: cmd.generation,
			reconciliationAck:        cmd.ack,
		})
		return true
	default:
		q.fail(fmt.Errorf("unsupported queue command %T", raw))
		return false
	}
}
