package embedindex

import (
	"context"
	"errors"
	"fmt"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/store"
)

// OntologyDerivedPlan seals projected chunks, provider text and cleanup before
// releasing the writer lease. ComputeDerived only contacts the provider.
type OntologyDerivedPlan struct {
	owners        []string
	chunks        []codeanchor.IntelChunk
	chunksChanged bool
	ids           []string
	texts         []string
	cached        map[string]embeddings.Embedding
	textHashes    map[string]string
	states        []codeanchor.IntelOntologyNodeEmbeddingState
}
type OntologyDerivedResult struct {
	plan    OntologyDerivedPlan
	vectors []embeddings.Embedding
}

// HasVectors reports whether the result holds provider vectors to cache.
func (r OntologyDerivedResult) HasVectors() bool { return len(r.vectors) > 0 }

// ontologyEmbedSliceTexts bounds how much provider work one failure discards
// when the shared embedding node owns batching.
const ontologyEmbedSliceTexts = 256

// ComputeDerived embeds the plan's texts in independent slices. When a slice
// fails it returns the error with a result holding only the vectors that
// succeeded: callers cache that result (never publish it) so a retry sends only
// the failed texts.
func (s OntologyNodeSyncer) ComputeDerived(ctx context.Context, plan OntologyDerivedPlan) (OntologyDerivedResult, error) {
	ctx = indexingperf.WithPhase(ctx, "embed_ontology_nodes")
	vectors := make([]embeddings.Embedding, len(plan.texts))
	sliceTexts := ontologyEmbedSliceTexts
	embed := func(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
		return s.EmbeddingNode.Submit(ctx, "embed_ontology_nodes", texts)
	}
	var sem chan struct{}
	if s.EmbeddingNode == nil {
		// One physical batch per slice, at most MaxConcurrent at once: the
		// same provider concurrency as a single embedTextsWithProvider call.
		batchSize := EffectiveBatchSize(s.Provider, s.BatchSize)
		sliceTexts = batchSize
		sem = make(chan struct{}, EffectiveMaxConcurrent(s.Provider, s.MaxConcurrent))
		embed = func(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
			return embedTextsWithProvider(ctx, s.Provider, texts, batchSize, 1, s.EmbedGate)
		}
	}
	errs := make([]error, (len(plan.texts)+sliceTexts-1)/sliceTexts)
	// After a failure, slices that have not started are skipped; slices
	// already at the provider finish so their vectors are kept.
	failed := make(chan struct{})
	var failOnce sync.Once
	var wg sync.WaitGroup
	for i := range errs {
		start := i * sliceTexts
		end := min(start+sliceTexts, len(plan.texts))
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sem != nil {
				select {
				case <-ctx.Done():
					errs[i] = ctx.Err()
					return
				case <-failed:
					return
				case sem <- struct{}{}:
				}
				defer func() { <-sem }()
			}
			got, err := embed(ctx, plan.texts[start:end])
			if err == nil && len(got) != end-start {
				err = fmt.Errorf("embed: unexpected vector count %d for %d texts", len(got), end-start)
			}
			if err != nil {
				errs[i] = err
				failOnce.Do(func() { close(failed) })
				return
			}
			copy(vectors[start:end], got)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		partial := OntologyDerivedResult{plan: OntologyDerivedPlan{textHashes: plan.textHashes}}
		for i, vector := range vectors {
			if len(vector) > 0 {
				partial.plan.ids = append(partial.plan.ids, plan.ids[i])
				partial.vectors = append(partial.vectors, vector)
			}
		}
		return partial, fmt.Errorf("embed ontology nodes: %w", err)
	}
	indexingperf.AddCount(ctx, indexingperf.OntologyNodesChunksEmbedded, int64(len(vectors)))
	indexingperf.AddCount(ctx, indexingperf.OntologyBodyChunksEmbedded, int64(len(vectors)))
	return OntologyDerivedResult{plan: plan, vectors: vectors}, nil
}

func (s OntologyNodeSyncer) PublishDerived(ctx context.Context, result OntologyDerivedResult) error {
	return runOntologyNodeWritePhase(ctx, "writeback_ontology_nodes", func(ctx context.Context) error {
		plan := result.plan
		if plan.chunksChanged {
			if err := s.Store.ReplaceIntelChunks(ctx, plan.owners, plan.chunks); err != nil {
				return err
			}
		}
		rows := make(map[string]embeddings.Embedding, len(plan.ids)+len(plan.cached))
		for id, vector := range plan.cached {
			rows[id] = vector
		}
		for i, id := range plan.ids {
			rows[id] = result.vectors[i]
		}
		if len(rows) > 0 {
			if err := s.Store.UpsertEmbeddings(ctx, rows); err != nil {
				return err
			}
		}
		if err := s.CacheDerived(ctx, result); err != nil {
			return err
		}
		if len(plan.states) > 0 {
			return s.Store.UpsertOntologyNodeEmbeddingStates(ctx, plan.states)
		}
		return nil
	})
}

// CacheDerived records the provider's vectors in the content-addressed cache.
// It is correct even when the result's ticket was superseded, so a skipped
// publication does not send the same text to the provider again.
func (s OntologyNodeSyncer) CacheDerived(ctx context.Context, result OntologyDerivedResult) error {
	if len(result.vectors) == 0 {
		return nil
	}
	entries := make([]store.OntologyEmbeddingCacheEntry, 0, len(result.vectors))
	for i, vector := range result.vectors {
		entries = append(entries, store.OntologyEmbeddingCacheEntry{Provider: s.ProviderInfo.Provider, Model: s.ProviderInfo.Model, TextHash: result.plan.textHashes[result.plan.ids[i]], Vector: vector, Format: OntologyEmbeddingTextFormat})
	}
	return s.Store.UpsertOntologyEmbeddingCache(ctx, entries)
}

// PrepareSourceSnapshots projects the same provider-current authored sources
// used by structural ontology indexing. Only Markdown attaches syntax spans;
// root-only formats retain provider title, metadata and visible evidence.
func (s OntologyNodeSyncer) PrepareSourceSnapshots(ctx context.Context, sources []notemeta.NoteSourceSnapshot) (OntologyDerivedPlan, error) {
	projections := make([]*ontology.NodeProjection, 0, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return OntologyDerivedPlan{}, err
		}
		projection, err := ontology.ProjectNoteSourceSnapshot(source, s.Schema)
		if err != nil {
			return OntologyDerivedPlan{}, err
		}
		if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
			projection = fallback
		}
		if projection != nil {
			projections = append(projections, projection)
		}
	}
	return s.PrepareProjections(ctx, projections...)
}

func (s OntologyNodeSyncer) SyncSourceSnapshots(ctx context.Context, sources []notemeta.NoteSourceSnapshot) error {
	plan, err := s.PrepareSourceSnapshots(ctx, sources)
	if err != nil {
		return err
	}
	return s.embedAndPublish(ctx, plan)
}

// embedAndPublish computes and publishes plan. When either step fails, the
// vectors the provider already returned are cached on a context detached from
// cancellation, so another lane failing or a stop does not make a retry send
// that text again.
func (s OntologyNodeSyncer) embedAndPublish(ctx context.Context, plan OntologyDerivedPlan) error {
	result, err := s.ComputeDerived(ctx, plan)
	if err == nil {
		if err = s.PublishDerived(ctx, result); err == nil {
			return nil
		}
	}
	return errors.Join(err, s.CacheDerived(context.WithoutCancel(ctx), result))
}
