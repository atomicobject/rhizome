//go:build fts5

package store

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/stretchr/testify/require"
)

func TestOntologyEmbeddingCacheSurvivesRetirementAndMatchesExactKey(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-embedding-cache.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := seedOntologyNodeSemanticRows(t, store) // current chunk text hash "chunk-hash"
	entry := func(hash string, vector ...float32) OntologyEmbeddingCacheEntry {
		return OntologyEmbeddingCacheEntry{Provider: "test", Model: "deterministic", TextHash: hash, Vector: vector}
	}
	require.NoError(t, store.UpsertOntologyEmbeddingCache(ctx, []OntologyEmbeddingCacheEntry{
		entry("chunk-hash", 1, 0, 0), entry("gone-hash", 0, 1, 0), entry("chunk-hash", 1, 0, 0, 0),
	}))
	cached := func() map[string]embeddings.Embedding {
		t.Helper()
		got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash", "gone-hash"})
		require.NoError(t, err)
		return got
	}
	require.Equal(t, embeddings.Embedding{1, 0, 0}, cached()["chunk-hash"])
	for _, key := range [][2]any{{"other", 3}, {"test", 2}, {"test", 0}} {
		got, err := store.OntologyEmbeddingCacheVectors(ctx, key[0].(string), "deterministic", key[1].(int), []string{"chunk-hash"})
		require.NoError(t, err)
		require.Empty(t, got, "provider and exact dimensions are part of the key; an unknown size with several cached sizes matches nothing")
	}
	got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 4, []string{"chunk-hash"})
	require.NoError(t, err)
	require.Equal(t, embeddings.Embedding{1, 0, 0, 0}, got["chunk-hash"], "each size is served only its own vector")

	// Ownership-transition retirement and a full catalog rebuild delete chunk
	// and state rows; neither may delete cached vectors.
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		return deleteStaleOntologyNodesForPathsTx(ctx, tx, []string{node.NotePath}, nil, false)
	}))
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{FullReplace: true}))
	require.Equal(t, [3]int{}, ontologyNodeSemanticRowCounts(t, store))
	require.ElementsMatch(t, []string{"chunk-hash", "gone-hash"}, slices.Collect(maps.Keys(cached())))
}

// A database upgraded from v77 or earlier has vectors but no cache rows; the
// v78 step copies them so the first ownership transition reuses them.
func TestOntologyEmbeddingCacheBackfillsExistingVectorsOnUpgrade(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "ontology-embedding-cache-backfill.db")
	store, err := Open(path)
	require.NoError(t, err)
	seedOntologyNodeSemanticRows(t, store) // state: test/deterministic, "chunk-hash", vector {1, 0, 0}
	_, err = store.db.ExecContext(ctx, `UPDATE rzm_migration_state SET version = 77 WHERE domain = ?`, string(migration.DomainIntel))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash"})
	require.NoError(t, err)
	require.Equal(t, embeddings.Embedding{1, 0, 0}, got["chunk-hash"])
}

// A schema-recovery domain reset keeps an intact cache, like the note and code
// embedding caches, and recreates a malformed one empty.
func TestOntologyEmbeddingCacheSurvivesIntactDomainReset(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "ontology-embedding-cache-reset.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.UpsertOntologyEmbeddingCache(ctx, []OntologyEmbeddingCacheEntry{
		{Provider: "test", Model: "deterministic", TextHash: "kept", Vector: embeddings.Embedding{1, 0}},
	}))
	require.NoError(t, store.ResetDomain(ctx))
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 2, []string{"kept"})
	require.NoError(t, err)
	require.Equal(t, embeddings.Embedding{1, 0}, got["kept"], "an intact cache survives the reset")

	_, err = store.db.ExecContext(ctx, `DROP TABLE ontology_embedding_cache`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `CREATE TABLE ontology_embedding_cache (text_hash TEXT)`)
	require.NoError(t, err)
	require.NoError(t, store.ResetDomain(ctx))
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	got, err = store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 2, []string{"kept"})
	require.NoError(t, err)
	require.Empty(t, got, "a malformed cache is dropped and recreated")
}

// Earlier builds of this branch created the v78 cache with an extra NOT NULL
// last_used_at column, which makes every upsert fail. Opening such a database
// rebuilds and backfills only the cache, keeping the stored vectors, and the
// repaired database opens cleanly again.
func TestOntologyEmbeddingCacheRepairsInterimV78Table(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "ontology-embedding-cache-interim.db")
	store, err := Open(path)
	require.NoError(t, err)
	seedOntologyNodeSemanticRows(t, store) // state: test/deterministic, "chunk-hash", vector {1, 0, 0}
	for _, stmt := range []string{
		`DROP TABLE ontology_embedding_cache`,
		`CREATE TABLE ontology_embedding_cache (
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			text_hash TEXT NOT NULL CHECK (text_hash != ''),
			vector BLOB NOT NULL,
			last_used_at INTEGER NOT NULL,
			PRIMARY KEY (provider, model, dimensions, text_hash)
		) STRICT`,
		`UPDATE rzm_migration_validation SET schema_fingerprint = 'intel-schema-v78-2026-10-31-ontology-embedding-cache'`,
	} {
		_, err = store.db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	for range 2 {
		store, err = Open(path)
		require.NoError(t, err)
		require.NoError(t, store.UpsertOntologyEmbeddingCache(ctx, []OntologyEmbeddingCacheEntry{
			{Provider: "test", Model: "deterministic", TextHash: "new-hash", Vector: embeddings.Embedding{0, 1, 0}},
		}))
		// Unknown dimensions resolve to the single cached size.
		got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 0, []string{"chunk-hash", "new-hash"})
		require.NoError(t, err)
		require.Equal(t, map[string]embeddings.Embedding{"chunk-hash": {1, 0, 0}, "new-hash": {0, 1, 0}}, got, "the rebuilt cache is backfilled from stored vectors")
		require.NoError(t, store.Close())
	}
}

// Unreferenced vectors are pruned only once they make up minShare of the cache,
// so a few edited-away texts keep their reuse while a replaced generation goes.
func TestOntologyEmbeddingCachePrunesOnlyDominantUnreferencedRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-embedding-cache-prune.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	seedOntologyNodeSemanticRows(t, store) // current state hash "chunk-hash"
	entry := func(hash string) OntologyEmbeddingCacheEntry {
		return OntologyEmbeddingCacheEntry{Provider: "test", Model: "deterministic", TextHash: hash, Vector: embeddings.Embedding{1, 0, 0}}
	}
	require.NoError(t, store.UpsertOntologyEmbeddingCache(ctx, []OntologyEmbeddingCacheEntry{entry("chunk-hash"), entry("old-1"), entry("old-2"), entry("old-3")}))
	count := func() int {
		var n int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT count(*) FROM ontology_embedding_cache`).Scan(&n))
		return n
	}

	pruned, err := store.PruneOntologyEmbeddingCache(ctx, 0.9)
	require.NoError(t, err)
	require.Zero(t, pruned, "3 of 4 rows unreferenced is below a 0.9 share")
	require.Equal(t, 4, count())

	pruned, err = store.PruneOntologyEmbeddingCache(ctx, 0.5)
	require.NoError(t, err)
	require.Equal(t, int64(3), pruned)
	got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash"})
	require.NoError(t, err)
	require.Contains(t, got, "chunk-hash", "referenced vectors survive")
}
