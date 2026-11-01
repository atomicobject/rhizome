//go:build fts5

package store

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingCacheSurvivesRetirementAndMatchesExactKey(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-embedding-cache.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := seedOntologyNodeSemanticRows(t, store) // current chunk text hash "chunk-hash"
	entry := func(hash string, vector ...float32) EmbeddingCacheEntry {
		return EmbeddingCacheEntry{Provider: "test", Model: "deterministic", TextHash: hash, Vector: vector}
	}
	require.NoError(t, store.UpsertEmbeddingCache(ctx, []EmbeddingCacheEntry{
		entry("chunk-hash", 1, 0, 0), entry("gone-hash", 0, 1, 0), entry("chunk-hash", 1, 0, 0, 0),
	}))
	cached := func() map[string]embeddings.Embedding {
		t.Helper()
		got, err := store.EmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash", "gone-hash"})
		require.NoError(t, err)
		return got
	}
	require.Equal(t, embeddings.Embedding{1, 0, 0}, cached()["chunk-hash"])
	for _, key := range [][2]any{{"other", 3}, {"test", 2}, {"test", 0}} {
		got, err := store.EmbeddingCacheVectors(ctx, key[0].(string), "deterministic", key[1].(int), []string{"chunk-hash"})
		require.NoError(t, err)
		require.Empty(t, got, "provider and exact dimensions are part of the key; an unknown size with several cached sizes matches nothing")
	}
	got, err := store.EmbeddingCacheVectors(ctx, "test", "deterministic", 4, []string{"chunk-hash"})
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
// v78 step copies them so the first ownership transition, projection bump, or
// plain-note edit reuses them.
func TestEmbeddingCacheBackfillsExistingVectorsOnUpgrade(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "ontology-embedding-cache-backfill.db")
	store, err := Open(path)
	require.NoError(t, err)
	seedOntologyNodeSemanticRows(t, store) // state: test/deterministic, "chunk-hash", vector {1, 0, 0}
	seedPlainNoteSectionVector(t, store, "note-hash", embeddings.Embedding{0, 0, 1})
	for _, stmt := range []string{
		`DROP TABLE intel_embedding_cache`,
		`UPDATE rzm_migration_state SET version = 77 WHERE domain = 'intel'`,
	} {
		_, err = store.db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	got, err := store.EmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash", "note-hash"})
	require.NoError(t, err)
	require.Equal(t, map[string]embeddings.Embedding{"chunk-hash": {1, 0, 0}, "note-hash": {0, 0, 1}}, got, "ontology and plain-note vectors are both backfilled")
}

// A schema-recovery domain reset keeps an intact cache, like the note and code
// embedding caches, and recreates a malformed one empty.
func TestEmbeddingCacheSurvivesIntactDomainReset(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "ontology-embedding-cache-reset.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.UpsertEmbeddingCache(ctx, []EmbeddingCacheEntry{
		{Provider: "test", Model: "deterministic", TextHash: "kept", Vector: embeddings.Embedding{1, 0}},
	}))
	require.NoError(t, store.ResetDomain(ctx))
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	got, err := store.EmbeddingCacheVectors(ctx, "test", "deterministic", 2, []string{"kept"})
	require.NoError(t, err)
	require.Equal(t, embeddings.Embedding{1, 0}, got["kept"], "an intact cache survives the reset")

	_, err = store.db.ExecContext(ctx, `DROP TABLE intel_embedding_cache`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `CREATE TABLE intel_embedding_cache (text_hash TEXT)`)
	require.NoError(t, err)
	require.NoError(t, store.ResetDomain(ctx))
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	got, err = store.EmbeddingCacheVectors(ctx, "test", "deterministic", 2, []string{"kept"})
	require.NoError(t, err)
	require.Empty(t, got, "a malformed cache is dropped and recreated")
}

// Earlier builds of this branch named the v78 cache ontology_embedding_cache,
// one with an extra NOT NULL last_used_at column that makes every upsert fail.
// Opening such a database moves its rows into intel_embedding_cache and
// backfills stored vectors without resetting Intel, and the repaired database
// opens cleanly again.
func TestEmbeddingCacheRepairsInterimV78Table(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "ontology-embedding-cache-interim.db")
	store, err := Open(path)
	require.NoError(t, err)
	seedOntologyNodeSemanticRows(t, store) // state: test/deterministic, "chunk-hash", vector {1, 0, 0}
	seedPlainNoteSectionVector(t, store, "note-hash", embeddings.Embedding{0, 0, 1})
	for _, stmt := range []string{
		`DROP TABLE intel_embedding_cache`,
		`CREATE TABLE ontology_embedding_cache (
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			text_hash TEXT NOT NULL CHECK (text_hash != ''),
			vector BLOB NOT NULL,
			last_used_at INTEGER NOT NULL,
			PRIMARY KEY (provider, model, dimensions, text_hash)
		) STRICT`,
		`INSERT INTO ontology_embedding_cache VALUES ('test', 'deterministic', 3, 'edited-away', X'0000803F0000000000000000', 1)`,
		`UPDATE rzm_migration_validation SET schema_fingerprint = 'intel-schema-v78-2026-10-31-ontology-embedding-cache'`,
	} {
		_, err = store.db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	for range 2 {
		store, err = Open(path)
		require.NoError(t, err)
		require.NoError(t, store.UpsertEmbeddingCache(ctx, []EmbeddingCacheEntry{
			{Provider: "test", Model: "deterministic", TextHash: "new-hash", Vector: embeddings.Embedding{0, 1, 0}},
		}))
		// Unknown dimensions resolve to the single cached size.
		got, err := store.EmbeddingCacheVectors(ctx, "test", "deterministic", 0, []string{"chunk-hash", "new-hash", "note-hash", "edited-away"})
		require.NoError(t, err)
		require.Equal(t, map[string]embeddings.Embedding{"chunk-hash": {1, 0, 0}, "new-hash": {0, 1, 0}, "note-hash": {0, 0, 1}, "edited-away": {1, 0, 0}}, got,
			"the interim table's rows move, including unreferenced history, and stored vectors are backfilled")
		legacy, err := tableExistsQuery(ctx, store.db, legacyEmbeddingCacheTable)
		require.NoError(t, err)
		require.False(t, legacy, "the interim table is dropped")
		require.NoError(t, store.Close())
	}
}

// Pruning removes only vectors an earlier format produced that no current chunk
// uses: current-format history keeps revert reuse, and a vector an earlier
// format produced but a current chunk still uses stays.
func TestEmbeddingCachePrunesOnlyUnreferencedEarlierFormats(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-embedding-cache-prune.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	seedOntologyNodeSemanticRows(t, store) // current state hash "chunk-hash"
	entry := func(hash, format string) EmbeddingCacheEntry {
		return EmbeddingCacheEntry{Provider: "test", Model: "deterministic", TextHash: hash, Vector: embeddings.Embedding{1, 0, 0}, Format: format}
	}
	seedPlainNoteSectionVector(t, store, "note-hash", embeddings.Embedding{0, 0, 1})
	require.NoError(t, store.UpsertEmbeddingCache(ctx, []EmbeddingCacheEntry{
		entry("chunk-hash", "old"), entry("old-generation", "old"), entry("edited-away", "current"), entry("backfilled-history", ""),
		entry("note-hash", "old-note"), entry("old-note-generation", "old-note"), entry("note-edited-away", "note-current"),
	}))
	pruned, err := store.PruneEmbeddingCache(ctx, "current", "note-current")
	require.NoError(t, err)
	require.Equal(t, int64(2), pruned)
	got, err := store.EmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash", "old-generation", "edited-away", "backfilled-history", "note-hash", "old-note-generation", "note-edited-away"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chunk-hash", "edited-away", "backfilled-history", "note-hash", "note-edited-away"}, slices.Collect(maps.Keys(got)), "only the unreferenced earlier-format vectors go; either kind's current-format history stays")
	formats := map[string]string{}
	for _, hash := range []string{"chunk-hash", "note-hash"} {
		var format string
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT format FROM intel_embedding_cache WHERE text_hash = ?`, hash).Scan(&format))
		formats[hash] = format
	}
	require.Equal(t, map[string]string{"chunk-hash": "current", "note-hash": "note-current"}, formats, "a vector a current chunk uses adopts its kind's current format")

	// Text that survived the format change is then edited away; the next
	// prune must keep it so reverting the edit still reuses it.
	_, err = store.db.ExecContext(ctx, `UPDATE ontology_node_embedding_state SET chunk_text_hash = 'edited-text'`)
	require.NoError(t, err)
	pruned, err = store.PruneEmbeddingCache(ctx, "current", "note-current")
	require.NoError(t, err)
	require.Zero(t, pruned)
	got, err = store.EmbeddingCacheVectors(ctx, "test", "deterministic", 3, []string{"chunk-hash"})
	require.NoError(t, err)
	require.Contains(t, got, "chunk-hash", "text carried across a format change keeps its revert reuse")
}

// seedPlainNoteSectionVector stores a plain note's authored-section chunk and
// its vector the way note publication does, with the note index metadata that
// names the provider and model, for a chunk whose text hashes to hash.
func seedPlainNoteSectionVector(t *testing.T, store *Store, hash string, vector embeddings.Embedding) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"section:guide"}, []codeanchor.IntelChunk{{
		ChunkID: "note-chunk", OwnerID: "section:guide", OwnerType: "doc_section", ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection,
		Granularity: "section", ContentHash: hash, UpdatedAt: 1,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"note-chunk": vector}))
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS emb_index_meta (id INTEGER PRIMARY KEY CHECK (id = 1), provider TEXT, model TEXT, dimensions INTEGER)`,
		`INSERT OR REPLACE INTO emb_index_meta (id, provider, model, dimensions) VALUES (1, 'test', 'deterministic', 3)`,
	} {
		_, err := store.db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
}
