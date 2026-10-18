package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func seedSchemaRecoveryStore(t *testing.T) *NoteStore {
	t.Helper()
	ctx := context.Background()
	store, err := OpenNotes(context.Background(), File(filepath.Join(t.TempDir(), "recovery.sqlite")), Options{Access: WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "paid", Dimensions: 4}))

	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "n1", Title: "Paid note", Path: "notes/paid.md", Mtime: time.Unix(1000, 0), Size: 1}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "n1", []embeddings.ChunkInput{embeddings.NewChunkInput(0, "paid chunk", "breadcrumb", "paid-chunk")}, []embeddings.Embedding{{1, 0, 0, 0}}))

	require.NoError(t, store.CacheEmbedding(ctx, "paid-cache-only", embeddings.Embedding{3, 4, 0, 0}))
	return store
}

func schemaRecoveryVectors(t *testing.T, store *NoteStore) []string {
	t.Helper()
	var values []string
	for _, table := range []string{tableChunkEmbeddings, tableEmbeddingCache} {
		rows, err := store.db.QueryContext(context.Background(), `SELECT content_hash || ':' || hex(embedding) || ':' || norm || ':' || dimensions || ':' || created_at FROM `+table+` ORDER BY content_hash`)
		require.NoError(t, err)
		for rows.Next() {
			var value string
			require.NoError(t, rows.Scan(&value))
			values = append(values, table+":"+value)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	return values
}

func TestRebuildPreservesVectorsWithRotatingPoolConnections(t *testing.T) {
	store := seedSchemaRecoveryStore(t)
	before := schemaRecoveryVectors(t, store)
	store.db.SetMaxOpenConns(4)
	store.db.SetMaxIdleConns(0)
	// Keep one connection occupied; every non-transactional call would use a new connection.
	held, err := store.db.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()
	require.NoError(t, store.rebuildDomainPreservingEmbeddings(context.Background()))
	require.Equal(t, before, schemaRecoveryVectors(t, store))
	vector, ok, err := store.EmbeddingByHash(context.Background(), "paid-cache-only")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, embeddings.Embedding{3, 4, 0, 0}, vector)
}

func TestSchemaRecoveryFailureRollsBackDroppedEmbeddingTables(t *testing.T) {
	store := seedSchemaRecoveryStore(t)
	ctx := context.Background()
	// Force automatic recovery, then fail after it drops the embedding domain tables.
	_, err := store.db.ExecContext(ctx, `ALTER TABLE `+tableIndexMeta+` DROP COLUMN visible_generation`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `CREATE TRIGGER reject_recovery BEFORE DELETE ON rzm_migration_state
 WHEN OLD.domain = '`+string(migration.DomainNoteEmbeddings)+`'
 BEGIN SELECT RAISE(ABORT, 'injected recovery failure'); END;`)
	require.NoError(t, err)
	before := schemaRecoveryVectors(t, store)
	err = store.ensureSchemaWithRecovery(ctx)
	require.ErrorContains(t, err, "injected recovery failure")
	require.Equal(t, before, schemaRecoveryVectors(t, store), "rollback must retain chunks and paid cache after domain tables were dropped")
	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainNoteEmbeddings)).Scan(&version))
	require.Equal(t, noteSchemaVersion, version)
}
