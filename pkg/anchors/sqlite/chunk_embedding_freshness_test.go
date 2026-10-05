package sqlite

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestReplaceIntelChunksRetiresOnlyChangedEmbeddingEvidence(t *testing.T) {
	for _, item := range []struct {
		owner, chunk string
		familyOnly   bool
	}{
		{owner: "anchor-a", chunk: "chunk-a"},
		{owner: "section-doc", chunk: "chunk-doc", familyOnly: true},
		{owner: "node-spec", chunk: "chunk-node"},
	} {
		t.Run(item.owner, func(t *testing.T) {
			ctx := context.Background()
			store := newEmbeddingParityStore(t, ctx)
			state := seedFreshnessSidecar(t, ctx, store)
			ids := []string{"chunk-a", "chunk-b", "chunk-doc", "chunk-node"}
			if item.familyOnly {
				sibling := codeanchor.IntelChunk{ChunkID: "other-family", OwnerID: item.owner, OwnerType: "doc_section", ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection, Ord: 1, Granularity: "section", ContentHash: "sibling"}
				require.NoError(t, store.ReplaceIntelChunksByFamily(ctx, []string{item.owner}, sibling.ChunkFamily, []codeanchor.IntelChunk{sibling}))
				require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{sibling.ChunkID: {0, 1, 0, 0}}))
				ids = append(ids, sibling.ChunkID)
			}
			allChunks, err := store.IntelChunksByOwners(ctx, []string{item.owner})
			require.NoError(t, err)
			var original codeanchor.IntelChunk
			for _, chunk := range allChunks {
				if chunk.ChunkID == item.chunk {
					original = chunk
				}
			}
			require.Equal(t, item.chunk, original.ChunkID)
			replace := func(chunks []codeanchor.IntelChunk) error {
				if item.familyOnly {
					return store.ReplaceIntelChunksByFamily(ctx, []string{item.owner}, codeanchor.IntelChunkFamilyDefault, chunks)
				}
				return store.ReplaceIntelChunks(ctx, []string{item.owner}, chunks)
			}
			before, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			fingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			transient := original
			transient.ContentHash = "superseded duplicate"
			transient.Ord += 1000
			unchanged := original
			unchanged.Heading, unchanged.UpdatedAt = "presentation changed", 42
			require.NoError(t, replace([]codeanchor.IntelChunk{transient, unchanged}))
			retained, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			require.Equal(t, before, retained, "last duplicate and equal content hashes preserve embedding evidence")
			unchangedFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			require.Equal(t, fingerprint, unchangedFingerprint)

			changed := unchanged
			changed.ContentHash += "-changed"
			require.NoError(t, replace([]codeanchor.IntelChunk{changed}))
			delete(before, item.chunk)
			after, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			require.Equal(t, before, after, "other owners and chunk families retain their vectors")
			sidecars, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, []string{state.ChunkID})
			require.NoError(t, err)
			if item.chunk == state.ChunkID {
				require.Empty(t, sidecars)
			} else {
				require.Equal(t, state, sidecars[state.ChunkID])
			}
			changedFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			require.NotEqual(t, fingerprint, changedFingerprint)
			hits, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 25, EmbeddingSearchFilters{})
			require.NoError(t, err)
			require.NotContains(t, scoredChunkIDs(hits), item.chunk)
			require.Len(t, hits, len(before))
			require.NoError(t, replace([]codeanchor.IntelChunk{changed}))
			repeatedFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			require.Equal(t, changedFingerprint, repeatedFingerprint)
		})
	}
}

func TestReplaceIntelChunksChangedContentRollbackRestoresEmbeddingEvidence(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)
	state := seedFreshnessSidecar(t, ctx, store)
	chunks, err := store.IntelChunksByOwners(ctx, []string{state.NodeID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	before, err := store.EmbeddingsByChunkIDs(ctx, []string{state.ChunkID})
	require.NoError(t, err)
	fingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	changed := chunks[0]
	changed.ContentHash = "changed"
	invalid := changed
	invalid.ChunkID, invalid.Ord, invalid.StartByte = "late-failure", 1, -1
	// The second upsert fails a real SQL constraint after retirement and the
	// first chunk update, so all their effects must roll back together.
	require.Error(t, store.ReplaceIntelChunks(ctx, []string{state.NodeID}, []codeanchor.IntelChunk{changed, invalid}))
	afterChunks, err := store.IntelChunksByOwners(ctx, []string{state.NodeID})
	require.NoError(t, err)
	require.Equal(t, chunks, afterChunks)
	after, err := store.EmbeddingsByChunkIDs(ctx, []string{state.ChunkID})
	require.NoError(t, err)
	require.Equal(t, before, after)
	sidecars, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, []string{state.ChunkID})
	require.NoError(t, err)
	require.Equal(t, state, sidecars[state.ChunkID])
	afterFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, fingerprint, afterFingerprint)
}

func TestReplaceIntelChunksRetiresChangedHashesAcrossBatches(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "freshness-batches.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	anchor := codeanchor.IntelAnchor{AnchorID: "owner", Lang: codeanchor.LangGo, Kind: "function", Path: "fixture.go", Symbol: "Fixture", Fingerprint: "source"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	chunks := make([]codeanchor.IntelChunk, 801)
	ids := make([]string, len(chunks))
	vectors := make(map[string]embeddingstypes.Embedding, len(chunks))
	for i := range chunks {
		ids[i] = fmt.Sprintf("chunk-%04d", i)
		chunks[i] = codeanchor.IntelChunk{ChunkID: ids[i], OwnerID: anchor.AnchorID, OwnerType: "anchor", Ord: i, Granularity: "symbol", ContentHash: ids[i]}
		vectors[ids[i]] = embeddingstypes.Embedding{1, 0}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchor.AnchorID}, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
	for i := range chunks {
		if i%2 == 0 {
			chunks[i].ContentHash += "-changed"
			delete(vectors, chunks[i].ChunkID)
		}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchor.AnchorID}, chunks))
	after, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, vectors, after)
	hits, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0}, sqliteVecMaxK, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Len(t, hits, len(vectors))
	for _, hit := range hits {
		require.Contains(t, vectors, hit.ChunkID)
	}
	var physicalRows int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+intelVecTableName(2)).Scan(&physicalRows))
	require.Equal(t, len(chunks), physicalRows, "retirement leaves physical vector cleanup deferred")
}

func TestReplaceIntelChunksUsesFinalHashForRepeatedChunkIDsAcrossBatches(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("final hash changed=%t", changed), func(t *testing.T) {
			ctx := context.Background()
			store := newEmbeddingParityStore(t, ctx)
			chunks, err := store.IntelChunksByOwners(ctx, []string{"anchor-a"})
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			original := chunks[0]
			ids := []string{original.ChunkID}
			before, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			fingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			transient := original
			transient.Ord, transient.ContentHash = 1000, "superseded hash"
			incoming := []codeanchor.IntelChunk{transient}
			for i := 1; i <= 400; i++ {
				filler := original
				filler.ChunkID, filler.Ord = fmt.Sprintf("filler-%03d", i), i
				incoming = append(incoming, filler)
			}
			final := original
			if changed {
				final.ContentHash = "final changed hash"
			}
			incoming = append(incoming, final)
			// The same primary key survives natural-key deduplication at both
			// ends of the input; sequential upserts publish only its final value.
			require.NoError(t, store.ReplaceIntelChunks(ctx, []string{original.OwnerID}, incoming))
			published, err := store.IntelChunksByOwners(ctx, []string{original.OwnerID})
			require.NoError(t, err)
			require.Equal(t, final, published[0])
			after, err := store.EmbeddingsByChunkIDs(ctx, ids)
			require.NoError(t, err)
			afterFingerprint, _, err := store.SearchEmbeddingFingerprint(ctx)
			require.NoError(t, err)
			hits, _, err := store.SearchEmbeddings(ctx, before[original.ChunkID], 25, EmbeddingSearchFilters{})
			require.NoError(t, err)
			if changed {
				require.Empty(t, after)
				require.NotEqual(t, fingerprint, afterFingerprint)
				require.NotContains(t, scoredChunkIDs(hits), original.ChunkID)
			} else {
				require.Equal(t, before, after)
				require.Equal(t, fingerprint, afterFingerprint)
				require.Contains(t, scoredChunkIDs(hits), original.ChunkID)
			}
		})
	}
}

func seedFreshnessSidecar(t *testing.T, ctx context.Context, store *Store) codeanchor.IntelOntologyNodeEmbeddingState {
	t.Helper()
	state := codeanchor.IntelOntologyNodeEmbeddingState{ChunkID: "chunk-node", NodeID: "node-spec", NotePath: "docs/spec.md", TypeName: "TechnicalSpec", NodeKind: "NOTE", EmbeddingSchemaSignature: "schema", NodeStructureFingerprint: "node", SourceContentHash: "source", ChunkTextHash: "node", ChunkGranularity: "node_body", Provider: "test", Model: "deterministic", UpdatedAt: 1}
	require.NoError(t, store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{state}))
	return state
}
