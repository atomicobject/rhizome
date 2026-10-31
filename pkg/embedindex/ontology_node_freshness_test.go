package embedindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyNodeSyncerRestoresRevertedContentAfterProviderFailure(t *testing.T) {
	ctx := context.Background()
	root, schema, original := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, original, 1)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	inner := embeddings.NewDeterministicProvider(info)
	provider := &countingProvider{inner: inner}
	syncer := OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: info, Schema: schema}
	require.NoError(t, syncer.SyncProjections(ctx, original))
	initialSet, err := BuildOntologyNodeChunks(schema, original, info, 0)
	require.NoError(t, err)
	initialHashes := make(map[string]string, len(initialSet.Chunks))
	ids := make([]string, 0, len(initialSet.Chunks))
	for _, chunk := range initialSet.Chunks {
		initialHashes[chunk.ChunkID] = chunk.ContentHash
		ids = append(ids, chunk.ChunkID)
	}
	initialVectors, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Len(t, initialVectors, len(ids))
	initialFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	provider.Reset()
	require.NoError(t, syncer.SyncProjections(ctx, original))
	require.Zero(t, provider.calls.Load())
	unchangedFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, initialFingerprint, unchangedFingerprint)

	path := filepath.Join(root, "notes", "specs", "checkout.md")
	initialSource, err := os.ReadFile(path)
	require.NoError(t, err)
	changedSource := strings.Replace(string(initialSource), "Users need quicker checkout.", "Users need accurate, audited checkout.", 1)
	require.NotEqual(t, string(initialSource), changedSource)
	require.NoError(t, os.WriteFile(path, []byte(changedSource), 0o644))
	changed, err := ontology.ProjectNote(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)
	changedSet, err := BuildOntologyNodeChunks(schema, changed, info, 0)
	require.NoError(t, err)
	var changedIDs []string
	for _, chunk := range changedSet.Chunks {
		if hash, exists := initialHashes[chunk.ChunkID]; exists && hash != chunk.ContentHash {
			changedIDs = append(changedIDs, chunk.ChunkID)
		}
	}
	require.NotEmpty(t, changedIDs, "the note edit must change an existing chunk's text hash")
	syncer.Provider = &failFirstBatchProvider{inner: inner}
	require.ErrorContains(t, syncer.SyncProjections(ctx, changed), "boom")
	pending, err := store.EmbeddingsByChunkIDs(ctx, changedIDs)
	require.NoError(t, err)
	require.Empty(t, pending)
	states, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, changedIDs)
	require.NoError(t, err)
	require.Empty(t, states, "old sidecars must not authorize a reverted chunk with no vector")
	hits, _, err := store.SearchEmbeddings(ctx, initialVectors[changedIDs[0]], 25, semdb.EmbeddingSearchFilters{})
	require.NoError(t, err)
	for _, hit := range hits {
		require.NotContains(t, changedIDs, hit.ChunkID)
	}

	// Reverting before B ever receives a vector must restore authorization for
	// A from the content-addressed cache, without sending A's text again.
	require.NoError(t, os.WriteFile(path, initialSource, 0o644))
	provider.Reset()
	syncer.Provider = provider
	require.NoError(t, syncer.SyncProjections(ctx, original))
	require.Zero(t, provider.calls.Load())
	reverted, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, initialVectors, reverted)
	revertedStates, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Len(t, revertedStates, len(ids))
	provider.Reset()
	require.NoError(t, syncer.SyncProjections(ctx, original))
	require.Zero(t, provider.calls.Load())
}

// The vector cache serves only vectors of the provider's effective size, even
// when the configuration leaves dimensions unset.
func TestOntologyNodeSyncerCacheRequiresEffectiveDimensions(t *testing.T) {
	ctx := context.Background()
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	owners := make([]string, 0, len(model.Nodes))
	for _, node := range model.Nodes {
		owners = append(owners, node.NodeID)
	}
	// Dropping chunks (and their embedding state) forces the cache lookup.
	syncFresh := func(provider *countingProvider, dims int) {
		t.Helper()
		require.NoError(t, store.ReplaceIntelChunks(ctx, owners, nil))
		info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: dims}
		require.NoError(t, OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: info, Schema: schema}.SyncProjections(ctx, projection))
	}
	small := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 4})}
	syncFresh(small, 4)
	require.Positive(t, small.calls.Load())

	// Configured dimensions are unset; the provider reports 8, so the cached
	// 4-value vectors must not be served.
	large := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8})}
	syncFresh(large, 0)
	require.Positive(t, large.calls.Load(), "a cached vector of another size must not be served")
	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(set.Chunks))
	for _, chunk := range set.Chunks {
		ids = append(ids, chunk.ChunkID)
	}
	vectors, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.NotEmpty(t, vectors)
	for _, vector := range vectors {
		require.Len(t, vector, 8)
	}

	large.Reset()
	syncFresh(large, 0)
	require.Zero(t, large.calls.Load(), "vectors of the effective size are reused")
}
