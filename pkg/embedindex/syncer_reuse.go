package embedindex

// Docs:
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"context"

	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func pickModuleAnchor(pathAnchors []codeanchor.IntelAnchor) (id codeindex.AnchorID, fingerprint string, lang codeanchor.Lang) {
	for _, a := range pathAnchors {
		if strings.EqualFold(a.Kind, "module") {
			return codeindex.AnchorID(a.AnchorID), a.Fingerprint, a.Lang
		}
	}
	return "", "", ""
}

// filterResult contains the filtered tasks and cache hit statistics.
type filterResult struct {
	tasks     []codeTask
	cacheHits int
	states    map[codeindex.AnchorID]codeindex.ItemEmbeddingState
}

func (s *Syncer) filterTasksNeedingEmbedding(ctx context.Context, tasks []codeTask, force bool) (filterResult, error) {
	// When force=true (rebuild), we process all tasks but still check cache hits
	// for accurate progress reporting.
	if force {
		cacheHits, err := s.countCachedPrimaryChunks(ctx, tasks)
		if err != nil {
			return filterResult{}, err
		}
		return filterResult{tasks: tasks, cacheHits: cacheHits}, nil
	}
	anchorIDs := uniqueAnchorIDs(tasks)
	var stateByAnchor map[codeindex.AnchorID]codeindex.ItemEmbeddingState
	if s.EmbeddingWriter != nil {
		if intelStates, ok := s.Intel.(IntelEmbeddingStateSource); ok {
			ownerIDs := make([]string, 0, len(anchorIDs))
			for _, id := range anchorIDs {
				if id == "" {
					continue
				}
				ownerIDs = append(ownerIDs, string(id))
			}
			intelHashes, err := intelStates.IntelChunkEmbeddingStates(ctx, ownerIDs)
			if err != nil {
				return filterResult{}, err
			}
			stateByAnchor = make(map[codeindex.AnchorID]codeindex.ItemEmbeddingState, len(intelHashes))
			for ownerID, hashes := range intelHashes {
				if strings.TrimSpace(ownerID) == "" {
					continue
				}
				state := codeindex.ItemEmbeddingState{ChunkHashes: hashes}
				if hashes != nil {
					if hash, ok := hashes[0]; ok && strings.TrimSpace(hash) != "" {
						state.ItemHash = hash
						state.HasItemHash = true
					}
				}
				stateByAnchor[codeindex.AnchorID(ownerID)] = state
			}
		}
	}
	if stateByAnchor == nil {
		started := time.Now()
		var err error
		stateByAnchor, err = s.Index.ItemEmbeddingStates(ctx, anchorIDs)
		indexingperf.ObserveLatency(ctx, indexingperf.PlanItemEmbeddingStates, time.Since(started))
		if err != nil {
			return filterResult{}, err
		}
	}

	neededFlags := make([]bool, len(tasks))
	workerCount := s.planConcurrent()
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	taskCh := make(chan int, workerCount*2)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range taskCh {
				if ctx.Err() != nil {
					return
				}
				if s.needsEmbedding(ctx, tasks[idx], stateByAnchor[tasks[idx].id]) {
					neededFlags[idx] = true
				}
			}
		}()
	}
	for idx := range tasks {
		if ctx.Err() != nil {
			break
		}
		taskCh <- idx
	}
	close(taskCh)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return filterResult{}, err
	}

	var needed []codeTask
	for idx, task := range tasks {
		if neededFlags[idx] {
			needed = append(needed, task)
		}
	}

	cacheHits, err := s.countCachedPrimaryChunks(ctx, needed)
	if err != nil {
		return filterResult{}, err
	}

	neededStates := make(map[codeindex.AnchorID]codeindex.ItemEmbeddingState, len(needed))
	for _, task := range needed {
		if state, ok := stateByAnchor[task.id]; ok {
			neededStates[task.id] = state
		}
	}

	return filterResult{tasks: needed, cacheHits: cacheHits, states: neededStates}, nil
}

func (s *Syncer) countCachedPrimaryChunks(ctx context.Context, tasks []codeTask) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}
	hashes := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if len(task.payload.chunks) == 0 {
			continue
		}
		hashes = append(hashes, task.payload.chunks[0].Input.Hash)
	}
	rows, err := s.prefetchEmbeddingHashes(ctx, hashes)
	if err != nil {
		return 0, err
	}
	cacheHits := 0
	for _, hash := range hashes {
		if vec, ok := rows[hash]; ok && len(vec) > 0 {
			cacheHits++
		}
	}
	return cacheHits, nil
}

func uniqueAnchorIDs(tasks []codeTask) []codeindex.AnchorID {
	seen := make(map[codeindex.AnchorID]struct{}, len(tasks))
	var ids []codeindex.AnchorID
	for _, task := range tasks {
		if _, ok := seen[task.id]; ok {
			continue
		}
		seen[task.id] = struct{}{}
		ids = append(ids, task.id)
	}
	return ids
}

func (s *Syncer) buildCodeReuseCache(ctx context.Context, tasks []codeTask, states map[codeindex.AnchorID]codeindex.ItemEmbeddingState, force bool) (*codeReuseCache, error) {
	cache := newCodeReuseCache()
	hashes := collectCodeReuseCandidateHashes(tasks, states, force)
	rows, err := s.prefetchEmbeddingHashes(ctx, hashes)
	if err != nil {
		return nil, err
	}
	cache.warm(rows, hashes)
	return cache, nil
}

func (s *Syncer) prefetchEmbeddingHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error) {
	unique := dedupeNonEmptyStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(unique))
	if len(unique) == 0 {
		return out, nil
	}
	batcher, ok := s.Index.(codeEmbeddingBatchSource)
	if !ok {
		for _, hash := range unique {
			started := time.Now()
			cached, hit, err := s.Index.EmbeddingByHash(ctx, hash)
			indexingperf.ObserveLatency(ctx, indexingperf.CodeembedHashPrefetch, time.Since(started))
			indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchCalls, 1)
			indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchHashes, 1)
			indexingperf.ObserveSample(ctx, indexingperf.CodeembedHashPrefetchBatch, 1)
			if err != nil {
				return nil, err
			}
			if hit && len(cached) > 0 {
				out[hash] = cached
				indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchHits, 1)
			} else {
				indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchMisses, 1)
			}
		}
		return out, nil
	}

	started := time.Now()
	rows, err := batcher.EmbeddingByHashes(ctx, unique)
	indexingperf.ObserveLatency(ctx, indexingperf.CodeembedHashPrefetch, time.Since(started))
	indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchCalls, 1)
	indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchHashes, int64(len(unique)))
	indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchHits, int64(len(rows)))
	indexingperf.ObserveSample(ctx, indexingperf.CodeembedHashPrefetchBatch, int64(len(unique)))
	if err != nil {
		return nil, err
	}
	for hash, vec := range rows {
		if len(vec) == 0 {
			continue
		}
		out[hash] = vec
	}
	indexingperf.AddCount(ctx, indexingperf.CodeembedHashPrefetchMisses, int64(len(unique)-len(out)))
	return out, nil
}

func collectCodeReuseCandidateHashes(tasks []codeTask, states map[codeindex.AnchorID]codeindex.ItemEmbeddingState, force bool) []string {
	hashes := make([]string, 0, len(tasks))
	for _, task := range tasks {
		state := states[task.id]
		chunkHashes := state.ChunkHashes
		if chunkHashes == nil || force {
			chunkHashes = map[int]string{}
		}
		if len(task.payload.chunks) == 0 {
			continue
		}
		itemHash := task.payload.chunks[0].Input.Hash
		if strings.TrimSpace(itemHash) != "" {
			hashes = append(hashes, itemHash)
		}
		for _, chunk := range task.payload.chunks {
			if !force && chunkHashes[chunk.Input.Index] == chunk.Input.Hash {
				continue
			}
			hashes = append(hashes, chunk.Input.Hash)
		}
	}
	return dedupeNonEmptyStrings(hashes)
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sameAnchorSet(existing []codeindex.Item, anchorIDs []codeindex.AnchorID) bool {
	if len(existing) != len(anchorIDs) {
		return false
	}
	have := make(map[codeindex.AnchorID]struct{}, len(existing))
	for _, item := range existing {
		if item.AnchorID == "" {
			return false
		}
		have[item.AnchorID] = struct{}{}
	}
	for _, id := range anchorIDs {
		if id == "" {
			return false
		}
		if _, ok := have[id]; !ok {
			return false
		}
	}
	return true
}

func (s *Syncer) needsEmbedding(ctx context.Context, task codeTask, state codeindex.ItemEmbeddingState) bool {
	observeReason := func(reason string) {
		if reason != "" {
			indexingperf.AddCount(ctx, indexingperf.CodeEmbeddingNeed.Select(reason), 1)
		}
	}
	if len(task.payload.chunks) == 0 {
		if len(state.ChunkHashes) > 0 {
			if task.payload.ownerKind != "" {
				indexingperf.AddCount(ctx, indexingperf.PrunedStaleChunks.Select(sanitizeMetricPart(task.payload.ownerKind)), int64(len(state.ChunkHashes)))
			}
			observeReason("pruned_all_chunks")
			return true
		}
		observeReason("no_chunks")
		return false
	}

	chunkHashes := state.ChunkHashes
	if chunkHashes == nil {
		chunkHashes = make(map[int]string)
	}
	switch {
	case !state.HasFingerprint:
		indexingperf.AddCount(ctx, indexingperf.CodeembedStateMissingFingerprint, 1)
	case state.Fingerprint != task.fingerprint:
		indexingperf.AddCount(ctx, indexingperf.CodeembedStateFingerprintMismatch, 1)
		indexingperf.AddCount(ctx, indexingperf.CodeembedStateSchemaOrFingerprintMismatch, 1)
	}
	if !state.HasItemHash {
		indexingperf.AddCount(ctx, indexingperf.CodeembedStateMissingItemHash, 1)
	}
	if !state.HasFingerprint && !state.HasItemHash && len(chunkHashes) == 0 {
		indexingperf.AddCount(ctx, indexingperf.CodeembedStateFirstBuildCandidate, 1)
	}

	// Fast-path: fingerprint unchanged and embeddings exist for all chunk indices.
	if state.HasFingerprint && state.Fingerprint == task.fingerprint && state.HasItemHash && len(chunkHashes) > 0 {
		allMatch := true
		expectedIndexes := make(map[int]struct{}, len(task.payload.chunks))
		for _, chunk := range task.payload.chunks {
			expectedIndexes[chunk.Input.Index] = struct{}{}
			if chunkHashes[chunk.Input.Index] != chunk.Input.Hash {
				allMatch = false
				break
			}
		}
		if allMatch && len(chunkHashes) != len(expectedIndexes) {
			allMatch = false
		}
		if allMatch {
			indexingperf.AddCount(ctx, indexingperf.CodeembedReuseSamePosition, int64(len(task.payload.chunks)))
			observeReason("unchanged")
			return false
		}
		observeReason("chunk_hash_mismatch")
		return true
	}

	// Hash comparison fallback.
	hash := task.payload.chunks[0].Input.Hash
	if !state.HasItemHash {
		if state.HasFingerprint {
			observeReason("legacy_missing_item_hash")
		} else {
			observeReason("first_build_missing_state")
		}
		observeReason("missing_item_hash")
		return true
	}
	if state.ItemHash != hash {
		observeReason("item_hash_mismatch")
		return true
	}

	for _, chunk := range task.payload.chunks {
		if chunkHashes[chunk.Input.Index] != chunk.Input.Hash {
			observeReason("chunk_hash_mismatch")
			return true
		}
	}
	if len(chunkHashes) != len(task.payload.chunks) {
		observeReason("chunk_hash_mismatch")
		return true
	}
	indexingperf.AddCount(ctx, indexingperf.CodeembedReuseSamePosition, int64(len(task.payload.chunks)))
	observeReason("unchanged_hash_fallback")
	return false
}
