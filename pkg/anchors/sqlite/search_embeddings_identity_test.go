package sqlite

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSearchEmbeddingsExcludesReusedUnembeddedChunkRows(t *testing.T) {
	for _, familyOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner replacement", true: "family replacement"}[familyOnly], func(t *testing.T) {
			ctx := context.Background()
			store, oldChunks := newEmbeddingIdentityStore(t, ctx)
			var oldRowID int64
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_chunks WHERE chunk_id = ?`, oldChunks[0].ChunkID).Scan(&oldRowID))
			newChunks := make([]codeanchor.IntelChunk, len(oldChunks))
			newIDs := make([]string, len(oldChunks))
			for i, old := range oldChunks {
				newChunks[i] = old
				newChunks[i].ChunkID = fmt.Sprintf("replacement-%02d", i)
				newChunks[i].ContentHash = "new content"
				newIDs[i] = newChunks[i].ChunkID
			}
			if familyOnly {
				require.NoError(t, store.ReplaceIntelChunksByFamily(ctx, []string{"replaced-owner"}, codeanchor.IntelChunkFamilyAuthoredSection, newChunks))
			} else {
				require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"replaced-owner"}, newChunks))
			}
			var newRowID int64
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_chunks WHERE chunk_id = ?`, newIDs[0]).Scan(&newRowID))
			require.Equal(t, oldRowID, newRowID, "the replacement must reuse a retained vector's integer identity")
			current, err := store.EmbeddingsByChunkIDs(ctx, newIDs)
			require.NoError(t, err)
			require.Empty(t, current, "replacement chunks have never received embeddings")
			requireOnlyCurrentDimensionHit(t, ctx, store)
		})
	}
}

func TestSearchEmbeddingsExcludesSupersededDimensions(t *testing.T) {
	ctx := context.Background()
	store, chunks := newEmbeddingIdentityStore(t, ctx)
	updated := make(map[string]embeddingstypes.Embedding, len(chunks))
	ids := make([]string, len(chunks))
	for i, chunk := range chunks {
		updated[chunk.ChunkID] = embeddingstypes.Embedding{1, 0, 0, 0}
		ids[i] = chunk.ChunkID
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, updated))
	current, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, updated, current, "the canonical embeddings now belong to the new dimension")
	requireOnlyCurrentDimensionHit(t, ctx, store)

	// The replacement vectors remain searchable in their current dimension.
	got, skipped, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, []string{chunks[0].ChunkID}, scoredChunkIDs(got))
	require.Equal(t, float64(1), got[0].Score)
}

func newEmbeddingIdentityStore(t *testing.T, ctx context.Context) (*Store, []codeanchor.IntelChunk) {
	t.Helper()
	store, err := Open(currentSchemaTestDBPath(t, "vector-identity.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "live-owner", Lang: codeanchor.LangGo, Kind: "function", Path: "src/fixture.go", Symbol: "Live", FQN: "fixture.Live", Fingerprint: "live"},
		{AnchorID: "replaced-owner", Lang: codeanchor.LangGo, Kind: "function", Path: "src/fixture.go", Symbol: "Replaced", FQN: "fixture.Replaced", Fingerprint: "replaced"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/fixture.go", anchors, nil, nil))
	// The live chunk is inserted first so replacement reuses the later row IDs.
	live := codeanchor.IntelChunk{ChunkID: "live-chunk", OwnerID: "live-owner", OwnerType: "anchor", Granularity: "symbol", ContentHash: "live"}
	chunks := make([]codeanchor.IntelChunk, 40)
	vectors := map[string]embeddingstypes.Embedding{live.ChunkID: {0, 1}}
	for i := range chunks {
		id := fmt.Sprintf("old-chunk-%02d", i)
		chunks[i] = codeanchor.IntelChunk{ChunkID: id, OwnerID: "replaced-owner", OwnerType: "anchor", ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection, Ord: i, Granularity: "section", ContentHash: id}
		vectors[id] = embeddingstypes.Embedding{1, 0}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"live-owner", "replaced-owner"}, append([]codeanchor.IntelChunk{live}, chunks...)))
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
	return store, chunks
}

func requireOnlyCurrentDimensionHit(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	// Forty nearer stale vectors exceed the K1 initial probe of 33. Filtering
	// after KNN would lose this valid, less similar hit behind them.
	for _, scope := range []struct {
		name    string
		filters EmbeddingSearchFilters
	}{
		{name: "unfiltered"},
		{name: "owner partition", filters: EmbeddingSearchFilters{OwnerTypes: []string{"anchor"}}},
		{name: "path", filters: EmbeddingSearchFilters{PathPrefixes: []string{"src"}}},
		{name: "owner and path", filters: EmbeddingSearchFilters{OwnerTypes: []string{"anchor"}, PathPrefixes: []string{"src"}}},
	} {
		for _, k := range []int{1, sqliteVecMaxK} {
			t.Run(fmt.Sprintf("%s/k%d", scope.name, k), func(t *testing.T) {
				got, skipped, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0}, k, scope.filters)
				require.NoError(t, err)
				require.Zero(t, skipped)
				require.Equal(t, []string{"live-chunk"}, scoredChunkIDs(got))
				require.Equal(t, "live-owner", got[0].OwnerID)
				require.Equal(t, "src/fixture.go", got[0].Path)
				require.Equal(t, float64(0), got[0].Score)
			})
		}
	}
}
