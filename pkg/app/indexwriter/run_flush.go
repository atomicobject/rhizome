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

type queueState struct {
	codeWorks       map[string]*queueBatch[codeanchor.CodeIndexWork]
	noteWorks       map[string]*queueBatch[codeanchor.NoteIndexWork]
	noteMetas       map[string]*queueBatch[embeddings.NoteFileInfo]
	itemEmbeds      map[string]*queueBatch[codeindex.ItemEmbeddingUpsert]
	codeChunkJobs   map[string]*queueBatch[codeindex.ItemChunksUpsert]
	noteChunkSyncs  map[string]*queueBatch[embeddings.NoteChunkSync]
	noteChunkJobs   map[string]*queueBatch[noteChunkWrite]
	intelChunks     map[string]*intelChunkBatch
	intelEmbeds     map[string]*intelEmbeddingBatch
	ontologyNodes   map[string]*queueBatch[ontologyNodeWrite]
	ontologyStates  map[string]*queueBatch[codeanchor.IntelOntologyNodeEmbeddingState]
	noteMetadata    map[string]*queueBatch[noteMetadataDeltaWrite]
	ontologyOps     map[string]*ontologyDeltaBatch
	validation      map[string]*queueBatch[ValidationStateWrite]
	intentSnapshots map[string]*queueBatch[intentstore.Snapshot]
	pendingCtrls    []pendingControl
	holdingForCtrl  bool
	drainingToCtrl  bool
	lastCodeAt      time.Time
}

func (s *queueState) shouldFlushNow(rows, bytes int, policy FlushPolicy, allowDuringHold bool, allowDuringDrain bool) bool {
	// Barrier drains should absorb most already-enqueued payloads into memory
	// and let the trailing flushAll() persist them as one bounded sync point.
	// Code-index work keeps its own threshold semantics so close/flush retains
	// the existing code-lane batching behavior.
	if s.holdingForCtrl && !allowDuringHold {
		return false
	}
	if s.drainingToCtrl && !allowDuringDrain {
		return false
	}
	return rows >= policy.Rows || bytes >= policy.Bytes
}

func (s *queueState) flushAll(q *Writer) error {
	for phase, batch := range s.codeWorks {
		if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.replace_code_file"), batch.rows, batch.bytes, func(ctx context.Context) error {
			return q.applyCodeIndexBatch(ctx, batch.items)
		}); err != nil {
			return err
		}
		delete(s.codeWorks, phase)
	}
	for phase, batch := range s.noteWorks {
		if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.replace_doc_sections"), batch.rows, batch.bytes, func(ctx context.Context) error {
			return q.applyNoteIndexBatch(ctx, batch.items)
		}); err != nil {
			return err
		}
		delete(s.noteWorks, phase)
	}
	for phase, batch := range s.noteMetas {
		if err := q.flushNoteMetaBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_meta"), batch); err != nil {
			return err
		}
		delete(s.noteMetas, phase)
	}
	for phase, batch := range s.itemEmbeds {
		if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), batch.rows, batch.bytes, func(ctx context.Context) error {
			return q.handlers.ApplyCodeItemEmbeddingBatch(ctx, batch.items)
		}); err != nil {
			return err
		}
		delete(s.itemEmbeds, phase)
	}
	for phase, batch := range s.codeChunkJobs {
		if err := q.flushCodeChunkBatch(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), batch.items); err != nil {
			return err
		}
		delete(s.codeChunkJobs, phase)
	}
	for phase, batch := range s.noteChunkSyncs {
		if err := q.flushNoteChunkSyncBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.sync_note_chunks"), batch.items); err != nil {
			return err
		}
		delete(s.noteChunkSyncs, phase)
	}
	for phase, batch := range s.noteChunkJobs {
		if err := q.flushNoteChunkBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_chunks"), batch.items); err != nil {
			return err
		}
		delete(s.noteChunkJobs, phase)
	}
	for key, batch := range s.intelChunks {
		if err := q.flushIntelChunkBatch(ctxWithPhaseOp(q.ctx, batch.phase, "intel.replace_chunks"), batch); err != nil {
			return err
		}
		delete(s.intelChunks, key)
	}
	for phase, batch := range s.intelEmbeds {
		rows := make(map[string]embeddings.Embedding, len(batch.rowsMap))
		for k, v := range batch.rowsMap {
			rows[k] = v
		}
		if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_embeddings"), batch.rows, batch.bytes, func(ctx context.Context) error {
			return q.handlers.ApplyIntelEmbeddings(ctx, rows)
		}); err != nil {
			return err
		}
		delete(s.intelEmbeds, phase)
	}
	// IMPORTANT: raw note metadata must be durable before ontology reads or
	// writes derived state. Validation projection also uses an explicit barrier,
	// while this ordering protects any shared final drain.
	for phase, batch := range s.noteMetadata {
		if err := q.flushNoteMetadataDeltas(ctxWithPhaseOp(q.ctx, phase, "intel.apply_note_metadata_delta"), batch); err != nil {
			return err
		}
		delete(s.noteMetadata, phase)
	}
	// IMPORTANT: s.ontologyOps must drain BEFORE s.ontologyNodes. On a FullRebuild
	// delta the apply truncates ontology_nodes / ontology_node_field_values; if
	// the read-model batch flushed first, its inserts would be wiped by the
	// trailing delta. Apply the delta first so the truncate runs against an
	// empty/old slate, then the read-model write populates the new rows.
	for phase, batch := range s.ontologyOps {
		delta := semdb.NormalizeOntologyDelta(batch.delta)
		if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.apply_ontology_delta"), batch.rows, batch.bytes, func(ctx context.Context) error {
			return q.handlers.ApplyOntologyDelta(ctx, delta)
		}); err != nil {
			return err
		}
		delete(s.ontologyOps, phase)
	}
	for phase, batch := range s.ontologyNodes {
		if err := q.flushOntologyNodeWrites(ctxWithPhaseOp(q.ctx, phase, "intel.replace_ontology_nodes"), batch); err != nil {
			return err
		}
		delete(s.ontologyNodes, phase)
	}
	for phase, batch := range s.ontologyStates {
		if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_ontology_node_embedding_state"), batch.rows, batch.bytes, func(ctx context.Context) error {
			if q.handlers.ApplyOntologyNodeStates == nil {
				return fmt.Errorf("ontology node state queue not initialized")
			}
			return q.handlers.ApplyOntologyNodeStates(ctx, batch.items)
		}); err != nil {
			return err
		}
		delete(s.ontologyStates, phase)
	}
	for phase, batch := range s.intentSnapshots {
		if err := q.flushIntentSnapshots(ctxWithPhaseOp(q.ctx, phase, "intel.replace_intent_embeddings"), batch); err != nil {
			return err
		}
		delete(s.intentSnapshots, phase)
	}
	for phase, batch := range s.validation {
		if err := q.flushValidationState(ctxWithPhaseOp(q.ctx, phase, "intel.validation_state"), batch); err != nil {
			return err
		}
		delete(s.validation, phase)
	}
	q.snapshotQueueDepths(q.ctx)
	return nil
}

func (s *queueState) flushExpired(q *Writer, now time.Time) error {
	for phase, batch := range s.codeWorks {
		if now.Sub(batch.updatedAt) >= q.cfg.CodeIndex.Idle {
			if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.replace_code_file"), batch.rows, batch.bytes, func(ctx context.Context) error {
				return q.applyCodeIndexBatch(ctx, batch.items)
			}); err != nil {
				return err
			}
			delete(s.codeWorks, phase)
		}
	}
	codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, len(s.codeWorks))
	// Once code indexing cools down, deferred semantic batches drain using
	// their own policies. The timings output distinguishes true enqueue
	// blocking from this intentional writer_deferred behavior.
	for phase, batch := range s.noteWorks {
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.replace_doc_sections"), batch.rows, batch.bytes, func(ctx context.Context) error {
				return q.applyNoteIndexBatch(ctx, batch.items)
			}); err != nil {
				return err
			}
			delete(s.noteWorks, phase)
		}
	}
	for phase, batch := range s.noteMetas {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_meta"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushNoteMetaBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_meta"), batch); err != nil {
				return err
			}
			delete(s.noteMetas, phase)
		}
	}
	for phase, batch := range s.itemEmbeds {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.ItemEmbed.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.ItemEmbed.Idle {
			if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_embeddings"), batch.rows, batch.bytes, func(ctx context.Context) error {
				return q.handlers.ApplyCodeItemEmbeddingBatch(ctx, batch.items)
			}); err != nil {
				return err
			}
			delete(s.itemEmbeds, phase)
		}
	}
	for phase, batch := range s.codeChunkJobs {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushCodeChunkBatch(ctxWithPhaseOp(q.ctx, phase, "codeemb.upsert_item_chunks"), batch.items); err != nil {
				return err
			}
			delete(s.codeChunkJobs, phase)
		}
	}
	for phase, batch := range s.noteChunkSyncs {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "noteemb.sync_note_chunks"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushNoteChunkSyncBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.sync_note_chunks"), batch.items); err != nil {
				return err
			}
			delete(s.noteChunkSyncs, phase)
		}
	}
	for phase, batch := range s.noteChunkJobs {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_chunks"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushNoteChunkBatch(ctxWithPhaseOp(q.ctx, phase, "noteemb.upsert_note_chunks"), batch.items); err != nil {
				return err
			}
			delete(s.noteChunkJobs, phase)
		}
	}
	for key, batch := range s.intelChunks {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, batch.phase, "intel.replace_chunks"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushIntelChunkBatch(ctxWithPhaseOp(q.ctx, batch.phase, "intel.replace_chunks"), batch); err != nil {
				return err
			}
			delete(s.intelChunks, key)
		}
	}
	for phase, batch := range s.intelEmbeds {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.IntelEmbed.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_embeddings"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.IntelEmbed.Idle {
			rows := make(map[string]embeddings.Embedding, len(batch.rowsMap))
			for k, v := range batch.rowsMap {
				rows[k] = v
			}
			if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_embeddings"), batch.rows, batch.bytes, func(ctx context.Context) error {
				return q.handlers.ApplyIntelEmbeddings(ctx, rows)
			}); err != nil {
				return err
			}
			delete(s.intelEmbeds, phase)
		}
	}
	// Ontology read-model writes intentionally do not idle-flush. FullRebuild
	// deltas are destructive, and they can arrive after a node batch in the
	// same queue run. flushAll is the ordering boundary that applies deltas
	// first, then persists the queued read model.
	for phase, batch := range s.noteMetadata {
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushNoteMetadataDeltas(ctxWithPhaseOp(q.ctx, phase, "intel.apply_note_metadata_delta"), batch); err != nil {
				return err
			}
			delete(s.noteMetadata, phase)
		}
	}
	for phase, batch := range s.ontologyStates {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_ontology_node_embedding_state"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.upsert_ontology_node_embedding_state"), batch.rows, batch.bytes, func(ctx context.Context) error {
				if q.handlers.ApplyOntologyNodeStates == nil {
					return fmt.Errorf("ontology node state queue not initialized")
				}
				return q.handlers.ApplyOntologyNodeStates(ctx, batch.items)
			}); err != nil {
				return err
			}
			delete(s.ontologyStates, phase)
		}
	}
	for phase, batch := range s.ontologyOps {
		if codeHot && now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			indexingperf.ObserveLatency(ctxWithPhaseOp(q.ctx, phase, "intel.apply_ontology_delta"), "node.write.deferred", q.cfg.PollInterval)
			continue
		}
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			delta := semdb.NormalizeOntologyDelta(batch.delta)
			if err := q.flush(ctxWithPhaseOp(q.ctx, phase, "intel.apply_ontology_delta"), batch.rows, batch.bytes, func(ctx context.Context) error {
				return q.handlers.ApplyOntologyDelta(ctx, delta)
			}); err != nil {
				return err
			}
			delete(s.ontologyOps, phase)
		}
	}
	for phase, batch := range s.intentSnapshots {
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushIntentSnapshots(ctxWithPhaseOp(q.ctx, phase, "intel.replace_intent_embeddings"), batch); err != nil {
				return err
			}
			delete(s.intentSnapshots, phase)
		}
	}
	for phase, batch := range s.validation {
		if now.Sub(batch.updatedAt) >= q.cfg.DefaultPolicy.Idle {
			if err := q.flushValidationState(ctxWithPhaseOp(q.ctx, phase, "intel.validation_state"), batch); err != nil {
				return err
			}
			delete(s.validation, phase)
		}
	}
	return nil
}
