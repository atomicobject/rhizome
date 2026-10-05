package semantic

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindexsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/indexgeneration"
	"github.com/stretchr/testify/require"
)

func TestSyncerRetriesChangedSynthesizedContentAfterProviderFailure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	index, err := codeindexsqlite.Open(filepath.Join(root, "code.db"), 8)
	require.NoError(t, err)
	t.Cleanup(func() { _ = index.Close() })
	inner := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	provider := &countingProvider{inner: inner}
	anchor := codeanchor.IntelAnchor{AnchorID: "owner", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/example.go", Symbol: "Example", FQN: "fixture.Example", Signature: "func Example()", DocComment: "Example returns a configured result.", Fingerprint: "unchanged-source", UpdatedAt: 1}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	setLabel := func(label string) {
		require.NoError(t, store.ReplaceDocLinksForPath(ctx, "docs/guide.md", []codeanchor.DocLink{{SrcType: "note", SrcPath: "docs/guide.md", DstKind: "anchor", DstID: anchor.AnchorID, Label: label}}))
	}
	setLabel("Old guide")
	syncer := Syncer{Index: index, Provider: provider, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}, Intel: store, ChunkWriter: store, EmbeddingWriter: store}
	require.NoError(t, syncer.Sync(ctx))
	chunks, err := store.IntelChunksByOwners(ctx, []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	initial := chunks[0]
	ids := []string{initial.ChunkID}
	oldVectors, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Len(t, oldVectors, 1)
	oldFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	oldGeneration, err := indexgeneration.Current(ctx, store, nil, index)
	require.NoError(t, err)
	provider.Reset()
	require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
	require.Zero(t, provider.calls.Load(), "unchanged synthesized content must reuse its embedding")
	unchangedFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, oldFingerprint, unchangedFingerprint)

	// Incoming link labels are real synthesis inputs without replacing the code owner.
	setLabel("New guidance changes the meaning of the linked source")
	failing := &failFirstBatchProvider{inner: inner}
	syncer.Provider = failing
	require.ErrorContains(t, syncer.SyncPaths(ctx, []string{anchor.Path}), "boom")
	require.Equal(t, int64(1), failing.calls.Load())
	changed, err := store.IntelChunksByOwners(ctx, []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Len(t, changed, 1)
	require.Equal(t, initial.ChunkID, changed[0].ChunkID)
	require.NotEqual(t, initial.ContentHash, changed[0].ContentHash)
	states, err := store.IntelChunkEmbeddingStates(ctx, []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Empty(t, states[anchor.AnchorID], "failed provider work cannot authorize the changed hash")
	pending, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Empty(t, pending)
	hits, _, err := store.SearchEmbeddings(ctx, oldVectors[initial.ChunkID], 1, semdb.EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Empty(t, hits)
	pendingFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, oldFingerprint, pendingFingerprint)
	pendingGeneration, err := indexgeneration.Current(ctx, store, nil, index)
	require.NoError(t, err)
	require.NotEqual(t, oldGeneration, pendingGeneration, "search continuation must invalidate before a provider succeeds")

	plan, err := syncer.PlanPaths(ctx, []string{anchor.Path})
	require.NoError(t, err)
	require.Equal(t, 1, plan.TotalWork)
	require.Zero(t, plan.CacheHits)
	require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, plan))
	require.Equal(t, int64(2), failing.calls.Load(), "the retry must request a current vector")
	retried, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.NotEqual(t, oldVectors, retried)

	// A clean public Syncer over identical current source is an independent oracle.
	fresh, err := semdb.Open(filepath.Join(root, "fresh-intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = fresh.Close() })
	require.NoError(t, fresh.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	links, err := store.DocLinksForAnchors(ctx, []string{anchor.AnchorID}, 10)
	require.NoError(t, err)
	require.NoError(t, fresh.ReplaceDocLinksForPath(ctx, "docs/guide.md", links[anchor.AnchorID]))
	freshIndex, err := codeindexsqlite.Open(filepath.Join(root, "fresh-code.db"), 8)
	require.NoError(t, err)
	t.Cleanup(func() { _ = freshIndex.Close() })
	freshSyncer := Syncer{Index: freshIndex, Provider: inner, ProviderInfo: syncer.ProviderInfo, Intel: fresh, ChunkWriter: fresh, EmbeddingWriter: fresh}
	require.NoError(t, freshSyncer.Sync(ctx))
	want, err := fresh.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, want, retried)

	setLabel("Old guide")
	plan, err = syncer.PlanPaths(ctx, []string{anchor.Path})
	require.NoError(t, err)
	require.Zero(t, plan.TotalWork)
	require.Equal(t, 1, plan.CacheHits)
	require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, plan))
	require.Equal(t, int64(2), failing.calls.Load(), "reversion must restore the hash-addressed cached vector")
	reverted, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, oldVectors, reverted)
	revertedFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
	require.Equal(t, int64(2), failing.calls.Load())
	finalFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, revertedFingerprint, finalFingerprint)
}
