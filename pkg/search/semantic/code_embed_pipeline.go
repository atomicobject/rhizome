package semantic

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

const (
	defaultCodePipelineReadyTexts = 16384
	defaultCodePipelineReadyBytes = 32 << 20
)

type codePipelineJobKind uint8

const (
	codePipelineChunkJob codePipelineJobKind = iota
	codePipelineItemOnlyJob
)

type codePreparedTask struct {
	anchorID             codeindex.AnchorID
	keepIndices          []int
	useLegacy            bool
	itemHash             string
	itemText             string
	itemOnly             bool
	itemNeedsWrite       bool
	itemVec              embeddings.Embedding
	reuseInputs          []codeindex.ChunkInput
	reuseTexts           []string
	reuseVecs            []embeddings.Embedding
	missingInputs        []codeindex.ChunkInput
	missingTexts         []string
	itemEmbedMissingIdx  int
	readyAt              time.Time
	missingProviderCalls int
}

func (t *codePreparedTask) pipelineProviderCalls() int {
	if t == nil {
		return 0
	}
	return t.missingProviderCalls
}

type codePipelineJob struct {
	anchorID   codeindex.AnchorID
	kind       codePipelineJobKind
	missingIdx int
	text       string
	bytes      int
}

func (j codePipelineJob) pipelineOwnerKey() codeindex.AnchorID { return j.anchorID }
func (j codePipelineJob) pipelineText() string                 { return j.text }
func (j codePipelineJob) pipelineBytes() int                   { return j.bytes }

type codePipelineOwner struct {
	prepared    *codePreparedTask
	missingVecs []embeddings.Embedding
	pending     int
}

func (o *codePipelineOwner) pipelineReadyAt() time.Time { return o.prepared.readyAt }
func (o *codePipelineOwner) pipelinePending() int       { return o.pending }
func (o *codePipelineOwner) pipelineSetPending(n int)   { o.pending = n }
func (o *codePipelineOwner) pipelineDoneCalls() int {
	if o == nil || o.prepared == nil {
		return 0
	}
	return o.prepared.missingProviderCalls
}

type codeEmbeddingPipeline struct {
	preparedCh chan *codePreparedTask
	stage      *preparedEmbedStage[codeindex.AnchorID, *codePreparedTask, *codePipelineOwner, codePipelineJob]
}

// newCodeEmbeddingPipelineWithNode wires code embedding through a shared node
// when semanticruntime has found a compatible provider lane.
func newCodeEmbeddingPipelineWithNode(ctx context.Context, provider embeddings.Provider, sharedNode *SharedEmbeddingNode, maxConcurrent int, globalSem chan struct{}, opts EmbedPackerOptions) *codeEmbeddingPipeline {
	stage := newPreparedEmbedStage(
		ctx,
		provider,
		sharedNode,
		maxConcurrent,
		globalSem,
		opts,
		func(prepared *codePreparedTask) (codeindex.AnchorID, *codePipelineOwner, []codePipelineJob) {
			prepared.readyAt = time.Now()
			owner := &codePipelineOwner{
				prepared:    prepared,
				missingVecs: make([]embeddings.Embedding, len(prepared.missingInputs)),
			}
			jobs := make([]codePipelineJob, 0, len(prepared.missingTexts)+1)
			for idx, text := range prepared.missingTexts {
				jobs = append(jobs, codePipelineJob{
					anchorID:   prepared.anchorID,
					kind:       codePipelineChunkJob,
					missingIdx: idx,
					text:       text,
					bytes:      len(text),
				})
			}
			if prepared.itemOnly {
				jobs = append(jobs, codePipelineJob{
					anchorID: prepared.anchorID,
					kind:     codePipelineItemOnlyJob,
					text:     prepared.itemText,
					bytes:    len(prepared.itemText),
				})
			}
			return prepared.anchorID, owner, jobs
		},
		func(owner *codePipelineOwner, job codePipelineJob, vec embeddings.Embedding) error {
			switch job.kind {
			case codePipelineItemOnlyJob:
				owner.prepared.itemVec = vec
			default:
				if job.missingIdx >= 0 && job.missingIdx < len(owner.missingVecs) {
					owner.missingVecs[job.missingIdx] = vec
					if owner.prepared.itemEmbedMissingIdx == job.missingIdx {
						owner.prepared.itemVec = vec
					}
				}
			}
			return nil
		},
	)
	return &codeEmbeddingPipeline{preparedCh: stage.preparedCh, stage: stage}
}

func (p *codeEmbeddingPipeline) run(finalize func(*codePipelineOwner) error) error {
	if p == nil || p.stage == nil {
		return nil
	}
	return p.stage.run(finalize)
}

func (s *Syncer) processTasksPipelined(ctx context.Context, plan SyncPlan, itemWriter func(context.Context, codeindex.AnchorID, string, embeddings.Embedding) error, intelWriter intelEmbeddingSubmitter, writebackBatcher *codeWritebackBatcher) error {
	return s.ComputeDerived(ctx, plan, func(ctx context.Context, result CodeDerivedResult) error {
		return applySemanticWriteIntents(ctx, codeWriteSink{syncer: s, useLegacy: result.prepared.useLegacy, itemWriter: itemWriter, intelWriter: intelWriter, queueBatcher: writebackBatcher}, buildCodeWriteIntents(result.prepared, result.vectors, result.prepared.itemVec, s.EmbeddingWriter != nil)...)
	})
}

func (s *Syncer) prepareTaskForPipeline(ctx context.Context, task codeTask, state codeindex.ItemEmbeddingState, reuseCache *codeReuseCache, force bool) (*codePreparedTask, error) {
	indexingperf.AddCount(ctx, "codeembed.plan.chunks", int64(len(task.payload.chunks)))
	recordSemanticChunkCounts(ctx, task.payload.chunks)
	recordCodeTaskChunkCounts(ctx, task)
	prepared := &codePreparedTask{
		anchorID:            task.id,
		useLegacy:           s.EmbeddingWriter == nil && s.Index != nil,
		itemEmbedMissingIdx: -1,
	}
	if len(task.payload.chunks) == 0 {
		return prepared, nil
	}

	existingHashes := state.ChunkHashes
	if existingHashes == nil || force {
		existingHashes = map[int]string{}
	}

	for _, chunk := range task.payload.chunks {
		if chunk.Input.Index != 0 {
			continue
		}
		itemHash := chunk.Input.Hash
		prepared.itemText = chunk.Text
		prepared.itemHash = itemHash
		prepared.itemNeedsWrite = force || !state.HasItemHash || state.ItemHash != itemHash
		break
	}

	if prepared.itemHash != "" {
		itemVec, ok, err := s.lookupCodeReuseEmbedding(ctx, reuseCache, prepared.itemHash)
		if err != nil {
			return nil, err
		}
		if ok && len(itemVec) > 0 {
			prepared.itemVec = itemVec
		} else if !prepared.itemNeedsWrite && existingHashes[0] == prepared.itemHash {
			started := time.Now()
			itemVec, _, ok, err = s.Index.GetItemEmbedding(ctx, task.id)
			indexingperf.ObserveLatency(ctx, "codeembed.item_lookup", time.Since(started))
			if err != nil {
				return nil, err
			}
			if ok && len(itemVec) > 0 {
				prepared.itemVec = itemVec
			}
		}
	}

	for _, chunk := range task.payload.chunks {
		prepared.keepIndices = append(prepared.keepIndices, chunk.Input.Index)

		if prepared.itemHash != "" && chunk.Input.Hash == prepared.itemHash && len(prepared.itemVec) > 0 {
			// IMPORTANT: the item vector can satisfy the first chunk when hashes
			// match. This avoids a duplicate provider call and keeps item-level
			// and chunk-level embeddings byte-identical for the primary chunk.
			indexingperf.AddCount(ctx, "codeembed.reuse.item_vector", 1)
			prepared.reuseInputs = append(prepared.reuseInputs, chunk.Input)
			prepared.reuseTexts = append(prepared.reuseTexts, chunk.Text)
			prepared.reuseVecs = append(prepared.reuseVecs, prepared.itemVec)
			continue
		}
		if !force && existingHashes[chunk.Input.Index] == chunk.Input.Hash {
			indexingperf.AddCount(ctx, "codeembed.reuse.same_position", 1)
			continue
		}

		cached, ok, err := s.lookupCodeReuseEmbedding(ctx, reuseCache, chunk.Input.Hash)
		if err != nil {
			return nil, err
		}
		if ok && len(cached) > 0 {
			indexingperf.AddCount(ctx, "codeembed.reuse.content_hash", 1)
			// Durable content-hash reuse is the main full-scan optimization:
			// unchanged chunk text can skip provider submission even when the
			// owning code item is being reindexed.
			prepared.reuseInputs = append(prepared.reuseInputs, chunk.Input)
			prepared.reuseTexts = append(prepared.reuseTexts, chunk.Text)
			prepared.reuseVecs = append(prepared.reuseVecs, cached)
			if prepared.itemHash != "" && chunk.Input.Hash == prepared.itemHash && len(prepared.itemVec) == 0 {
				prepared.itemVec = cached
			}
			continue
		}
		if prepared.itemNeedsWrite && len(prepared.itemVec) == 0 && chunk.Input.Hash == prepared.itemHash && prepared.itemEmbedMissingIdx == -1 {
			prepared.itemEmbedMissingIdx = len(prepared.missingInputs)
		}
		indexingperf.AddCount(ctx, "codeembed.plan.embed_chunks", 1)
		prepared.missingInputs = append(prepared.missingInputs, chunk.Input)
		prepared.missingTexts = append(prepared.missingTexts, chunk.Text)
		prepared.missingProviderCalls++
	}

	if prepared.itemNeedsWrite && len(prepared.itemVec) == 0 && len(prepared.missingInputs) == 0 && prepared.itemText != "" {
		indexingperf.AddCount(ctx, "codeembed.plan.embed_item_only", 1)
		prepared.itemOnly = true
		prepared.missingProviderCalls++
	}

	return prepared, nil
}

func (s *Syncer) lookupCodeReuseEmbedding(ctx context.Context, reuseCache *codeReuseCache, hash string) (embeddings.Embedding, bool, error) {
	if vec, ok, known := reuseCache.get(hash); known {
		if ok && len(vec) > 0 {
			indexingperf.AddCount(ctx, "codeembed.reuse_mem.hit", 1)
			return vec, true, nil
		}
		indexingperf.AddCount(ctx, "codeembed.reuse_mem.miss", 1)
		return nil, false, nil
	}
	indexingperf.AddCount(ctx, "codeembed.reuse_mem.miss", 1)
	started := time.Now()
	cached, ok, err := s.Index.EmbeddingByHash(ctx, hash)
	indexingperf.ObserveLatency(ctx, "codeembed.cache_lookup", time.Since(started))
	if err != nil {
		indexingperf.AddCount(ctx, "codeembed.hash_cache.error", 1)
		return nil, false, err
	}
	if ok && len(cached) > 0 {
		indexingperf.AddCount(ctx, "codeembed.hash_cache.hit", 1)
		reuseCache.put(hash, cached)
		return cached, true, nil
	}
	indexingperf.AddCount(ctx, "codeembed.hash_cache.miss", 1)
	reuseCache.noteMiss(hash)
	return nil, false, nil
}

func (s *Syncer) recordPreparedReuseEmbeddings(reuseCache *codeReuseCache, prepared *codePreparedTask, missingVecs []embeddings.Embedding, itemVec embeddings.Embedding) {
	if reuseCache == nil || prepared == nil {
		return
	}
	for i, input := range prepared.reuseInputs {
		if i < len(prepared.reuseVecs) {
			reuseCache.put(input.Hash, prepared.reuseVecs[i])
		}
	}
	for i, input := range prepared.missingInputs {
		if i < len(missingVecs) {
			reuseCache.put(input.Hash, missingVecs[i])
		}
	}
	if prepared.itemHash != "" && len(itemVec) > 0 {
		reuseCache.put(prepared.itemHash, itemVec)
	}
}
