package sqlite

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/stretchr/testify/require"
)

func TestEnsureDomainMigrations_RebuildsLegacyBodyChunkState(t *testing.T) {
	ctx := context.Background()

	path := t.TempDir() + "/code.db"
	store, err := Open(path, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := int64(1)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at)
		VALUES (1, 'test', 'm', 4, 1, ?)`, now)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors (anchor_id, lang, kind, path, symbol, fqn)
		VALUES ('a1', 'go', 'func', 'pkg/foo.go', 'Foo', 'pkg.Foo')`)
	require.NoError(t, err)
	res, err := store.db.ExecContext(ctx, `
		INSERT INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at)
		SELECT id, 'fp', ? FROM intel_code_anchors WHERE anchor_id = 'a1'`, now)
	require.NoError(t, err)
	rowID, err := res.LastInsertId()
	require.NoError(t, err)

	body := "Kind: func_body\nLang: go\nFQN: pkg.Foo\nPath: pkg/foo.go\nChunk: 1/2\n\nbody"
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableChunkEmbeddings+` (item_row_id, chunk_index, granularity, breadcrumb, heading, content_hash, embedding, dimensions, created_at)
		VALUES (?, 0, 'body', 'pkg/foo.go', 'Foo', 'h0', X'0001', 4, ?)`, rowID, now)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableChunkEmbeddings+` (item_row_id, chunk_index, granularity, breadcrumb, heading, content_hash, embedding, dimensions, created_at)
		VALUES (?, 1, 'body', 'pkg/foo.go', 'Foo', 'h1', X'0002', 4, ?)`, rowID, now)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableChunkFTS+` (anchor_id, chunk_index, path, symbol, fqn, kind, granularity, breadcrumb, heading, body)
		VALUES ('a1', 0, 'pkg/foo.go', 'Foo', 'pkg.Foo', 'func', 'body', 'pkg/foo.go', 'Foo', ?)`, body)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableChunkFTS+` (anchor_id, chunk_index, path, symbol, fqn, kind, granularity, breadcrumb, heading, body)
		VALUES ('a1', 1, 'pkg/foo.go', 'Foo', 'pkg.Foo', 'func', 'body', 'pkg/foo.go', 'Foo', ?)`, body)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `UPDATE rzm_migration_state SET version = ?, dirty = 0 WHERE domain = ?`, 1, string(migration.DomainCodeEmbeddings))
	require.NoError(t, err)
	require.NoError(t, store.ensureDomainMigrations(ctx))

	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT schema_version FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM `+tableChunkEmbeddings+`
		WHERE item_row_id = ? AND granularity = 'body'`, rowID).Scan(&count))
	require.Equal(t, 0, count)

	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM `+tableChunkFTS+`
		WHERE anchor_id = 'a1' AND granularity = 'body'`).Scan(&count))
	require.Equal(t, 0, count)
}

func TestEnsureDomainMigrations_DropsLegacyDuplicateChunksAndPreservesCache(t *testing.T) {
	ctx := context.Background()

	path := t.TempDir() + "/code.db"
	store, err := Open(path, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Start at schema version 2.
	now := int64(10)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at)
		VALUES (1, 'test', 'm', 4, 2, ?)`, now)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors (anchor_id, lang, kind, path, symbol, fqn)
		VALUES ('a1', 'go', 'module', 'pkg/foo.go', 'foo.go', '')`)
	require.NoError(t, err)
	res, err := store.db.ExecContext(ctx, `
		INSERT INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at)
		SELECT id, 'fp', ? FROM intel_code_anchors WHERE anchor_id = 'a1'`, now)
	require.NoError(t, err)
	rowID, err := res.LastInsertId()
	require.NoError(t, err)

	// Version 2 permitted two granularities at the same index. Current migrations
	// rebuild derived rows from source anchors, so neither legacy row survives.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableChunkEmbeddings+` (item_row_id, chunk_index, granularity, breadcrumb, heading, content_hash, embedding, dimensions, created_at)
		VALUES (?, 0, 'symbol', 'pkg/foo.go', 'foo.go', 'old', X'00000000000000000000000000000000', 4, ?)`, rowID, now)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO `+tableEmbeddingCache+` (content_hash, embedding, dimensions, created_at) VALUES ('reusable', X'00000000000000000000000000000000', 4, ?)`, now)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableChunkEmbeddings+` (item_row_id, chunk_index, granularity, breadcrumb, heading, content_hash, embedding, dimensions, created_at)
		VALUES (?, 0, 'module', 'pkg/foo.go', 'foo.go', 'new', X'00000000000000000000000000000000', 4, ?)`, rowID, now+1)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `UPDATE rzm_migration_state SET version = ?, dirty = 0 WHERE domain = ?`, 2, string(migration.DomainCodeEmbeddings))
	require.NoError(t, err)
	require.NoError(t, store.ensureDomainMigrations(ctx))

	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT schema_version FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM `+tableChunkEmbeddings+`
		WHERE item_row_id = ? AND chunk_index = 0
	`, rowID).Scan(&count))
	require.Equal(t, 0, count)
	var provider, model string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT provider, model FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&provider, &model))
	require.Equal(t, "test", provider)
	require.Equal(t, "m", model)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableEmbeddingCache+` WHERE content_hash = 'reusable'`).Scan(&count))
	require.Equal(t, 1, count)
}
