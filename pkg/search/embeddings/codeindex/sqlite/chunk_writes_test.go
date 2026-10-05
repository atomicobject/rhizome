package sqlite

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
)

func TestItemChunkWritesPreserveCanonicalMetadataAndReplaceGranularity(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(t.TempDir()+"/code.db", 4)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			item := codeindex.Item{AnchorID: "example", Lang: "go", Path: "pkg/example.go", Kind: "function", Symbol: "Example", FQN: "pkg.Example", Fingerprint: "source"}
			require.NoError(t, store.UpsertItemMeta(ctx, item))
			write := func(input codeindex.ItemChunksUpsert) error {
				if batch {
					return store.UpsertItemChunksBatch(ctx, []codeindex.ItemChunksUpsert{input})
				}
				return store.UpsertItemChunks(ctx, input.AnchorID, input.Chunks, input.Texts, input.Embeddings)
			}
			input := codeindex.ItemChunksUpsert{
				AnchorID: item.AnchorID,
				Chunks:   []codeindex.ChunkInput{{Index: 1, Granularity: "body", Breadcrumb: "pkg/example.go", Heading: "Example", Hash: "old", StartLine: 2, EndLine: 3}},
				Texts:    []string{"old body"}, Embeddings: []embeddings.Embedding{{1, 0, 0, 0}},
			}
			require.NoError(t, write(input))
			input.Chunks[0].Granularity = "symbol"
			input.Chunks[0].Hash = "new"
			input.Texts[0] = "replacement body"
			input.Embeddings[0] = embeddings.Embedding{0, 1, 0, 0}
			require.NoError(t, write(input))

			chunks, err := store.ItemChunks(ctx, item.AnchorID)
			require.NoError(t, err)
			require.Equal(t, []codeindex.StoredChunk{{Index: 1, Granularity: "symbol", Breadcrumb: "pkg/example.go", Heading: "Example", Embedding: input.Embeddings[0]}}, chunks)
			hashes, err := store.ChunkHashes(ctx, item.AnchorID)
			require.NoError(t, err)
			require.Equal(t, map[int]string{1: "new"}, hashes)
			body, err := store.GetChunkBody(ctx, item.AnchorID, 1)
			require.NoError(t, err)
			require.Equal(t, "replacement body", body)
			hits, err := store.SearchChunksByText(ctx, "replacement", 10)
			require.NoError(t, err)
			require.Len(t, hits, 1)
			require.Equal(t, item.Path, hits[0].Path)
			require.Equal(t, item.Symbol, hits[0].Symbol)
			require.Equal(t, item.FQN, hits[0].FQN)
			require.Equal(t, item.Kind, hits[0].Kind)
			cached, found, err := store.EmbeddingByHash(ctx, "new")
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, input.Embeddings[0], cached)
			fingerprint, found, err := store.ItemFingerprint(ctx, item.AnchorID)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, item.Fingerprint, fingerprint)
			require.Equal(t, int64(4), store.runtime.Dimensions.Load())
		})
	}
}

func TestUpsertItemChunksBatchRollsBackEarlierItemsOnInvalidInput(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertItemMeta(ctx, codeindex.Item{AnchorID: "example", Path: "example.go"}))
	err = store.UpsertItemChunksBatch(ctx, []codeindex.ItemChunksUpsert{
		{AnchorID: "example", Chunks: []codeindex.ChunkInput{{Index: 1, Granularity: "body", Hash: "rolled-back"}}, Texts: []string{"temporary body"}, Embeddings: []embeddings.Embedding{{1, 0, 0, 0}}},
		{AnchorID: "example", Chunks: []codeindex.ChunkInput{{Index: 2}}},
	})
	require.EqualError(t, err, "chunks, texts, and embeddings length mismatch")
	chunks, err := store.ItemChunks(ctx, "example")
	require.NoError(t, err)
	require.Empty(t, chunks)
	_, found, err := store.EmbeddingByHash(ctx, "rolled-back")
	require.NoError(t, err)
	require.False(t, found)
	hits, err := store.SearchChunksByText(ctx, "temporary", 10)
	require.NoError(t, err)
	require.Empty(t, hits)
}
