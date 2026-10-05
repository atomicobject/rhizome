package semantic

import (
	"context"
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// OntologyDerivedPlan seals projected chunks, provider text and cleanup before
// releasing the writer lease. ComputeDerived only contacts the provider.
type OntologyDerivedPlan struct {
	owners        []string
	chunks        []codeanchor.IntelChunk
	chunksChanged bool
	ids           []string
	texts         []string
	states        []codeanchor.IntelOntologyNodeEmbeddingState
}
type OntologyDerivedResult struct {
	plan    OntologyDerivedPlan
	vectors []embeddings.Embedding
}

func (s OntologyNodeSyncer) ComputeDerived(ctx context.Context, plan OntologyDerivedPlan) (OntologyDerivedResult, error) {
	ctx = indexingperf.WithPhase(ctx, "embed_ontology_nodes")
	var vectors []embeddings.Embedding
	var err error
	if len(plan.texts) > 0 {
		if s.EmbeddingNode != nil {
			vectors, err = s.EmbeddingNode.Submit(ctx, "embed_ontology_nodes", plan.texts)
		} else {
			vectors, err = embedTextsWithProvider(ctx, s.Provider, plan.texts, EffectiveBatchSize(s.Provider, s.BatchSize), EffectiveMaxConcurrent(s.Provider, s.MaxConcurrent), s.EmbedGate)
		}
	}
	if err != nil {
		return OntologyDerivedResult{}, fmt.Errorf("embed ontology nodes: %w", err)
	}
	indexingperf.AddCount(ctx, "ontology_nodes.chunks_embedded", int64(len(vectors)))
	indexingperf.AddCount(ctx, "ontology_body.chunks_embedded", int64(len(vectors)))
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
		rows := make(map[string]embeddings.Embedding, len(plan.ids))
		for i, id := range plan.ids {
			rows[id] = result.vectors[i]
		}
		if len(rows) > 0 {
			if err := s.Store.UpsertEmbeddings(ctx, rows); err != nil {
				return err
			}
		}
		if len(plan.states) > 0 {
			return s.Store.UpsertOntologyNodeEmbeddingStates(ctx, plan.states)
		}
		return nil
	})
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
	result, err := s.ComputeDerived(ctx, plan)
	if err != nil {
		return err
	}
	return s.PublishDerived(ctx, result)
}
