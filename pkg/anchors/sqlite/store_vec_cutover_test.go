package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSearchEmbeddings_LegacyBlobSchemaRequiresReindexAfterUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "intel-vec-legacy.db")
	store, err := Open(path)
	require.NoError(t, err)
	now := time.Now().Unix()
	_, err = store.db.ExecContext(ctx, `
		UPDATE schema_version SET version = 25
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		UPDATE rzm_migration_state SET version = 25, dirty = 0
		WHERE domain = 'intel'
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DROP TABLE IF EXISTS intel_embeddings`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		CREATE TABLE intel_embeddings (
			chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
			embedding BLOB NOT NULL,
			norm REAL NOT NULL DEFAULT 0,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			created_at INTEGER NOT NULL CHECK (created_at >= 0),
			FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT
	`)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_chunks (chunk_id, owner_id, owner_type, ord, granularity, breadcrumb, heading, content_hash, start_byte, end_byte, updated_at)
		VALUES ('c1', 'a1', 'anchor', 0, 'symbol', 'src/a.go', 'A', 'h1', 0, 0, ?)
	`, now)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
		VALUES ('c1', ?, 1.0, 4, ?)
	`, embedToBytes(embeddings.Embedding{1, 0, 0, 0}), now)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hits, skipped, err := store.SearchEmbeddings(ctx, embeddings.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{
		OwnerTypes: []string{"anchor"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, hits, 0)

	hasEmbeddingBlob, err := store.tableHasColumn(ctx, "intel_embeddings", "embedding")
	require.NoError(t, err)
	require.False(t, hasEmbeddingBlob, "open should replace legacy blob schema with the current derived layout")
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+intelVecTableName(4)).Scan(&count))
	require.Zero(t, count)
}
