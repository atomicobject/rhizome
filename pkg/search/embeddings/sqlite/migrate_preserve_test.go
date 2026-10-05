package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestOpen_PreservesEmbeddingsWhenLegacyTablesMissingColumns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")

	db, err := sql.Open("sqlite3", sqliteutil.DSN(path))
	require.NoError(t, err)

	// Legacy schema: emb_index_meta is missing schema_version + last_sync columns.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE emb_index_meta (
			id INTEGER PRIMARY KEY,
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			created_at INTEGER NOT NULL
		);
		CREATE TABLE emb_notes (
			id INTEGER PRIMARY KEY,
			note_id TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			path TEXT NOT NULL,
			last_seen_mtime INTEGER NOT NULL,
			last_seen_size INTEGER NOT NULL
		);
		CREATE TABLE emb_note_embeddings (
			id INTEGER PRIMARY KEY,
			note_row_id INTEGER NOT NULL REFERENCES emb_notes(id) ON DELETE CASCADE,
			content_hash TEXT NOT NULL,
			embedding BLOB NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE TABLE emb_chunk_embeddings (
			id INTEGER PRIMARY KEY,
			note_row_id INTEGER NOT NULL REFERENCES emb_notes(id) ON DELETE CASCADE,
			chunk_index INTEGER NOT NULL,
			breadcrumb TEXT,
			heading TEXT,
			content_hash TEXT NOT NULL,
			embedding BLOB NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		);
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO emb_index_meta(id, provider, model, dimensions, created_at) VALUES (1, 'test', 'm', 3, 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO emb_notes(note_id, title, path, last_seen_mtime, last_seen_size) VALUES ('n1', 'N1', 'n1.md', 1, 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO emb_note_embeddings(note_row_id, content_hash, embedding, dimensions, created_at) VALUES (1, 'h', X'000000000000000000000000', 3, 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO emb_chunk_embeddings(note_row_id, chunk_index, breadcrumb, heading, content_hash, embedding, dimensions, created_at) VALUES (1, 0, 'b', 'h', 'ch', X'000000000000000000000000', 3, 1)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path, 3)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:      "test",
		Model:         "m",
		Dimensions:    3,
		SchemaVersion: currentSchemaVersion,
	}))

	chunks, err := store.NoteChunks(ctx, embeddings.NoteID("n1"))
	require.NoError(t, err)
	require.Len(t, chunks, 1)
}

func TestOpen_BackfillsMissingSourceHighWaterToZero(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-highwater.sqlite")

	db, err := sql.Open("sqlite3", sqliteutil.DSN(path))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE emb_index_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			schema_version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			last_sync INTEGER,
			sync_generation INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO emb_index_meta(id, provider, model, dimensions, schema_version, created_at, last_sync, sync_generation)
		VALUES (1, 'test', 'm', 3, 6, 1, 123, 0);
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path, 3)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.SourceHighWater.IsZero())
}
