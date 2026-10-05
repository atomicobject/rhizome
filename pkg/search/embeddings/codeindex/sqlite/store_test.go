package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

func TestStoreSearchChunksByText_AllowsFreeTextPunctuation(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 4})

	path := t.TempDir() + "/code.db"
	store, err := Open(path, prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: prov.Dimensions()}))

	item := codeindex.Item{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"}
	require.NoError(t, store.UpsertItemMeta(ctx, item))

	vecs, err := prov.EmbedTexts(ctx, []string{"func Foo() { return 1 }"})
	require.NoError(t, err)
	require.NoError(t, store.UpsertItemChunks(ctx, item.AnchorID, []codeindex.ChunkInput{{Index: 0, Granularity: "symbol", Breadcrumb: "pkg/foo.go", Heading: "Foo", Hash: "h1"}}, []string{"func Foo() { return 1 }"}, vecs))

	for _, query := range []string{"Foo", "How does Foo work?"} {
		hits, err := store.SearchChunksByText(ctx, query, 5)
		require.NoError(t, err)
		require.Len(t, hits, 1)
		require.Equal(t, codeindex.AnchorID("a1"), hits[0].AnchorID)
		require.Equal(t, 0, hits[0].ChunkIndex)
	}
}

func TestOpen_ResetsDomainOnSchemaError(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "broken-code.db")

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE VIEW code_chunk_embeddings AS SELECT 1 AS id;`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var typ string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT type FROM sqlite_master WHERE name = 'code_chunk_embeddings'`).Scan(&typ))
	require.Equal(t, "table", typ)
}

func TestDeleteChunksNotInRemovesStaleRows(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))

	item := codeindex.Item{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"}
	require.NoError(t, store.UpsertItemMeta(ctx, item))

	chunks := []codeindex.ChunkInput{
		{Index: 1, Granularity: "body", Breadcrumb: "pkg/foo.go", Heading: "Foo", Hash: "h1"},
		{Index: 2, Granularity: "body", Breadcrumb: "pkg/foo.go", Heading: "Foo", Hash: "h2"},
	}
	vecs := []embeddings.Embedding{{1, 2, 3, 4}, {5, 6, 7, 8}}
	require.NoError(t, store.UpsertItemChunks(ctx, item.AnchorID, chunks, []string{"c1", "c2"}, vecs))
	require.NoError(t, store.DeleteChunksNotIn(ctx, item.AnchorID, []int{1, 2}))

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableChunkEmbeddings+``).Scan(&count))
	require.Equal(t, 2, count)

	// Update with only one chunk; DeleteChunksNotIn should remove the other.
	require.NoError(t, store.UpsertItemChunks(ctx, item.AnchorID, chunks[:1], []string{"c1"}, vecs[:1]))
	require.NoError(t, store.DeleteChunksNotIn(ctx, item.AnchorID, []int{1}))

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableChunkEmbeddings+``).Scan(&count))
	require.Equal(t, 1, count)
}

func TestDeleteItemsForPathsNotInBatchRemovesStaleRows(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))

	items := []codeindex.Item{
		{AnchorID: "a1", Path: "pkg/a.go", Kind: "function", Symbol: "A1", FQN: "pkg.A1"},
		{AnchorID: "a2", Path: "pkg/a.go", Kind: "function", Symbol: "A2", FQN: "pkg.A2"},
		{AnchorID: "b1", Path: "pkg/b.go", Kind: "function", Symbol: "B1", FQN: "pkg.B1"},
	}
	for _, item := range items {
		require.NoError(t, store.UpsertItemMeta(ctx, item))
	}

	require.NoError(t, store.DeleteItemsForPathsNotIn(ctx, map[string][]codeindex.AnchorID{
		"pkg/a.go": {"a1"},
		"pkg/b.go": nil,
	}))

	got, err := store.ListItems(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, codeindex.AnchorID("a1"), got[0].AnchorID)
}

func TestUpsertItemMetaBatch_PreservesAnchorPathsForNewRows(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))

	require.NoError(t, store.UpsertItemMetaBatch(ctx, []codeindex.Item{
		{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"},
		{AnchorID: "a2", Path: "pkg/bar.go", Kind: "function", Symbol: "Bar", FQN: "pkg.Bar"},
	}))

	items, err := store.ListItems(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "pkg/foo.go", items[0].Path)
	require.Equal(t, "pkg/bar.go", items[1].Path)
}

func TestUpsertItemEmbeddingBatch_PersistsEmbeddingsForResolvedRows(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
	require.NoError(t, store.UpsertItemMetaBatch(ctx, []codeindex.Item{
		{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"},
		{AnchorID: "a2", Path: "pkg/bar.go", Kind: "function", Symbol: "Bar", FQN: "pkg.Bar"},
	}))

	require.NoError(t, store.UpsertItemEmbeddingBatch(ctx, []codeindex.ItemEmbeddingUpsert{
		{AnchorID: "a1", Hash: "hash-a1", Embedding: embeddings.Embedding{1, 2, 3, 4}},
		{AnchorID: "a2", Hash: "hash-a2", Embedding: embeddings.Embedding{4, 3, 2, 1}},
	}))

	vec, hash, ok, err := store.GetItemEmbedding(ctx, "a1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "hash-a1", hash)
	require.Equal(t, embeddings.Embedding{1, 2, 3, 4}, vec)

	vec, hash, ok, err = store.GetItemEmbedding(ctx, "a2")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "hash-a2", hash)
	require.Equal(t, embeddings.Embedding{4, 3, 2, 1}, vec)
}

func TestUpsertItemEmbeddingBatch_BatchesCacheBelowSQLiteVariableLimit(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
	items := make([]codeindex.Item, 0, 205)
	upserts := make([]codeindex.ItemEmbeddingUpsert, 0, 205)
	for i := 0; i < 205; i++ {
		anchorID := codeindex.AnchorID(fmt.Sprintf("a-%03d", i))
		items = append(items, codeindex.Item{AnchorID: anchorID, Path: fmt.Sprintf("pkg/%03d.go", i), Kind: "function", Symbol: "Run"})
		upserts = append(upserts, codeindex.ItemEmbeddingUpsert{
			AnchorID:  anchorID,
			Hash:      fmt.Sprintf("hash-%03d", i),
			Embedding: embeddings.Embedding{float32(i), 0, 0, 0},
		})
	}
	require.NoError(t, store.UpsertItemMetaBatch(ctx, items))

	require.NoError(t, store.UpsertItemEmbeddingBatch(ctx, upserts))
	for i, upsert := range upserts {
		vec, hash, ok, err := store.GetItemEmbedding(ctx, upsert.AnchorID)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, upsert.Hash, hash)
		require.Equal(t, embeddings.Embedding{float32(i), 0, 0, 0}, vec)
		cached, ok, err := store.EmbeddingByHash(ctx, upsert.Hash)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, vec, cached)
	}
}

func TestUpsertItemChunks_BatchesRowsBelowSQLiteVariableLimit(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
	item := codeindex.Item{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"}
	require.NoError(t, store.UpsertItemMeta(ctx, item))

	chunks := make([]codeindex.ChunkInput, 0, 75)
	texts := make([]string, 0, 75)
	vecs := make([]embeddings.Embedding, 0, 75)
	for i := 0; i < 75; i++ {
		chunks = append(chunks, codeindex.ChunkInput{
			Index:       i,
			Granularity: "body",
			Breadcrumb:  "pkg/foo.go",
			Heading:     "Foo",
			Hash:        fmt.Sprintf("chunk-hash-%03d", i),
		})
		texts = append(texts, fmt.Sprintf("chunk %03d", i))
		vecs = append(vecs, embeddings.Embedding{float32(i), 0, 0, 0})
	}

	require.NoError(t, store.UpsertItemChunks(ctx, item.AnchorID, chunks, texts, vecs))
	stored, err := store.ItemChunks(ctx, item.AnchorID)
	require.NoError(t, err)
	require.Len(t, stored, len(chunks))
	last := stored[len(stored)-1]
	require.Equal(t, 74, last.Index)
	require.Equal(t, embeddings.Embedding{74, 0, 0, 0}, last.Embedding)
	hits, err := store.SearchChunksByText(ctx, "chunk 074", 10)
	require.NoError(t, err)
	foundLast := false
	for _, hit := range hits {
		if hit.AnchorID == item.AnchorID && hit.ChunkIndex == 74 {
			foundLast = true
		}
	}
	require.True(t, foundLast, "last SQL batch must be searchable")
}

func TestStoreCacheEmbeddingExposesNamedOp(t *testing.T) {
	ctx := indexingperf.WithCollector(context.Background(), indexingperf.New())

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.CacheEmbedding(ctx, "code-hash", embeddings.Embedding{1, 2, 3, 4}))

	summary := indexingperf.FromContext(ctx).RenderSummary()
	require.Contains(t, summary, "codeemb.cache_embedding")
	require.NotContains(t, summary, "unknown")
}

func TestLazyPruning_PreservesStaleItemsBelowThreshold(t *testing.T) {
	for _, tc := range []struct {
		name            string
		current, pruned int
	}{
		{"below threshold", 8, 0},
		{"above threshold", 5, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(t.TempDir()+"/code.db", 4)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			require.NoError(t, store.EnsureSchema(ctx))
			require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
			for i := 0; i < 10; i++ {
				item := codeindex.Item{AnchorID: codeindex.AnchorID(filepath.Join("a", string(rune('0'+i)))), Path: "pkg/foo.go", Kind: "function", Symbol: "Foo"}
				require.NoError(t, store.UpsertItemMeta(ctx, item))
			}
			gen, err := store.IncrementSyncGeneration(ctx)
			require.NoError(t, err)
			require.Equal(t, int64(1), gen)
			for i := 0; i < tc.current; i++ {
				item := codeindex.Item{AnchorID: codeindex.AnchorID(filepath.Join("a", string(rune('0'+i)))), Path: "pkg/foo.go", Kind: "function", Symbol: "Foo"}
				require.NoError(t, store.UpsertItemMeta(ctx, item))
			}
			pruned, err := store.PruneStaleItems(ctx, 0.3)
			require.NoError(t, err)
			require.Equal(t, tc.pruned, pruned)
			items, err := store.ListItems(ctx)
			require.NoError(t, err)
			require.Len(t, items, 10-tc.pruned)
			got := make(map[codeindex.AnchorID]struct{}, len(items))
			for _, item := range items {
				got[item.AnchorID] = struct{}{}
			}
			for i := 0; i < 10; i++ {
				id := codeindex.AnchorID(filepath.Join("a", string(rune('0'+i))))
				if i < 10-tc.pruned {
					require.Contains(t, got, id)
				} else {
					require.NotContains(t, got, id)
				}
			}
		})
	}
}

func TestLazyPruning_BranchSwitchReusesEmbeddings(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 4})
	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))

	// Simulate feature branch: add item with embedding.
	item := codeindex.Item{AnchorID: "feature-item", Path: "feature.go", Kind: "function", Symbol: "FeatureFunc"}
	require.NoError(t, store.UpsertItemMeta(ctx, item))
	vecs, err := prov.EmbedTexts(ctx, []string{"feature function body"})
	require.NoError(t, err)
	require.NoError(t, store.CacheEmbedding(ctx, "feature-hash", vecs[0]))
	require.NoError(t, store.UpsertItemEmbedding(ctx, item.AnchorID, "feature-hash", vecs[0]))

	// Verify embedding exists.
	emb, hash, ok, err := store.GetItemEmbedding(ctx, item.AnchorID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "feature-hash", hash)
	require.Len(t, emb, 4)

	// Simulate switch to main: increment generation and don't update the feature item.
	_, err = store.IncrementSyncGeneration(ctx)
	require.NoError(t, err)

	// Prune with threshold - item is 100% stale, but we use 0 threshold to force prune.
	pruned, err := store.PruneStaleItems(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// Item is gone.
	_, _, ok, err = store.GetItemEmbedding(ctx, item.AnchorID)
	require.NoError(t, err)
	require.False(t, ok)

	// But embedding cache still has the vector!
	cached, ok, err := store.EmbeddingByHash(ctx, "feature-hash")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, cached, 4)

	// Switch back to feature branch: recreate item and reuse cached embedding.
	_, err = store.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.NoError(t, store.UpsertItemMeta(ctx, item))

	// Check cache for embedding - it's still there!
	reused, ok, err := store.EmbeddingByHash(ctx, "feature-hash")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cached, reused)
}

func TestRebuildDomainPreservingEmbeddings_PreservesCacheNorm(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "det",
		Dimensions: 4,
	}))

	cacheOnlyHash := "cache-only-hash"
	cacheOnlyVec := embeddings.Embedding{3, 4, 0, 0}
	require.NoError(t, store.CacheEmbedding(ctx, cacheOnlyHash, cacheOnlyVec))

	var beforeNorm float64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT norm
		FROM `+tableEmbeddingCache+`
		WHERE content_hash = ?
	`, cacheOnlyHash).Scan(&beforeNorm))
	require.InDelta(t, 5.0, beforeNorm, 1e-9)

	require.NoError(t, store.rebuildDomainPreservingEmbeddings(ctx))

	var afterNorm float64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT norm
		FROM `+tableEmbeddingCache+`
		WHERE content_hash = ?
	`, cacheOnlyHash).Scan(&afterNorm))
	require.InDelta(t, beforeNorm, afterNorm, 1e-9)

	gotVec, ok, err := store.EmbeddingByHash(ctx, cacheOnlyHash)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, cacheOnlyVec, gotVec)
}

func TestEmbeddingByHashesReturnsOnlyPresentUniqueRows(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "det",
		Dimensions: 4,
	}))

	vecA := embeddings.Embedding{1, 2, 3, 4}
	vecB := embeddings.Embedding{5, 6, 7, 8}
	require.NoError(t, store.CacheEmbedding(ctx, "hash-a", vecA))
	require.NoError(t, store.CacheEmbedding(ctx, "hash-b", vecB))

	rows, err := store.EmbeddingByHashes(ctx, []string{"hash-a", "hash-b", "hash-a", "", "missing"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, vecA, rows["hash-a"])
	require.Equal(t, vecB, rows["hash-b"])
}

func TestEmbeddingByHashesSkipsDimensionMismatch(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir()+"/code.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "det",
		Dimensions: 4,
	}))

	badVec := embeddings.Embedding{1, 2, 3}
	now := time.Now().Unix()
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		return cacheEmbeddingTx(ctx, tx, tableEmbeddingCache, "bad-hash", badVec, now)
	}))

	rows, err := store.EmbeddingByHashes(ctx, []string{"bad-hash"})
	require.NoError(t, err)
	require.Empty(t, rows)
}
