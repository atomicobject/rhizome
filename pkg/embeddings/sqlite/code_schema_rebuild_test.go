package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func codeSeedSchemaRecoveryStore(t *testing.T) *CodeStore {
	t.Helper()
	ctx := context.Background()
	store, err := OpenCode(context.Background(), File(filepath.Join(t.TempDir(), "recovery.sqlite")), Options{Access: WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "paid", Dimensions: 4}))

	item := codeindex.Item{AnchorID: "a1", Path: "pkg/source.go", Kind: "function", Symbol: "Source", FQN: "pkg.Source"}
	require.NoError(t, store.UpsertItemMeta(ctx, item))
	require.NoError(t, store.UpsertItemChunks(ctx, item.AnchorID, []codeindex.ChunkInput{{Index: 0, Granularity: "symbol", Hash: "paid-chunk"}}, []string{"func Source() {}"}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertItemEmbedding(ctx, item.AnchorID, "paid-item", embeddings.Embedding{0, 1, 0, 0}))

	require.NoError(t, store.CacheEmbedding(ctx, "paid-cache-only", embeddings.Embedding{3, 4, 0, 0}))
	return store
}

func codeSchemaRecoveryVectors(t *testing.T, store *CodeStore) []string {
	t.Helper()
	var values []string
	for _, table := range []string{codeTableChunkEmbeddings, codeTableEmbeddingCache, tableItemEmbeddings} {
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

func TestCodeRebuildPreservesVectorsWithRotatingPoolConnections(t *testing.T) {
	store := codeSeedSchemaRecoveryStore(t)
	before := codeSchemaRecoveryVectors(t, store)
	store.db.SetMaxOpenConns(4)
	store.db.SetMaxIdleConns(0)
	// Keep one connection occupied; every non-transactional call would use a new connection.
	held, err := store.db.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()
	require.NoError(t, store.rebuildDomainPreservingEmbeddings(context.Background()))
	require.Equal(t, before, codeSchemaRecoveryVectors(t, store))
	vector, ok, err := store.EmbeddingByHash(context.Background(), "paid-cache-only")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, embeddings.Embedding{3, 4, 0, 0}, vector)
}

func TestCodeSchemaRecoveryFailureRollsBackDroppedEmbeddingTables(t *testing.T) {
	store := codeSeedSchemaRecoveryStore(t)
	ctx := context.Background()
	// Force automatic recovery, then fail after it drops the embedding domain tables.
	_, err := store.db.ExecContext(ctx, `ALTER TABLE `+codeTableIndexMeta+` DROP COLUMN visible_generation`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `CREATE TRIGGER reject_recovery BEFORE DELETE ON rzm_migration_state
 WHEN OLD.domain = '`+string(migration.DomainCodeEmbeddings)+`'
 BEGIN SELECT RAISE(ABORT, 'injected recovery failure'); END;`)
	require.NoError(t, err)
	before := codeSchemaRecoveryVectors(t, store)
	err = store.ensureSchemaWithRecovery(ctx)
	require.ErrorContains(t, err, "injected recovery failure")
	require.Equal(t, before, codeSchemaRecoveryVectors(t, store), "rollback must retain chunks and paid cache after domain tables were dropped")
	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainCodeEmbeddings)).Scan(&version))
	require.Equal(t, codeSchemaVersion, version)
}
