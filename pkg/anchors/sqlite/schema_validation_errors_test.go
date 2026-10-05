package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestValidationReadFailurePreservesIntelRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	want := codeanchor.Rationale{ID: "seed", Path: "src/a.go", Kind: codeanchor.RationaleTodo, Content: "keep me", Fingerprint: "seed"}
	require.NoError(t, store.ReplaceRationaleForPath(ctx, want.Path, []codeanchor.Rationale{want}))
	_, err = store.db.ExecContext(ctx, `DELETE FROM rzm_migration_validation WHERE domain = 'intel'`)
	require.NoError(t, err)
	store.db.SetMaxOpenConns(1)
	denied := false
	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, table, column, database string) int {
			if !denied && op == sqlite3.SQLITE_READ && table == "intel_embeddings" && column == "dimensions" && database == "main" {
				denied = true
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())
	reopened, openErr := OpenWithDB(store.db)
	if reopened != nil {
		defer reopened.Close()
	}
	require.True(t, denied, "fault must reach actual schema validator")
	got, err := store.RationaleForPath(ctx, want.Path)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.Rationale{want}, got, "operational validation failure must preserve derived rows")
	require.Error(t, openErr)
	var drift *migration.ErrSchemaDrift
	require.False(t, errors.As(openErr, &drift))
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, openErr, &sqliteErr)
	require.Equal(t, sqlite3.ErrAuth, sqliteErr.Code)
	conn, err = store.db.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(nil)
		return nil
	}))
	require.NoError(t, conn.Close())
	reopened, err = OpenWithDB(store.db)
	require.NoError(t, err)
	defer reopened.Close()
	got, err = reopened.RationaleForPath(ctx, want.Path)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.Rationale{want}, got)
}

func TestManagedSchemaReadFailureDoesNotRequireRebuild(t *testing.T) {
	ctx := t.Context()
	store, err := Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	store.db.SetMaxOpenConns(1)
	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	denied := false
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, table, column, database string) int {
			if op == sqlite3.SQLITE_READ && table == "rzm_migration_validation" {
				denied = true
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())
	err = store.validateExistingSchema(ctx)
	require.True(t, denied, "the fault must reach the managed schema proof read")
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrAuth, sqliteErr.Code)
	require.False(t, IsSchemaIncompatibleError(err), "a failed read does not establish schema drift or authorize rebuild")
	conn, err = store.db.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(nil)
		return nil
	}))
	require.NoError(t, conn.Close())
	require.NoError(t, store.validateExistingSchema(ctx), "the existing proof works once reading succeeds, without a rebuild")
}

func TestManagedSchemaRejectsUnavailableOrIncompatibleProof(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"missing proof row", `DELETE FROM rzm_migration_validation WHERE domain = 'intel'`},
		{"missing proof table", `DROP TABLE rzm_migration_validation`},
		{"mismatched proof", `UPDATE rzm_migration_validation SET sqlite_schema_version = -1 WHERE domain = 'intel'`},
		{"future schema", `UPDATE rzm_migration_state SET version = 999999 WHERE domain = 'intel'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "intel.db")
			store, err := Open(path)
			require.NoError(t, err)
			_, err = store.db.ExecContext(t.Context(), tc.mutation)
			require.NoError(t, err)
			require.NoError(t, store.Close())
			reader, err := OpenReadOnlyExisting(path, t.Context(), sqliteutil.Options{})
			if reader != nil {
				defer reader.Close()
			}
			require.True(t, IsSchemaIncompatibleError(err), "unusable proof must still be classified as incompatible: %v", err)
			if tc.name == "future schema" {
				var future *migration.ErrFutureSchema
				require.ErrorAs(t, err, &future)
			}
		})
	}
}
