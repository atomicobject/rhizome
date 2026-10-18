package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

func sharedDomainFixture(t *testing.T) (*sql.DB, *NoteStore, *CodeStore) {
	t.Helper()
	ctx := t.Context()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(filepath.Join(t.TempDir(), "shared.sqlite")), sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	noteMeta := embeddings.IndexMetadata{Provider: "test-notes", Model: "note-paid", Dimensions: 4}
	codeMeta := embeddings.IndexMetadata{Provider: "test-code", Model: "code-paid", Dimensions: 4}
	notes, err := OpenNotes(ctx, Borrowed(db), Options{Access: WriteIndex, Dimensions: 4, Metadata: &noteMeta})
	require.NoError(t, err)
	code, err := OpenCode(ctx, Borrowed(db), Options{Access: WriteIndex, Dimensions: 4, Metadata: &codeMeta})
	require.NoError(t, err)
	_, err = notes.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	_, err = code.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.NoError(t, notes.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "n1", Title: "First", Path: "first.md"}))
	require.NoError(t, notes.UpsertNoteChunks(ctx, "n1", []embeddings.ChunkInput{{Index: 0, Hash: "note-paid"}}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, code.UpsertItemMeta(ctx, codeindex.Item{AnchorID: "a1", Path: "source.go", Kind: "function", Symbol: "Source"}))
	require.NoError(t, code.UpsertItemChunks(ctx, "a1", []codeindex.ChunkInput{{Index: 0, Granularity: "symbol", Hash: "code-paid"}}, []string{"func Source() {}"}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, code.UpsertItemEmbedding(ctx, "a1", "item-paid", embeddings.Embedding{0, 1, 0, 0}))
	require.NoError(t, notes.CommitSyncGeneration(ctx))
	require.NoError(t, code.CommitSyncGeneration(ctx))
	return db, notes, code
}

func TestSharedDatabaseKeepsDomainCacheVisibilityAndRollbackIndependent(t *testing.T) {
	db, notes, code := sharedDomainFixture(t)
	ctx := t.Context()
	query := embeddings.Embedding{1, 0, 0, 0}
	require.NoError(t, notes.CacheEmbedding(ctx, "note-paid", embeddings.Embedding{0, 1, 0, 0}))
	require.NoError(t, code.CacheEmbedding(ctx, "code-paid", embeddings.Embedding{0, 1, 0, 0}))
	noteVector, ok, err := notes.EmbeddingByHash(ctx, "note-paid")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, embeddings.Embedding{1, 0, 0, 0}, noteVector)
	codeVector, ok, err := code.EmbeddingByHash(ctx, "code-paid")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, embeddings.Embedding{0, 1, 0, 0}, codeVector)

	require.NoError(t, code.CacheEmbedding(ctx, "unused", query))
	pruned, err := code.PruneStaleCache(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	itemVector, ok, err := code.EmbeddingByHash(ctx, "item-paid")
	require.NoError(t, err)
	require.True(t, ok, "item-only paid vectors must survive cache pruning")
	require.Equal(t, embeddings.Embedding{0, 1, 0, 0}, itemVector)

	_, err = notes.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.NoError(t, notes.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "n2", Title: "Second", Path: "second.md"}))
	require.NoError(t, notes.UpsertNoteChunks(ctx, "n2", []embeddings.ChunkInput{{Index: 0, Hash: "second-paid"}}, []embeddings.Embedding{query}))
	hits, _, err := notes.SearchChunksByVector(ctx, query, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, embeddings.NoteID("n1"), hits[0].NoteID)
	require.NoError(t, notes.CommitSyncGeneration(ctx))
	hits, _, err = notes.SearchChunksByVector(ctx, query, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, embeddings.NoteID("n2"), hits[0].NoteID)
	codeHits, _, err := code.SearchChunksByVector(ctx, query, 10)
	require.NoError(t, err)
	require.Len(t, codeHits, 1)
	require.Equal(t, codeindex.AnchorID("a1"), codeHits[0].AnchorID)
	generation, visible, err := code.VisibleGeneration(ctx)
	require.NoError(t, err)
	require.True(t, visible)
	require.EqualValues(t, 1, generation)

	_, err = db.ExecContext(ctx, `CREATE TRIGGER reject_shared_recovery BEFORE DELETE ON rzm_migration_state
WHEN OLD.domain = 'code_embeddings' BEGIN SELECT RAISE(ABORT, 'cross-domain rollback'); END`)
	require.NoError(t, err)
	err = code.rebuildDomainPreservingEmbeddings(ctx)
	require.ErrorContains(t, err, "cross-domain rollback")
	for _, cached := range []struct {
		store interface {
			EmbeddingByHash(context.Context, string) (embeddings.Embedding, bool, error)
		}
		hash string
		want embeddings.Embedding
	}{{notes, "note-paid", noteVector}, {code, "code-paid", codeVector}, {code, "item-paid", itemVector}} {
		vector, present, err := cached.store.EmbeddingByHash(ctx, cached.hash)
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, cached.want, vector)
	}
	codeHits, _, err = code.SearchChunksByVector(ctx, query, 10)
	require.NoError(t, err)
	require.Len(t, codeHits, 1)
	_, err = db.ExecContext(ctx, `DROP TRIGGER reject_shared_recovery`)
	require.NoError(t, err)
	require.NoError(t, code.ResetDomainPreservingCache(ctx))
	hits, _, err = notes.SearchChunksByVector(ctx, query, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, embeddings.NoteID("n2"), hits[0].NoteID)
	vector, present, err := code.EmbeddingByHash(ctx, "item-paid")
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, itemVector, vector)
}

func TestBorrowedReadersCannotMutateOrHealTheirDatabase(t *testing.T) {
	db, notes, code := sharedDomainFixture(t)
	ctx := t.Context()
	query := embeddings.Embedding{1, 0, 0, 0}
	_, _, err := notes.SearchChunksByVector(ctx, query, 1)
	require.NoError(t, err)
	_, _, err = code.SearchChunksByVector(ctx, query, 1)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `PRAGMA busy_timeout=1234`)
	require.NoError(t, err)
	reader, err := OpenNotes(ctx, Borrowed(db), Options{Access: ReadExisting, Dimensions: 4})
	require.NoError(t, err)
	codeReader, err := OpenCode(ctx, Borrowed(db), Options{Access: ReadExisting, Dimensions: 4})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DROP TABLE emb_chunk_embeddings_vec_d4`)
	require.NoError(t, err)
	var beforeSchema, beforeChanges int64
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&beforeSchema))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&beforeChanges))
	for _, mutate := range []func() error{
		func() error { return reader.CacheEmbedding(ctx, "forbidden", query) },
		func() error { return reader.EnsureSchema(ctx) },
		func() error { return reader.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Dimensions: 4}) },
		func() error { return reader.ResetDomain(ctx) },
		func() error { return reader.ResetDomainPreservingCache(ctx) },
		func() error { _, err := reader.IncrementSyncGeneration(ctx); return err },
		func() error { return reader.CommitSyncGeneration(ctx) },
		func() error { return reader.UpdateLastSync(ctx, time.Now()) },
		func() error { return reader.UpdateSourceHighWater(ctx, time.Time{}) },
		func() error { return reader.ClearEmbeddingCache(ctx) },
		func() error { return reader.UpsertNoteMetaBatch(ctx, nil) },
		func() error { return reader.SyncNoteChunksBatch(ctx, nil) },
		func() error { _, err := reader.PruneStaleCache(ctx, 0); return err },
		func() error { _, err := codeReader.PruneStaleItems(ctx, 0); return err },
		func() error { return codeReader.UpsertItemEmbeddingBatch(ctx, nil) },
		func() error { return codeReader.SetFingerprintVersion(ctx, 42) },
	} {
		require.ErrorIs(t, mutate(), ErrReadOnly)
	}
	_, _, err = reader.SearchChunksByVector(ctx, query, 1)
	require.ErrorContains(t, err, "mirror emb_chunk_embeddings_vec_d4 is missing")
	var afterSchema, afterChanges int64
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&afterSchema))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&afterChanges))
	require.Equal(t, beforeSchema, afterSchema)
	require.Equal(t, beforeChanges, afterChanges)
	var foreignKeys, busyTimeout int
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys))
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout))
	require.Equal(t, 0, foreignKeys)
	require.Equal(t, 1234, busyTimeout)
	require.NoError(t, reader.Close())
	require.NoError(t, codeReader.Close())
	require.NoError(t, db.PingContext(ctx))

	missing := filepath.Join(t.TempDir(), "never-created.sqlite")
	_, err = OpenNotes(ctx, File(missing), Options{})
	require.ErrorContains(t, err, "access must be")
	for _, source := range []Source{{}, File(""), Borrowed(nil), {file: missing, db: db}} {
		_, err = OpenNotes(ctx, source, Options{Access: WriteIndex})
		require.ErrorContains(t, err, "exactly one")
	}
	_, err = OpenNotes(ctx, File(missing), Options{Access: ReadExisting})
	require.Error(t, err)
	_, err = os.Stat(missing)
	require.True(t, os.IsNotExist(err))
}
