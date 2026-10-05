package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type vecLogicalRow struct {
	chunkID   int64
	embedding string
}

func TestOpenMigratesVectorOwnerPartitionsWithoutChangingLogicalPopulation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "owner-partition.db")
	store := newVecOwnerMigrationFixture(t, ctx, dbPath)
	before := readVecLogicalRows(t, ctx, store.db, intelVecTableName(4))
	expected := append([]vecLogicalRow(nil), before...)
	expected = append(expected, vecLogicalRow{chunkID: 9999, embedding: fmt.Sprintf("%X", embedToBytes(embeddingstypes.Embedding{0, 0, 1, 0}))})

	replaceWithV66VectorTable(t, ctx, store.db, before, true)
	markIntelSchemaVersion(t, ctx, store.db, 66)
	require.NoError(t, store.Close())

	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hasOwnerType, err := store.tableHasColumn(ctx, intelVecTableName(4), "owner_type")
	require.NoError(t, err)
	require.True(t, hasOwnerType)
	require.Equal(t, expected, readVecLogicalRows(t, ctx, store.db, intelVecTableName(4)))

	var ownerTypes map[int64]string = map[int64]string{}
	rows, err := store.db.QueryContext(ctx, `SELECT chunk_id, owner_type FROM `+intelVecTableName(4)+` ORDER BY chunk_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ownerType string
		require.NoError(t, rows.Scan(&id, &ownerType))
		ownerTypes[id] = ownerType
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[int64]string{1: "anchor", 2: "doc_section", 9999: ""}, ownerTypes)

	got, skipped, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{
		OwnerTypes:   []string{"anchor"},
		PathPrefixes: []string{"pkg"},
	})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, []string{"chunk-code"}, scoredChunkIDs(got))

	// A second open exercises validation of the committed schema rather than
	// relying on migration-time state.
	require.NoError(t, store.Close())
	store, err = Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.Equal(t, expected, readVecLogicalRows(t, ctx, store.db, intelVecTableName(4)))
}

func TestOpenRollsBackVectorOwnerPartitionMigrationOnCopyFailure(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "owner-partition-rollback.db")
	store := newVecOwnerMigrationFixture(t, ctx, dbPath)
	before := readVecLogicalRows(t, ctx, store.db, intelVecTableName(4))
	replaceWithV66VectorTable(t, ctx, store.db, before, false)
	markIntelSchemaVersion(t, ctx, store.db, 66)
	store.db.SetMaxOpenConns(1)
	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	denied := false
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, name, _ string, database string) int {
			if op == sqlite3.SQLITE_INSERT && name == intelVecTableName(4) && database == "main" {
				denied = true
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())

	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	err = migrateIntelVecOwnerPartitions(ctx, tx)
	require.Error(t, err)
	require.True(t, denied, "the real vector copy must reach the denied INSERT")
	require.NoError(t, tx.Rollback())

	// The rollback restores the original virtual table and its shadow tables.
	// Reading embeddings exercises those shadow references directly.
	require.Equal(t, before, readVecLogicalRows(t, ctx, store.db, intelVecTableName(4)))
	hasOwnerType, err := store.tableHasColumn(ctx, intelVecTableName(4), "owner_type")
	require.NoError(t, err)
	require.False(t, hasOwnerType)
	var temporaryCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, intelVecTableName(4)+"_owner_partition_v67_stage").Scan(&temporaryCount))
	require.Zero(t, temporaryCount, "the failed transaction must not expose the replacement table")
	require.NoError(t, store.Close())

	// A later open reruns the numbered migration and leaves the database usable.
	store, err = Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	got, skipped, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 2, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, []string{"chunk-code", "chunk-doc"}, scoredChunkIDs(got))
}

func newVecOwnerMigrationFixture(t *testing.T, ctx context.Context, dbPath string) *Store {
	t.Helper()
	store, err := Open(dbPath)
	require.NoError(t, err)
	anchor := codeanchor.IntelAnchor{AnchorID: "owner-code", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/code.go", Symbol: "Code", FQN: "pkg.Code", Fingerprint: "code"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	section := codeanchor.IntelDocSection{SectionID: "owner-doc", Path: "docs/guide.md", Title: "Guide", Level: 1, Content: "guide", Fingerprint: "doc"}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, section.Path, []codeanchor.IntelDocSection{section}, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchor.AnchorID, section.SectionID}, []codeanchor.IntelChunk{
		{ChunkID: "chunk-code", OwnerID: anchor.AnchorID, OwnerType: "anchor", Granularity: "symbol", ContentHash: "code"},
		{ChunkID: "chunk-doc", OwnerID: section.SectionID, OwnerType: "doc_section", Granularity: "section", ContentHash: "doc"},
	}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"chunk-code": {1, 0, 0, 0},
		"chunk-doc":  {0, 1, 0, 0},
	}))
	return store
}

func readVecLogicalRows(t *testing.T, ctx context.Context, db *sql.DB, table string) []vecLogicalRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT chunk_id, hex(embedding) FROM `+table+` ORDER BY chunk_id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []vecLogicalRow
	for rows.Next() {
		var row vecLogicalRow
		require.NoError(t, rows.Scan(&row.chunkID, &row.embedding))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}

func replaceWithV66VectorTable(t *testing.T, ctx context.Context, db *sql.DB, rows []vecLogicalRow, includeOrphan bool) {
	t.Helper()
	table := intelVecTableName(4)
	_, err := db.ExecContext(ctx, `DROP TABLE `+table)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE VIRTUAL TABLE %s USING vec0(chunk_id integer primary key, embedding float[4] distance_metric=cosine)`, table))
	require.NoError(t, err)
	for _, row := range rows {
		_, err = db.ExecContext(ctx, `INSERT INTO `+table+`(chunk_id, embedding) VALUES (?, ?)`, row.chunkID, mustDecodeHex(t, row.embedding))
		require.NoError(t, err)
	}
	if includeOrphan {
		_, err = db.ExecContext(ctx, `INSERT INTO `+table+`(chunk_id, embedding) VALUES (9999, ?)`, embedToBytes(embeddingstypes.Embedding{0, 0, 1, 0}))
		require.NoError(t, err)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	require.NoError(t, err)
	return decoded
}

func markIntelSchemaVersion(t *testing.T, ctx context.Context, db *sql.DB, version int) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		UPDATE schema_version SET version = ?;
		UPDATE rzm_migration_state SET version = ?, dirty = 0 WHERE domain = 'intel';
		DELETE FROM rzm_migration_log WHERE domain = 'intel' AND from_version >= ?;
	`, version, version, version)
	require.NoError(t, err)
}
