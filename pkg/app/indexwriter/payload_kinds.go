package indexwriter

import (
	"context"
	"fmt"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type payloadKind interface {
	offer(*Writer, *queueState, queuedPayload, time.Time) (bool, error)
	flush(*Writer, time.Time, bool) error
	pending() int
}

type kindPolicy struct {
	op                                     string
	policy                                 flushPolicy
	deferred, idle, immediate, hold, drain bool
	ready                                  bool
}

type typedPayloadKind[T any] struct {
	kindPolicy
	batches                   []*queueBatch[T]
	pendingRows, pendingBytes int
	rows                      func(T) int
	apply                     func(context.Context, *queueBatch[T]) error
	observe                   func(context.Context, *queueBatch[T])
}

func payload[T any](policy kindPolicy, rows func(T) int, apply func(context.Context, *queueBatch[T]) error) *typedPayloadKind[T] {
	return &typedPayloadKind[T]{kindPolicy: policy, rows: rows, apply: apply}
}

func (k *typedPayloadKind[T]) pending() int { return len(k.batches) }

func (k *typedPayloadKind[T]) offer(q *Writer, s *queueState, cmd queuedPayload, now time.Time) (bool, error) {
	var items []T
	switch v := cmd.value.(type) {
	case T:
		items = []T{v}
	case []T:
		items = v
	default:
		return false, nil
	}
	if !k.ready {
		return true, fmt.Errorf("%s queue not initialized", k.op)
	}
	phase := normalizeQueuePhase(cmd.phase)
	var batch *queueBatch[T]
	// Separate repeated phases into ordered segments: a -> b -> a must never
	// become a -> a -> b merely because phases share an instrumentation label.
	if n := len(k.batches); n > 0 && k.batches[n-1].phase == phase {
		batch = k.batches[n-1]
	} else {
		batch = &queueBatch[T]{phase: phase}
		k.batches = append(k.batches, batch)
	}
	rows := 0
	for _, item := range items {
		rows += max(1, k.rows(item))
	}
	batch.addAll(items, rows, cmd.bytes, now)
	k.pendingRows += rows
	k.pendingBytes += cmd.bytes
	ctx := ctxWithPhaseOp(q.ctx, phase, k.op)
	if k.observe != nil {
		k.observe(ctx, batch)
	}
	if k.op == "intel.replace_code_file" {
		s.lastCodeAt = now
	}
	if !k.immediate || !s.shouldFlushNow(k.pendingRows, k.pendingBytes, k.policy, k.hold, k.drain) {
		return true, nil
	}
	codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, s.kinds[0].pending()+len(q.codeCmdCh))
	if k.deferred && codeHot && !q.cfg.semanticUrgent(k.policy, k.pendingRows, k.pendingBytes) {
		indexingperf.ObserveLatency(ctx, indexingperf.NodeWriteDeferred, 0)
		return true, nil
	}
	return true, k.flush(q, time.Time{}, false)
}

func (k *typedPayloadKind[T]) flush(q *Writer, now time.Time, codeHot bool) error {
	for len(k.batches) > 0 {
		batch := k.batches[0]
		ctx := ctxWithPhaseOp(q.ctx, batch.phase, k.op)
		if !now.IsZero() {
			if !k.idle || now.Sub(batch.updatedAt) < k.policy.Idle {
				break
			}
			if k.deferred && codeHot {
				indexingperf.ObserveLatency(ctx, indexingperf.NodeWriteDeferred, q.cfg.PollInterval)
				break
			}
		}
		if err := k.apply(ctx, batch); err != nil {
			return err
		}
		k.pendingRows -= batch.rows
		k.pendingBytes -= batch.bytes
		k.batches[0] = nil
		k.batches = k.batches[1:]
	}
	return nil
}

func applyItems[T any](q *Writer, apply func(context.Context, []T) error) func(context.Context, *queueBatch[T]) error {
	return func(ctx context.Context, batch *queueBatch[T]) error {
		return q.flush(ctx, batch.rows, batch.bytes, func(ctx context.Context) error { return apply(ctx, batch.items) })
	}
}

// Slice order is the durable flush order. Raw metadata precedes ontology deltas;
// destructive full deltas precede read-model publication and semantic states.
func newPayloadKinds(q *Writer) []payloadKind {
	basic := func(op string, ready bool) kindPolicy {
		return kindPolicy{op: op, policy: q.cfg.DefaultPolicy, ready: ready, idle: true, immediate: true}
	}
	code := basic("intel.replace_code_file", q.handlers.ApplyCodeIndexBatch != nil)
	code.policy, code.hold, code.drain = q.cfg.CodeIndex, true, true
	noteMeta := basic("noteemb.upsert_note_meta", q.handlers.ApplyNoteMetaBatch != nil)
	noteMeta.deferred = true
	itemEmb := basic("codeemb.upsert_item_embeddings", q.handlers.ApplyCodeItemEmbeddingBatch != nil)
	itemEmb.policy, itemEmb.deferred, itemEmb.hold, itemEmb.drain = q.cfg.ItemEmbed, true, true, true
	chunks := basic("codeemb.upsert_item_chunks", q.handlers.ApplyCodeItemChunkBatch != nil)
	chunks.deferred = true
	noteSync := basic("noteemb.sync_note_chunks", q.handlers.ApplyNoteChunkSyncBatch != nil)
	noteSync.deferred = true
	intelChunks := basic("intel.replace_chunks", q.handlers.ApplyIntelChunks != nil || q.handlers.ApplyIntelChunksByFamily != nil)
	intelChunks.deferred = true
	intelEmb := basic("intel.upsert_embeddings", q.handlers.ApplyIntelEmbeddings != nil)
	intelEmb.policy, intelEmb.deferred, intelEmb.hold, intelEmb.drain = q.cfg.IntelEmbed, true, true, true
	metadata := basic("intel.apply_note_metadata_delta", q.handlers.ApplyNoteMetadataDelta != nil)
	metadata.immediate = false
	delta := basic("intel.apply_ontology_delta", q.handlers.ApplyOntologyDelta != nil)
	delta.deferred = true
	nodes := basic("intel.replace_ontology_nodes", q.handlers.ApplyOntologyNodeReadModel != nil)
	nodes.idle, nodes.immediate = false, false
	states := basic("intel.upsert_ontology_node_embedding_state", q.handlers.ApplyOntologyNodeStates != nil)
	states.deferred = true
	validation := basic("intel.validation_state", q.handlers.ApplyValidationState != nil)
	validation.drain = true
	metas := payload(noteMeta, func(embeddings.NoteFileInfo) int { return 1 }, q.flushNoteMetaBatch)
	metas.observe = observeNoteMetaQueueMetrics
	syncs := payload(noteSync, noteChunkSyncRows, func(ctx context.Context, b *queueBatch[embeddings.NoteChunkSync]) error {
		return q.flushNoteChunkSyncBatch(ctx, b.items)
	})
	syncs.observe = observeNoteChunkSyncQueueMetrics
	metadataKind := payload(metadata, func(v noteMetadataDeltaWrite) int { return noteMetadataDeltaRows(v.delta) }, q.flushNoteMetadataDeltas)
	metadataKind.observe = func(ctx context.Context, b *queueBatch[noteMetadataDeltaWrite]) {
		indexingperf.SetGauge(ctx, noteMetadataQueueDepthName, int64(b.rows))
	}
	return []payloadKind{
		payload(code, func(codeanchor.CodeIndexWork) int { return 1 }, applyItems(q, q.applyCodeIndexBatch)),
		payload(basic("intel.replace_doc_sections", q.handlers.ApplyNoteIndexBatch != nil), func(codeanchor.NoteIndexWork) int { return 1 }, applyItems(q, q.applyNoteIndexBatch)),
		metas,
		payload(itemEmb, func(codeindex.ItemEmbeddingUpsert) int { return 1 }, applyItems(q, q.handlers.ApplyCodeItemEmbeddingBatch)),
		payload(chunks, func(v codeindex.ItemChunksUpsert) int { return len(v.Chunks) }, func(ctx context.Context, b *queueBatch[codeindex.ItemChunksUpsert]) error {
			return q.flushCodeChunkBatch(ctx, b.items)
		}),
		syncs,
		payload(intelChunks, func(v intelChunkWrite) int { return len(v.chunks) }, q.flushIntelChunkWrites),
		payload(intelEmb, func(v intelEmbeddingWrite) int { return len(v.rows) }, q.flushIntelEmbeddingWrites),
		metadataKind,
		payload(delta, func(v ontologyDeltaWrite) int { return v.delta.RowCount() }, q.flushOntologyDeltas),
		payload(nodes, func(v ontologyNodeWrite) int {
			return len(v.model.NotePaths) + len(v.model.Nodes) + len(v.model.FieldValues)
		}, q.flushOntologyNodeWrites),
		payload(states, func(codeanchor.IntelOntologyNodeEmbeddingState) int { return 1 }, applyItems(q, q.handlers.ApplyOntologyNodeStates)),
		payload(validation, func(ValidationStateWrite) int { return 1 }, q.flushValidationState),
	}
}

func normalizePayload(value any) any {
	switch v := value.(type) {
	case codeItemEmbeddingBatchWrite:
		return v.items
	case codeChunkBatchWrite:
		return v.items
	case noteChunkSyncWrite:
		return v.item
	case ontologyNodeStateWrite:
		return v.states
	default:
		return value
	}
}

func (q *Writer) flushIntelChunkWrites(ctx context.Context, b *queueBatch[intelChunkWrite]) error {
	for i := 0; i < len(b.items); {
		batch := &intelChunkBatch{family: b.items[i].family}
		for ; i < len(b.items) && b.items[i].family == batch.family; i++ {
			v := b.items[i]
			batch.add(v.ownerIDs, v.chunks, len(v.ownerIDs)*48+len(v.chunks)*160)
		}
		batch.rows = max(1, batch.rows)
		if err := q.flushIntelChunkBatch(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}
func (q *Writer) flushIntelEmbeddingWrites(ctx context.Context, b *queueBatch[intelEmbeddingWrite]) error {
	rows := make(map[string]embeddings.Embedding)
	for _, v := range b.items {
		for key, row := range v.rows {
			rows[key] = row
		}
	}
	return q.flush(ctx, b.rows, b.bytes, func(ctx context.Context) error { return q.handlers.ApplyIntelEmbeddings(ctx, rows) })
}
func (q *Writer) flushOntologyDeltas(ctx context.Context, b *queueBatch[ontologyDeltaWrite]) error {
	var delta semdb.OntologyDelta
	for _, v := range b.items {
		semdb.MergeOntologyDelta(&delta, v.delta)
	}
	delta = semdb.NormalizeOntologyDelta(delta)
	return q.flush(ctx, b.rows, b.bytes, func(ctx context.Context) error { return q.handlers.ApplyOntologyDelta(ctx, delta) })
}
