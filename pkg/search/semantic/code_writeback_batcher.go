package semantic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

const (
	defaultWritebackQueueCapacity = 64
	defaultWritebackRows          = 256
	defaultWritebackBytes         = 1 << 20
	defaultWritebackIdle          = 100 * time.Millisecond
)

type codeWritebackBatcher struct {
	chunks *writebackBatcher[[]codeindex.ItemChunksUpsert]
	items  *writebackBatcher[[]codeindex.ItemEmbeddingUpsert]
	intel  *writebackBatcher[map[string]embeddings.Embedding]
}

func newCodeWritebackBatcher(ctx context.Context, queue SyncWriteQueue, cfg CodeWritebackBatchConfig) *codeWritebackBatcher {
	if queue == nil {
		return nil
	}
	cfg = normalizeCodeWritebackBatchConfig(cfg)
	return &codeWritebackBatcher{
		chunks: newChunkWritebackBatcher(ctx, queue, cfg),
		items:  newItemWritebackBatcher(ctx, queue, cfg),
		intel:  newIntelWritebackBatcher(ctx, queue, cfg),
	}
}

func (b *codeWritebackBatcher) SubmitCodeItemEmbedding(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
	if b == nil || b.items == nil || len(vec) == 0 {
		return nil
	}
	if err := b.items.Err(); err != nil {
		return err
	}
	items := []codeindex.ItemEmbeddingUpsert{{AnchorID: anchorID, Hash: hash, Embedding: vec}}
	return b.items.submit(ctx, writebackPayload[[]codeindex.ItemEmbeddingUpsert]{
		phase: indexingperf.PhaseFromContext(ctx), values: items, rows: 1, bytes: estimateCodeItemEmbeddingUpserts(items),
	})
}

func (b *codeWritebackBatcher) SubmitCodeChunks(ctx context.Context, anchorID codeindex.AnchorID, chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) error {
	if b == nil || b.chunks == nil || len(chunks) == 0 {
		return nil
	}
	if err := b.chunks.Err(); err != nil {
		return err
	}
	items := []codeindex.ItemChunksUpsert{{AnchorID: anchorID, Chunks: chunks, Texts: texts, Embeddings: vecs}}
	return b.chunks.submit(ctx, writebackPayload[[]codeindex.ItemChunksUpsert]{
		phase: indexingperf.PhaseFromContext(ctx), values: items, rows: len(chunks), bytes: estimateCodeChunkUpserts(items),
	})
}

func (b *codeWritebackBatcher) SubmitIntelEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	if b == nil || b.intel == nil || len(rows) == 0 {
		return nil
	}
	if err := b.intel.Err(); err != nil {
		return err
	}
	cloned := make(map[string]embeddings.Embedding, len(rows))
	for key, vec := range rows {
		if len(vec) == 0 {
			continue
		}
		cloned[key] = append(embeddings.Embedding(nil), vec...)
	}
	if len(cloned) == 0 {
		return nil
	}
	return b.intel.submit(ctx, writebackPayload[map[string]embeddings.Embedding]{
		phase: indexingperf.PhaseFromContext(ctx), values: cloned, rows: len(cloned), bytes: estimateIntelEmbeddingRows(cloned),
	})
}

func (b *codeWritebackBatcher) Close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.chunks != nil {
		err = errors.Join(err, b.chunks.Close())
	}
	if b.items != nil {
		err = errors.Join(err, b.items.Close())
	}
	if b.intel != nil {
		err = errors.Join(err, b.intel.Close())
	}
	return err
}

func normalizeCodeWritebackBatchConfig(cfg CodeWritebackBatchConfig) CodeWritebackBatchConfig {
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = defaultWritebackQueueCapacity
	}
	cfg.ItemEmbed = normalizeWritebackFlushPolicy(cfg.ItemEmbed)
	cfg.CodeChunks = normalizeWritebackFlushPolicy(cfg.CodeChunks)
	cfg.IntelEmbed = normalizeWritebackFlushPolicy(cfg.IntelEmbed)
	return cfg
}

func normalizeWritebackFlushPolicy(policy WritebackFlushPolicy) WritebackFlushPolicy {
	if policy.Rows <= 0 {
		policy.Rows = defaultWritebackRows
	}
	if policy.Bytes <= 0 {
		policy.Bytes = defaultWritebackBytes
	}
	if policy.Idle <= 0 {
		policy.Idle = defaultWritebackIdle
	}
	return policy
}

func phaseCtx(ctx context.Context, phase string) context.Context {
	if phase == "" {
		return ctx
	}
	return indexingperf.WithPhase(ctx, phase)
}

type writebackPressureBand uint8

const (
	writebackPressureNormal writebackPressureBand = iota
	writebackPressureHigh
	writebackPressureCritical
)

type writebackPressureController struct {
	base       WritebackFlushPolicy
	rowsFloor  int
	bytesFloor int
	band       writebackPressureBand
}

// effectivePolicy keeps backlog-driven flushes stable across oscillating queue
// depths; we flush earlier under pressure, but never collapse below lane floors.
func newWritebackPressureController(policy WritebackFlushPolicy, rowsFloor, bytesFloor int) *writebackPressureController {
	return &writebackPressureController{
		base:       policy,
		rowsFloor:  rowsFloor,
		bytesFloor: bytesFloor,
	}
}

func writebackRowsFloor(baseRows, minimum int) int {
	return max(minimum, baseRows/2)
}

func writebackBytesFloor(baseBytes int) int {
	return max(512<<10, baseBytes/2)
}

func queueDepthAtOrAbove(queueDepth, queueCap, percent int) bool {
	if queueCap <= 0 || queueDepth <= 0 {
		return false
	}
	return queueDepth*100 >= queueCap*percent
}

func (c *writebackPressureController) effectivePolicy(queueDepth, queueCap int) WritebackFlushPolicy {
	if c == nil {
		return WritebackFlushPolicy{}
	}
	switch c.band {
	case writebackPressureCritical:
		switch {
		case queueDepthAtOrAbove(queueDepth, queueCap, 90):
		case queueDepthAtOrAbove(queueDepth, queueCap, 60):
			c.band = writebackPressureHigh
		default:
			c.band = writebackPressureNormal
		}
	case writebackPressureHigh:
		switch {
		case queueDepthAtOrAbove(queueDepth, queueCap, 90):
			c.band = writebackPressureCritical
		case queueDepthAtOrAbove(queueDepth, queueCap, 60):
		default:
			c.band = writebackPressureNormal
		}
	default:
		switch {
		case queueDepthAtOrAbove(queueDepth, queueCap, 90):
			c.band = writebackPressureCritical
		case queueDepthAtOrAbove(queueDepth, queueCap, 75):
			c.band = writebackPressureHigh
		}
	}

	policy := c.base
	divisor := 1
	switch c.band {
	case writebackPressureHigh:
		divisor = 2
	case writebackPressureCritical:
		divisor = 4
	}
	if divisor > 1 {
		policy.Rows = max(policy.Rows/divisor, c.rowsFloor)
		policy.Bytes = max(policy.Bytes/divisor, c.bytesFloor)
	}
	return policy
}

func newItemWritebackBatcher(ctx context.Context, queue SyncWriteQueue, cfg CodeWritebackBatchConfig) *writebackBatcher[[]codeindex.ItemEmbeddingUpsert] {
	b := newWritebackBatcher(ctx, cfg.QueueCapacity, cfg.ItemEmbed, 128, "codeembed.writeback.item", func(existing, incoming []codeindex.ItemEmbeddingUpsert) []codeindex.ItemEmbeddingUpsert {
		return append(existing, incoming...)
	})
	b.flush = func(phase string, batch *writebackBatch[[]codeindex.ItemEmbeddingUpsert]) error {
		if batch == nil || len(batch.values) == 0 {
			return nil
		}
		ctx := phaseCtx(b.drainCtx, phase)
		started := time.Now()
		if err := queue.SubmitCodeItemEmbeddingBatch(ctx, batch.values); err != nil {
			return fmt.Errorf("submit code item embedding batch items=%d rows=%d bytes=%d: %w", len(batch.values), batch.rows, batch.bytes, err)
		}
		indexingperf.AddCount(ctx, "codeembed.writeback.item.flush.count", 1)
		indexingperf.ObserveSample(ctx, "codeembed.writeback.item.batch_rows", int64(batch.rows))
		indexingperf.ObserveSample(ctx, "codeembed.writeback.item.batch_bytes", int64(batch.bytes))
		indexingperf.ObserveLatency(ctx, "codeembed.writeback.item.flush", time.Since(started))
		indexingperf.SetGauge(ctx, "codeembed.writeback.item.queue_depth", int64(len(b.submitCh)))
		return nil
	}
	go b.run()
	return b
}

func newChunkWritebackBatcher(ctx context.Context, queue SyncWriteQueue, cfg CodeWritebackBatchConfig) *writebackBatcher[[]codeindex.ItemChunksUpsert] {
	b := newWritebackBatcher(ctx, cfg.QueueCapacity, cfg.CodeChunks, 128, "codeembed.writeback.chunk", func(existing, incoming []codeindex.ItemChunksUpsert) []codeindex.ItemChunksUpsert {
		return append(existing, incoming...)
	})
	b.flush = func(phase string, batch *writebackBatch[[]codeindex.ItemChunksUpsert]) error {
		if batch == nil || len(batch.values) == 0 {
			return nil
		}
		ctx := phaseCtx(b.drainCtx, phase)
		started := time.Now()
		if err := queue.SubmitCodeItemChunkBatch(ctx, batch.values); err != nil {
			return fmt.Errorf("submit code chunk batch items=%d rows=%d bytes=%d: %w", len(batch.values), batch.rows, batch.bytes, err)
		}
		indexingperf.AddCount(ctx, "codeembed.writeback.chunk.flush.count", 1)
		indexingperf.ObserveSample(ctx, "codeembed.writeback.chunk.batch_rows", int64(batch.rows))
		indexingperf.ObserveSample(ctx, "codeembed.writeback.chunk.batch_bytes", int64(batch.bytes))
		indexingperf.ObserveLatency(ctx, "codeembed.writeback.chunk.flush", time.Since(started))
		indexingperf.SetGauge(ctx, "codeembed.writeback.chunk.queue_depth", int64(len(b.submitCh)))
		return nil
	}
	go b.run()
	return b
}

func newIntelWritebackBatcher(ctx context.Context, queue SyncWriteQueue, cfg CodeWritebackBatchConfig) *writebackBatcher[map[string]embeddings.Embedding] {
	b := newWritebackBatcher(ctx, cfg.QueueCapacity, cfg.IntelEmbed, 256, "codeembed.writeback.intel", func(existing, incoming map[string]embeddings.Embedding) map[string]embeddings.Embedding {
		if existing == nil {
			existing = make(map[string]embeddings.Embedding)
		}
		for key, vec := range incoming {
			existing[key] = vec
		}
		return existing
	})
	b.flush = func(phase string, batch *writebackBatch[map[string]embeddings.Embedding]) error {
		if batch == nil || len(batch.values) == 0 {
			return nil
		}
		ctx := phaseCtx(b.drainCtx, phase)
		rows := make(map[string]embeddings.Embedding, len(batch.values))
		for key, vec := range batch.values {
			rows[key] = vec
		}
		started := time.Now()
		if err := queue.SubmitIntelEmbeddings(ctx, rows); err != nil {
			return fmt.Errorf("submit intel embedding batch rows=%d bytes=%d: %w", len(rows), batch.bytes, err)
		}
		indexingperf.AddCount(ctx, "codeembed.writeback.intel.flush.count", 1)
		indexingperf.ObserveSample(ctx, "codeembed.writeback.intel.batch_rows", int64(batch.rows))
		indexingperf.ObserveSample(ctx, "codeembed.writeback.intel.batch_bytes", int64(batch.bytes))
		indexingperf.ObserveLatency(ctx, "codeembed.writeback.intel.flush", time.Since(started))
		indexingperf.SetGauge(ctx, "codeembed.writeback.intel.queue_depth", int64(len(b.submitCh)))
		return nil
	}
	go b.run()
	return b
}

func estimateCodeItemEmbeddingUpserts(items []codeindex.ItemEmbeddingUpsert) int {
	total := 0
	for _, item := range items {
		total += len(item.Hash) + len(item.Embedding)*4 + 64
	}
	return total
}

func estimateCodeChunkUpserts(items []codeindex.ItemChunksUpsert) int {
	total := 0
	for _, item := range items {
		total += len(item.AnchorID)
		for i, chunk := range item.Chunks {
			total += 96 + len(chunk.Granularity) + len(chunk.Breadcrumb) + len(chunk.Heading) + len(chunk.Hash)
			if i < len(item.Texts) {
				total += len(item.Texts[i])
			}
			if i < len(item.Embeddings) {
				total += len(item.Embeddings[i]) * 4
			}
		}
	}
	return total
}

func estimateIntelEmbeddingRows(rows map[string]embeddings.Embedding) int {
	total := 0
	for key, vec := range rows {
		total += len(key) + len(vec)*4 + 32
	}
	return total
}
