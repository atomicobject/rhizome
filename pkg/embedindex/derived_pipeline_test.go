package embedindex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNoteDerivedMetricsMeasureSinkFailures(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(t.Context(), collector), "embed_notes")
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	syncer := NoteSyncer{Provider: provider}
	plan := NotePlan{
		tasks: []noteTask{{id: "notes/test.md", payload: noteTaskPayload{
			embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(0, "new content", "Test", "Test")},
			embedTexts:  []string{"new content"},
		}}},
		TotalWork: 1,
	}
	want := errors.New("synthetic sink failure")
	err := syncer.ComputeDerived(ctx, plan, func(context.Context, NoteDerivedResult) error {
		// Keep the synthetic sink above coarse Windows clock resolution.
		time.Sleep(25 * time.Millisecond)
		return want
	})
	require.ErrorIs(t, err, want)
	require.Contains(t, collector.RenderSummary(), "finalize_cum=")
}

func TestDerivedFinalizersAreBoundedByProviderConcurrency(t *testing.T) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	syncer := NoteSyncer{Provider: provider, MaxConcurrent: 2}
	plan := NotePlan{TotalWork: 64}
	for i := range 64 {
		text := fmt.Sprintf("content %d", i)
		plan.tasks = append(plan.tasks, noteTask{id: embeddings.NoteID(fmt.Sprintf("notes/%d.md", i)), payload: noteTaskPayload{
			embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(0, text, "T", "T")},
			embedTexts:  []string{text},
		}})
	}
	var active, peak, seen atomic.Int64
	require.NoError(t, syncer.ComputeDerived(t.Context(), plan, func(context.Context, NoteDerivedResult) error {
		n := active.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		// A slow sink must stall the pipeline rather than accumulate owners.
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		seen.Add(1)
		return nil
	}))
	require.EqualValues(t, 64, seen.Load())
	require.LessOrEqual(t, peak.Load(), int64(2))
}

func TestOntologyDerivedComputeDoesNotPublishChunksOrVectors(t *testing.T) {
	ctx := context.Background()
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	s := OntologyNodeSyncer{Store: store, Provider: embeddings.NewDeterministicProvider(info), ProviderInfo: info, Schema: schema}
	plan, err := s.PrepareProjections(ctx, projection)
	require.NoError(t, err)
	require.NotEmpty(t, plan.texts)
	result, err := s.ComputeDerived(ctx, plan)
	require.NoError(t, err)
	chunks, err := store.IntelChunksByOwners(ctx, plan.owners)
	require.NoError(t, err)
	require.Empty(t, chunks)
	vecs, err := store.EmbeddingsByChunkIDs(ctx, plan.ids)
	require.NoError(t, err)
	require.Empty(t, vecs)
	require.NoError(t, s.PublishDerived(ctx, result))
	chunks, err = store.IntelChunksByOwners(ctx, plan.owners)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	vecs, err = store.EmbeddingsByChunkIDs(ctx, plan.ids)
	require.NoError(t, err)
	require.Len(t, vecs, len(plan.ids))
}
