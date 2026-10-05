package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestRationaleSchemaExistsBeforeFirstOperation(t *testing.T) {
	for _, mode := range []string{"fresh", "warm", "external handle", "obsolete reset"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "rationale.db")
			if mode == "warm" {
				store, err := Open(path)
				require.NoError(t, err)
				require.NoError(t, store.Close())
			}
			if mode == "obsolete reset" {
				db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
				require.NoError(t, err)
				_, err = db.ExecContext(ctx, `CREATE TABLE schema_version(version INTEGER NOT NULL); INSERT INTO schema_version VALUES (55)`)
				require.NoError(t, err)
				require.NoError(t, db.Close())
			}
			var store *Store
			var err error
			if mode == "external handle" {
				db, openErr := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
				require.NoError(t, openErr)
				t.Cleanup(func() { require.NoError(t, db.Close()) })
				store, err = OpenWithDB(db)
			} else {
				store, err = Open(path)
			}
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			for _, name := range []string{"intel_rationale_fts", "intel_rationale_fts_rowid", "idx_intel_rationale_fts_rowid"} {
				var count int
				require.NoError(t, store.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name = ?`, name).Scan(&count))
				require.Equal(t, 1, count, name)
			}
		})
	}
}

func TestRationaleOperationsRequireNoPermanentDDL(t *testing.T) {
	for _, operation := range []string{"search", "replace", "batch populated", "batch empty", "delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(filepath.Join(t.TempDir(), "rationale.db"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			batch := atomicCodeBatch(t, 2, "old")
			require.NoError(t, store.ApplyCodePersistenceBatch(ctx, batch))
			store.db.SetMaxOpenConns(1)
			conn, err := store.db.Conn(ctx)
			require.NoError(t, err)
			require.NoError(t, conn.Raw(func(raw any) error {
				raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, _, _ string, database string) int {
					if database == "main" {
						switch op {
						case sqlite3.SQLITE_CREATE_TABLE, sqlite3.SQLITE_CREATE_INDEX, sqlite3.SQLITE_CREATE_VTABLE:
							return sqlite3.SQLITE_DENY
						}
					}
					return sqlite3.SQLITE_OK
				})
				return nil
			}))
			require.NoError(t, conn.Close())
			switch operation {
			case "search":
				var hits []RationaleSearchRow
				hits, err = store.SearchRationaleFTS(ctx, "old", nil, 10)
				require.NoError(t, err)
				require.Len(t, hits, 2)
			case "replace":
				err = store.ReplaceRationaleForPath(ctx, "src/f000.py", []codeanchor.Rationale{{ID: "new", Path: "src/f000.py", Kind: codeanchor.RationaleTodo, Content: "new", Fingerprint: "new"}})
			case "batch populated", "batch empty":
				batch = atomicCodeBatch(t, 2, "new")
				if operation == "batch empty" {
					for i := range batch.RationaleBatches {
						batch.RationaleBatches[i].Rationales = nil
					}
				}
				err = store.ApplyCodePersistenceBatch(ctx, batch)
			case "delete":
				err = store.DeleteIntelByPath(ctx, "src/f000.py")
			}
			require.NoError(t, err)
		})
	}
}

func TestRationaleSearchOnReadOnlyStorePreservesSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rationale.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, atomicCodeBatch(t, 2, "evidence")))
	var before int
	require.NoError(t, store.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&before))
	require.NoError(t, store.Close())

	reader, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	hits, err := reader.SearchRationaleFTS(ctx, "evidence", nil, 10)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	for _, hit := range hits {
		require.Equal(t, "evidence", hit.Content)
	}
	var after int
	require.NoError(t, reader.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&after))
	require.Equal(t, before, after)
}

func TestRationaleSchemaDriftInvalidatesOldProofAndRepairsOnWritableOpen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt string
	}{
		{name: "missing fts", corrupt: `DROP TABLE intel_rationale_fts`},
		{name: "plain table lookalike", corrupt: `
			DROP TABLE intel_rationale_fts;
			CREATE TABLE intel_rationale_fts (
				rationale_id TEXT, path TEXT, symbol_fqn TEXT, kind TEXT, content TEXT
			)`},
		{name: "wrong fts shape", corrupt: `
			DROP TABLE intel_rationale_fts;
			CREATE VIRTUAL TABLE intel_rationale_fts USING fts5(
				rationale_id, path, symbol_fqn, kind, content, tokenize='unicode61'
			)`},
		{name: "missing rowid map index", corrupt: `DROP INDEX idx_intel_rationale_fts_rowid`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "rationale.db")
			store, err := Open(path)
			require.NoError(t, err)
			require.NoError(t, store.ReplaceRationaleForPath(ctx, "src/a.go", []codeanchor.Rationale{{
				ID: "seed", Path: "src/a.go", Kind: codeanchor.RationaleTodo, Content: "seed", Fingerprint: "seed",
			}}))
			require.NoError(t, store.Close())

			db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, tc.corrupt)
			require.NoError(t, err)
			var schemaVersion int
			require.NoError(t, db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&schemaVersion))
			_, err = db.ExecContext(ctx, `
				UPDATE rzm_migration_validation
				SET schema_fingerprint = ?, sqlite_schema_version = ?
				WHERE domain = 'intel'
			`, "intel-schema-v62-2026-09-05", schemaVersion)
			require.NoError(t, err)
			require.NoError(t, db.Close())

			reader, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
			require.Error(t, err)
			require.Nil(t, reader)
			require.True(t, IsSchemaIncompatibleError(err), err)

			store, err = Open(path)
			require.NoError(t, err)
			var ddl string
			require.NoError(t, store.db.QueryRowContext(ctx, `
				SELECT sql FROM sqlite_master
				WHERE type = 'table' AND name = 'intel_rationale_fts'
			`).Scan(&ddl))
			require.Contains(t, ddl, "CREATE VIRTUAL TABLE")
			require.Contains(t, ddl, "rationale_id UNINDEXED")
			require.Contains(t, ddl, "tokenize='porter'")
			var count int
			require.NoError(t, store.db.QueryRowContext(ctx, `
				SELECT count(*) FROM sqlite_master
				WHERE type = 'index' AND name = 'idx_intel_rationale_fts_rowid'
			`).Scan(&count))
			require.Equal(t, 1, count)
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT count(*) FROM intel_rationale`).Scan(&count))
			require.Zero(t, count, "schema drift recovery must reset the derived intel domain")
			require.NoError(t, store.Close())

			reader, err = OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
			require.NoError(t, err)
			require.NoError(t, reader.Close())
		})
	}
}

func TestHealthyRationaleSchemaRefreshesOldProofWithoutReset(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rationale.db")
	store, err := Open(path)
	require.NoError(t, err)
	want := codeanchor.Rationale{
		ID: "seed", Path: "src/a.go", Kind: codeanchor.RationaleTodo, Content: "keep me", Fingerprint: "seed",
	}
	require.NoError(t, store.ReplaceRationaleForPath(ctx, want.Path, []codeanchor.Rationale{want}))
	_, err = store.db.ExecContext(ctx, `
		UPDATE rzm_migration_validation
		SET schema_fingerprint = ?
		WHERE domain = 'intel'
	`, "intel-schema-v62-2026-09-05")
	require.NoError(t, err)
	require.NoError(t, store.Close())

	reader, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
	require.Error(t, err)
	require.Nil(t, reader)
	require.True(t, IsSchemaIncompatibleError(err), err)

	store, err = Open(path)
	require.NoError(t, err)
	got, err := store.RationaleForPath(ctx, want.Path)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.Rationale{want}, got)
	require.NoError(t, store.Close())

	reader, err = OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	got, err = reader.RationaleForPath(ctx, want.Path)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.Rationale{want}, got)
	require.NoError(t, reader.Close())
}
