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

	// Legacy schema: code_index_meta is missing fingerprint_version column.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE code_index_meta (
			id INTEGER PRIMARY KEY,
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			schema_version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			last_sync INTEGER
		);
		CREATE TABLE code_items (
			id INTEGER PRIMARY KEY,
			anchor_id TEXT NOT NULL UNIQUE,
			lang TEXT,
			kind TEXT,
			path TEXT NOT NULL,
			symbol TEXT,
			fqn TEXT,
			fingerprint TEXT,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE code_item_embeddings (
			id INTEGER PRIMARY KEY,
			item_row_id INTEGER NOT NULL REFERENCES code_items(id) ON DELETE CASCADE,
			content_hash TEXT NOT NULL,
			embedding BLOB NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			UNIQUE(item_row_id)
		);
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_index_meta(id, provider, model, dimensions, schema_version, created_at, last_sync) VALUES (1, 'test', 'm', 3, 1, 1, 0)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_items(anchor_id, path, updated_at) VALUES ('a1', 'p.go', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_item_embeddings(item_row_id, content_hash, embedding, dimensions, created_at) VALUES (1, 'h', X'000000000000000000000000', 3, 1)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path, 3)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:           "test",
		Model:              "m",
		Dimensions:         3,
		SchemaVersion:      currentSchemaVersion,
		FingerprintVersion: 1,
	}))
	emb, _, ok, err := store.GetItemEmbedding(ctx, "a1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, emb, 3)
}

func TestOpen_BackfillsMissingSourceHighWaterToZero(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-highwater.sqlite")

	db, err := sql.Open("sqlite3", sqliteutil.DSN(path))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE code_index_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			schema_version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			fingerprint_version INTEGER NOT NULL DEFAULT 0,
			last_sync INTEGER,
			sync_generation INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO code_index_meta(id, provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, sync_generation)
		VALUES (1, 'test', 'm', 3, 11, 1, 2, 123, 0);
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

func TestOpen_PreservesEmbeddingsWhenLegacyItemsNeedAnchorNormalization(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-unified.sqlite")

	db, err := sql.Open("sqlite3", sqliteutil.DSN(path))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE code_index_meta (
			id INTEGER PRIMARY KEY,
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			schema_version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			last_sync INTEGER
		);
		CREATE TABLE code_items (
			id INTEGER PRIMARY KEY,
			anchor_id TEXT NOT NULL UNIQUE,
			lang TEXT,
			kind TEXT,
			path TEXT NOT NULL,
			symbol TEXT,
			fqn TEXT,
			fingerprint TEXT,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE code_item_embeddings (
			id INTEGER PRIMARY KEY,
			item_row_id INTEGER NOT NULL REFERENCES code_items(id) ON DELETE CASCADE,
			content_hash TEXT NOT NULL,
			embedding BLOB NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			UNIQUE(item_row_id)
		);
		CREATE TABLE intel_code_anchors (
			id INTEGER PRIMARY KEY,
			anchor_id TEXT NOT NULL UNIQUE CHECK (anchor_id != ''),
			lang TEXT NOT NULL CHECK (lang != ''),
			kind TEXT NOT NULL CHECK (kind != ''),
			path TEXT NOT NULL CHECK (path != ''),
			symbol TEXT NOT NULL CHECK (symbol != ''),
			fqn TEXT,
			signature TEXT,
			doc_comment TEXT,
			start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
			end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
			start_line INTEGER NOT NULL CHECK (start_line >= 0),
			end_line INTEGER NOT NULL CHECK (end_line >= start_line),
			fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
			updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
		) STRICT;
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_index_meta(id, provider, model, dimensions, schema_version, created_at, last_sync) VALUES (1, 'test', 'm', 3, 1, 1, 0)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_items(anchor_id, lang, kind, path, symbol, updated_at) VALUES ('a1', NULL, NULL, 'pkg/legacy.go', NULL, 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_item_embeddings(item_row_id, content_hash, embedding, dimensions, created_at) VALUES (1, 'h', X'000000000000000000000000', 3, 1)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path, 3)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	emb, _, ok, err := store.GetItemEmbedding(ctx, "a1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, emb, 3)

	items, err := store.ListItems(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "a1", string(items[0].AnchorID))
	require.Equal(t, "pkg/legacy.go", items[0].Path)
	require.Equal(t, "unknown", items[0].Lang)
	require.Equal(t, "unknown", items[0].Kind)
	require.Equal(t, "a1", items[0].Symbol)
}
