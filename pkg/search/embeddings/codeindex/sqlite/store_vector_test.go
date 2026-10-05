package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
)

func TestSearchByVector_SQLiteVecChunkAndItemPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "code-vec.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "det",
		Dimensions: 4,
	}))

	chunkVecTable := chunkVecTableName(4)
	itemVecTable := itemVecTableName(4)
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, chunkVecTable).Scan(&count))
	require.Equal(t, 0, count, "metadata validation should not build vec mirrors")
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, itemVecTable).Scan(&count))
	require.Equal(t, 0, count, "metadata validation should not build vec mirrors")

	item1 := codeindex.Item{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"}
	item2 := codeindex.Item{AnchorID: "a2", Path: "lib/bar.go", Kind: "function", Symbol: "Bar", FQN: "lib.Bar"}
	require.NoError(t, store.UpsertItemMeta(ctx, item1))
	require.NoError(t, store.UpsertItemMeta(ctx, item2))

	require.NoError(t, store.UpsertItemChunks(ctx, item1.AnchorID, []codeindex.ChunkInput{
		{Index: 0, Granularity: "symbol", Breadcrumb: "pkg/foo.go", Heading: "Foo", Hash: "h1"},
	}, []string{"func Foo() {}"}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertItemChunks(ctx, item2.AnchorID, []codeindex.ChunkInput{
		{Index: 0, Granularity: "symbol", Breadcrumb: "lib/bar.go", Heading: "Bar", Hash: "h2"},
	}, []string{"func Bar() {}"}, []embeddings.Embedding{{0, 1, 0, 0}}))

	require.NoError(t, store.UpsertItemEmbedding(ctx, item1.AnchorID, "ih1", embeddings.Embedding{1, 0, 0, 0}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, item2.AnchorID, "ih2", embeddings.Embedding{0, 1, 0, 0}))

	chunks, skipped, err := store.SearchChunksByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 10)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, chunks, 2)
	require.Equal(t, codeindex.AnchorID("a1"), chunks[0].AnchorID)
	require.Greater(t, chunks[0].Score, chunks[1].Score)

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+chunkVecTable).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+itemVecTable).Scan(&count))
	require.Equal(t, 2, count)

	filtered, skipped, err := store.SearchChunksByVectorFiltered(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 10, codeindex.VectorSearchFilters{
		PathPrefixes: []string{"pkg/"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, filtered, 1)
	require.Equal(t, codeindex.AnchorID("a1"), filtered[0].AnchorID)

	items, skipped, err := store.SearchItemsByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 10)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, items, 2)
	require.Equal(t, codeindex.AnchorID("a1"), items[0].AnchorID)
	require.Greater(t, items[0].Score, items[1].Score)

	require.NoError(t, store.UpsertItemEmbedding(ctx, item1.AnchorID, "ih1-updated", embeddings.Embedding{0.9, 0.1, 0, 0}))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+itemVecTable).Scan(&count))
	require.Equal(t, 2, count)
}

func TestSearchFiltersStaleGenerationWhenLazyItemsPreserved(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "code-visible-generation.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
	current := codeindex.Item{AnchorID: "current", Path: "pkg/current.go", Kind: "function", Symbol: "FreshThing", FQN: "pkg.FreshThing"}
	stale := codeindex.Item{AnchorID: "stale", Path: "pkg/stale.go", Kind: "function", Symbol: "FreshThingStale", FQN: "pkg.FreshThingStale"}
	require.NoError(t, store.UpsertItemMeta(ctx, current))
	require.NoError(t, store.UpsertItemMeta(ctx, stale))
	require.NoError(t, store.UpsertItemChunks(ctx, current.AnchorID, []codeindex.ChunkInput{{Index: 0, Granularity: "symbol", Breadcrumb: "pkg/current.go", Heading: "FreshThing", Hash: "h-current"}}, []string{"func FreshThing() {}"}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertItemChunks(ctx, stale.AnchorID, []codeindex.ChunkInput{{Index: 0, Granularity: "symbol", Breadcrumb: "pkg/stale.go", Heading: "FreshThingStale", Hash: "h-stale"}}, []string{"func FreshThingStale() {}"}, []embeddings.Embedding{{0.95, 0.05, 0, 0}}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, current.AnchorID, "ih-current", embeddings.Embedding{1, 0, 0, 0}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, stale.AnchorID, "ih-stale", embeddings.Embedding{0.95, 0.05, 0, 0}))

	_, err = store.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.NoError(t, store.UpsertItemMeta(ctx, current))

	uncommittedChunks, _, err := store.SearchChunksByVector(ctx, embeddings.Embedding{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, uncommittedChunks, 2, "uncommitted generations must not hide the last visible index")

	pruned, err := store.PruneStaleItems(ctx, 0.9)
	require.NoError(t, err)
	require.Zero(t, pruned, "test setup should lazy-preserve the stale row")
	require.NoError(t, store.CommitSyncGeneration(ctx))

	chunks, _, err := store.SearchChunksByVector(ctx, embeddings.Embedding{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, codeindex.AnchorID("current"), chunks[0].AnchorID)

	items, _, err := store.SearchItemsByVector(ctx, embeddings.Embedding{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, codeindex.AnchorID("current"), items[0].AnchorID)

	textHits, err := store.SearchChunksByText(ctx, "FreshThing", 10)
	require.NoError(t, err)
	require.Len(t, textHits, 1)
	require.Equal(t, codeindex.AnchorID("current"), textHits[0].AnchorID)
}

func TestSearchByVector_UsesVecMirrorTablesNotRelationalBlobs(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "code-vec-source.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "det",
		Dimensions: 4,
	}))

	item1 := codeindex.Item{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"}
	item2 := codeindex.Item{AnchorID: "a2", Path: "lib/bar.go", Kind: "function", Symbol: "Bar", FQN: "lib.Bar"}
	require.NoError(t, store.UpsertItemMeta(ctx, item1))
	require.NoError(t, store.UpsertItemMeta(ctx, item2))
	require.NoError(t, store.UpsertItemChunks(ctx, item1.AnchorID, []codeindex.ChunkInput{
		{Index: 0, Granularity: "symbol", Breadcrumb: "pkg/foo.go", Heading: "Foo", Hash: "h1"},
	}, []string{"func Foo() {}"}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertItemChunks(ctx, item2.AnchorID, []codeindex.ChunkInput{
		{Index: 0, Granularity: "symbol", Breadcrumb: "lib/bar.go", Heading: "Bar", Hash: "h2"},
	}, []string{"func Bar() {}"}, []embeddings.Embedding{{0, 1, 0, 0}}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, item1.AnchorID, "ih1", embeddings.Embedding{1, 0, 0, 0}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, item2.AnchorID, "ih2", embeddings.Embedding{0, 1, 0, 0}))

	chunks, _, err := store.SearchChunksByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 2)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, codeindex.AnchorID("a1"), chunks[0].AnchorID)

	items, _, err := store.SearchItemsByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 2)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, codeindex.AnchorID("a1"), items[0].AnchorID)

	chunkVecTable := chunkVecTableName(4)
	itemVecTable := itemVecTableName(4)
	_, err = store.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_`+chunkVecTable+`_upd`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_`+itemVecTable+`_upd`)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		UPDATE `+tableChunkEmbeddings+`
		SET embedding = ?
		WHERE id = (
			SELECT c.id
			FROM `+tableChunkEmbeddings+` c
			JOIN `+tableItems+` i ON i.id = c.item_row_id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id = ? AND c.chunk_index = 0
			LIMIT 1
		)
	`, embedToBytes(embeddings.Embedding{0, 1, 0, 0}), item1.AnchorID)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		UPDATE `+tableItemEmbeddings+`
		SET embedding = ?
		WHERE item_row_id = (
			SELECT i.id
			FROM `+tableItems+` i
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id = ?
			LIMIT 1
		)
	`, embedToBytes(embeddings.Embedding{0, 1, 0, 0}), item1.AnchorID)
	require.NoError(t, err)

	chunks, _, err = store.SearchChunksByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 2)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, codeindex.AnchorID("a1"), chunks[0].AnchorID)

	items, _, err = store.SearchItemsByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 2)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, codeindex.AnchorID("a1"), items[0].AnchorID)
}

func TestSearchByVector_AfterRebuild_RecreatesVecMirrorsLazily(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "code-vec-rebuild.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "det",
		Dimensions: 4,
	}))

	item := codeindex.Item{AnchorID: "a1", Path: "pkg/foo.go", Kind: "function", Symbol: "Foo", FQN: "pkg.Foo"}
	require.NoError(t, store.UpsertItemMeta(ctx, item))
	require.NoError(t, store.UpsertItemChunks(ctx, item.AnchorID, []codeindex.ChunkInput{
		{Index: 0, Granularity: "symbol", Breadcrumb: "pkg/foo.go", Heading: "Foo", Hash: "h1"},
	}, []string{"func Foo() {}"}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, item.AnchorID, "ih1", embeddings.Embedding{1, 0, 0, 0}))

	require.NoError(t, store.rebuildDomainPreservingEmbeddings(ctx))

	chunkVecTable := chunkVecTableName(4)
	itemVecTable := itemVecTableName(4)
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, chunkVecTable).Scan(&count))
	require.Equal(t, 0, count, "rebuild should not eagerly create vec mirrors")
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, itemVecTable).Scan(&count))
	require.Equal(t, 0, count, "rebuild should not eagerly create vec mirrors")

	chunks, skipped, err := store.SearchChunksByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 1)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, chunks, 1)
	require.Equal(t, codeindex.AnchorID("a1"), chunks[0].AnchorID)

	items, skipped, err := store.SearchItemsByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 1)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, items, 1)
	require.Equal(t, codeindex.AnchorID("a1"), items[0].AnchorID)

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+chunkVecTable).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+itemVecTable).Scan(&count))
	require.Equal(t, 1, count)
}

func TestEnsureSchema_CreatesVectorFilterIndexes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "code-vec-indexes.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))

	filters := []struct{ table, expression string }{
		{tableChunkEmbeddings, "lower(granularity)"},
		{"intel_code_anchors", "(path)"},
		{"intel_code_anchors", "lower(kind)"},
	}
	for _, filter := range filters {
		rows, err := store.db.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL AND tbl_name = ?`, filter.table)
		require.NoError(t, err)
		found := false
		for rows.Next() {
			var definition string
			require.NoError(t, rows.Scan(&definition))
			found = found || strings.Contains(strings.ToLower(definition), filter.expression)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Truef(t, found, "missing index for %s on %s", filter.expression, filter.table)
	}
}
