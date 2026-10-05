package semantic

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
)

func TestDerivedCodeMetricsRetainReuseAndActualProviderWork(t *testing.T) {
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(t.Context(), c), "embed_code")
	provider := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
	cache := newCodeReuseCache()
	cache.put("cached", embeddings.Embedding{1, 2, 3, 4, 5, 6, 7, 8})
	cache.noteMiss("new")
	const id codeindex.AnchorID = "synthetic-code"
	plan := SyncPlan{
		tasks: []codeTask{{id: id, payload: codeTaskPayload{ownerKind: "function", chunks: []SemanticChunk{
			{Input: codeindex.ChunkInput{Index: 1, Hash: "stable", Granularity: "symbol"}, Text: "stable text"},
			{Input: codeindex.ChunkInput{Index: 2, Hash: "cached", Granularity: "symbol"}, Text: "cached text"},
			{Input: codeindex.ChunkInput{Index: 3, Hash: "new", Granularity: "symbol"}, Text: "new text"},
		}}}},
		state:     codePlanState{states: map[codeindex.AnchorID]codeindex.ItemEmbeddingState{id: {ChunkHashes: map[int]string{1: "stable"}}}, reuseCache: cache},
		TotalWork: 1,
	}
	syncer := Syncer{Provider: provider}
	var result CodeDerivedResult
	require.NoError(t, syncer.ComputeDerived(ctx, plan, func(_ context.Context, next CodeDerivedResult) error {
		result = next
		return nil
	}))
	require.Len(t, result.vectors, 1)
	require.Len(t, result.prepared.reuseVecs, 1)
	require.Equal(t, int64(1), provider.calls.Load())
	for name, count := range map[string]int64{
		"codeembed.reuse.same_position": 1,
		"codeembed.reuse.content_hash":  1,
		"codeembed.plan.embed_chunks":   1,
		"semantic.code_chunks":          3,
		"provider.calls":                1,
		"provider.texts":                1,
	} {
		if name == "semantic.code_chunks" {
			name += ".kind.function"
		}
		require.Equal(t, count, semanticPhaseMetricCount(c, "embed_code", name), name)
	}
}

func TestDerivedNoteMetricsRetainReuseAndActualProviderWork(t *testing.T) {
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(t.Context(), c), "embed_notes")
	provider := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
	plan := NotePlan{
		tasks: []noteTask{{id: "synthetic-note.md", payload: noteTaskPayload{
			reuseChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(1, "reused text", "Test", "Existing")},
			reuseVecs:   []embeddings.Embedding{{1, 2, 3, 4, 5, 6, 7, 8}},
			embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(2, "new text", "Test", "New")},
			embedTexts:  []string{"new text"},
		}}},
		TotalWork: 1,
	}
	syncer := NoteSyncer{Provider: provider}
	var result NoteDerivedResult
	require.NoError(t, syncer.ComputeDerived(ctx, plan, func(_ context.Context, next NoteDerivedResult) error {
		result = next
		return nil
	}))
	require.Len(t, result.vectors, 1)
	require.Len(t, result.task.payload.reuseVecs, 1)
	require.Equal(t, int64(1), provider.calls.Load())
	require.Equal(t, int64(1), semanticPhaseMetricCount(c, "embed_notes", "noteembed.reuse_hits"))
	require.Equal(t, int64(1), semanticPhaseMetricCount(c, "embed_notes", "provider.calls"))
	require.Equal(t, int64(1), semanticPhaseMetricCount(c, "embed_notes", "provider.texts"))
}

func TestOntologyDerivedMetricsSeparatePlanningComputationAndPublication(t *testing.T) {
	for _, route := range []string{"derived", "shared", "legacy"} {
		t.Run(route, func(t *testing.T) {
			c := indexingperf.NewBounded()
			ctx := indexingperf.WithPhase(indexingperf.WithCollector(t.Context(), c), "plan_ontology")
			syncer, store, provider, projection := ontologyDiagnosticFixture(t, ctx)
			if route == "shared" {
				syncer.EmbeddingNode = NewSharedEmbeddingNode(ctx, provider, 1, nil, EmbedPackerOptions{})
				defer syncer.EmbeddingNode.Close()
			}
			if route == "legacy" {
				require.NoError(t, syncer.SyncProjections(ctx, projection))
			} else {
				plan, err := syncer.PrepareProjections(ctx, projection)
				require.NoError(t, err)
				require.NotEmpty(t, plan.texts)
				require.Zero(t, semanticMetricCount(c, "ontology_nodes.chunks_embedded"), "planning is not successful provider work")
				result, err := syncer.ComputeDerived(ctx, plan)
				require.NoError(t, err)
				require.Equal(t, int64(len(result.vectors)), semanticPhaseMetricCount(c, "embed_ontology_nodes", "ontology_nodes.chunks_embedded"))
				require.Empty(t, store.writePhases, "computation cannot publish")
				require.NoError(t, syncer.PublishDerived(ctx, result))
			}
			require.Len(t, store.writePhases, 3)
			for _, phase := range store.writePhases {
				require.Equal(t, "writeback_ontology_nodes", phase)
			}
			require.Equal(t, provider.calls.Load(), semanticPhaseMetricCount(c, "embed_ontology_nodes", "provider.calls"))
			embedded := semanticPhaseMetricCount(c, "embed_ontology_nodes", "ontology_nodes.chunks_embedded")
			require.Positive(t, embedded)
			require.Equal(t, embedded, semanticPhaseMetricCount(c, "embed_ontology_nodes", "ontology_body.chunks_embedded"))
			plan, err := syncer.PrepareProjections(ctx, projection)
			require.NoError(t, err)
			require.Empty(t, plan.texts)
			require.Equal(t, embedded, semanticPhaseMetricCount(c, "plan_ontology", "ontology_nodes.chunks_reused"))
			calls := provider.calls.Load()
			_, err = syncer.ComputeDerived(ctx, plan)
			require.NoError(t, err)
			require.Equal(t, calls, provider.calls.Load(), "current vectors avoid provider work")
			require.Equal(t, embedded, semanticPhaseMetricCount(c, "embed_ontology_nodes", "ontology_nodes.chunks_embedded"))
		})
	}
}

func TestOntologyDerivedFailedProviderDoesNotCountEmbeddedChunks(t *testing.T) {
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithCollector(t.Context(), c)
	syncer, store, _, projection := ontologyDiagnosticFixture(t, ctx)
	want := errors.New("synthetic provider failure")
	syncer.Provider = diagnosticFailureProvider{err: want}
	plan, err := syncer.PrepareProjections(ctx, projection)
	require.NoError(t, err)
	require.NotEmpty(t, plan.texts)
	_, err = syncer.ComputeDerived(ctx, plan)
	require.ErrorIs(t, err, want)
	require.Zero(t, semanticMetricCount(c, "ontology_nodes.chunks_embedded"))
	require.Zero(t, semanticMetricCount(c, "ontology_body.chunks_embedded"))
	require.Equal(t, int64(1), semanticPhaseMetricCount(c, "embed_ontology_nodes", "provider.calls"))
	require.Equal(t, int64(len(plan.texts)), semanticPhaseMetricCount(c, "embed_ontology_nodes", "provider.texts"))
	require.Empty(t, store.writePhases)
}

func semanticPhaseMetricCount(c *indexingperf.Collector, phase, name string) int64 {
	for _, counter := range c.Snapshot().Counters {
		if counter.Name == name && counter.Phase == phase {
			return counter.Total
		}
	}
	return 0
}

func ontologyDiagnosticFixture(t *testing.T, ctx context.Context) (*OntologyNodeSyncer, *diagnosticOntologyStore, *countingProvider, *ontology.NodeProjection) {
	t.Helper()
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	db, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NoError(t, db.ReplaceOntologyNodeReadModel(ctx, model))
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	provider := &countingProvider{inner: embeddings.NewDeterministicProvider(info)}
	store := &diagnosticOntologyStore{OntologyNodeStore: db}
	return &OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: info, Schema: schema}, store, provider, projection
}

type diagnosticOntologyStore struct {
	OntologyNodeStore
	writePhases []string
}

func (s *diagnosticOntologyStore) ReplaceIntelChunks(ctx context.Context, owners []string, chunks []codeanchor.IntelChunk) error {
	s.writePhases = append(s.writePhases, indexingperf.PhaseFromContext(ctx))
	return s.OntologyNodeStore.ReplaceIntelChunks(ctx, owners, chunks)
}

func (s *diagnosticOntologyStore) UpsertEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	s.writePhases = append(s.writePhases, indexingperf.PhaseFromContext(ctx))
	return s.OntologyNodeStore.UpsertEmbeddings(ctx, rows)
}

func (s *diagnosticOntologyStore) UpsertOntologyNodeEmbeddingStates(ctx context.Context, rows []codeanchor.IntelOntologyNodeEmbeddingState) error {
	s.writePhases = append(s.writePhases, indexingperf.PhaseFromContext(ctx))
	return s.OntologyNodeStore.UpsertOntologyNodeEmbeddingStates(ctx, rows)
}

type diagnosticFailureProvider struct{ err error }

func (diagnosticFailureProvider) Dimensions() int { return 8 }
func (p diagnosticFailureProvider) EmbedTexts(context.Context, []string) ([]embeddings.Embedding, error) {
	return nil, p.err
}
