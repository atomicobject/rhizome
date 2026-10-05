package sqlite

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestUpsertEmbeddingsRollsBackVectorsAndGenerationAcrossBatches(t *testing.T) {
	for _, failure := range []struct {
		name, trigger string
	}{
		{"later embedding batch", `CREATE TRIGGER reject_embedding_publication BEFORE INSERT ON intel_embeddings
			WHEN NEW.chunk_id = 'chunk-050' BEGIN SELECT RAISE(ABORT, 'synthetic embedding failure'); END`},
		{"generation publication", `CREATE TRIGGER reject_embedding_publication BEFORE UPDATE ON index_metadata
			WHEN NEW.key = 'search_embeddings_generation' BEGIN SELECT RAISE(ABORT, 'synthetic embedding failure'); END`},
	} {
		t.Run(failure.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(currentSchemaTestDBPath(t, "embedding-publication.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			anchor := codeanchor.IntelAnchor{AnchorID: "owner", Lang: codeanchor.LangGo, Kind: "function", Path: "fixture.go", Symbol: "Fixture", Fingerprint: "source"}
			require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
			chunks := make([]codeanchor.IntelChunk, 51)
			ids := make([]string, len(chunks))
			initial := make(map[string]embeddingstypes.Embedding, len(chunks))
			updated := make(map[string]embeddingstypes.Embedding, len(chunks))
			for i := range chunks {
				ids[i] = fmt.Sprintf("chunk-%03d", i)
				chunks[i] = codeanchor.IntelChunk{ChunkID: ids[i], OwnerID: anchor.AnchorID, OwnerType: "anchor", Ord: i, Granularity: "symbol", ContentHash: ids[i]}
				initial[ids[i]] = embeddingstypes.Embedding{1, 0}
				updated[ids[i]] = embeddingstypes.Embedding{0, 1}
			}
			require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchor.AnchorID}, chunks))
			require.NoError(t, store.UpsertEmbeddings(ctx, initial))
			fingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			generation, exists, err := store.GetMetadata(ctx, searchEmbeddingGenerationKey)
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, "1", generation)
			_, err = store.db.ExecContext(ctx, failure.trigger)
			require.NoError(t, err)

			require.ErrorContains(t, store.UpsertEmbeddings(ctx, updated), "synthetic embedding failure")
			retained, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			require.Equal(t, initial, retained, "metadata and vec bytes must roll back together")
			afterFailure, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			require.Equal(t, fingerprint, afterFailure)
			_, err = store.db.ExecContext(ctx, `DROP TRIGGER reject_embedding_publication`)
			require.NoError(t, err)

			require.NoError(t, store.UpsertEmbeddings(ctx, updated))
			published, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			require.Equal(t, updated, published)
			generation, _, err = store.GetMetadata(ctx, searchEmbeddingGenerationKey)
			require.NoError(t, err)
			require.Equal(t, "2", generation, "one successful call publishes one generation across batches")
			afterSuccess, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			require.NotEqual(t, fingerprint, afterSuccess)
		})
	}
}
