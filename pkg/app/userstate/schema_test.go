package userstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"
	"github.com/stretchr/testify/require"
)

func TestOpenRefusesFuturePersonalSchemaWithoutSuggestingIndexRebuild(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	store := openTestStore(t, root)
	require.NoError(t, store.withWrite(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE rzm_migration_state SET version=3 WHERE domain='user_state'`)
		return err
	}))
	require.NoError(t, store.Close())
	opened, err := Open(ctx, root)
	if opened != nil {
		_ = opened.Close()
	}
	var future *migration.ErrFutureSchema
	require.ErrorAs(t, err, &future)
	require.Equal(t, migration.DomainUserState, future.Domain)
	require.NotContains(t, err.Error(), "index --rebuild")
}

func TestOpenMigratesV1PreferencesWithoutChangingExistingRevisions(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	path := filepath.Join(root, ".rhizome", "user-state.sqlite")
	release, err := sqliteutil.LockSchemaInit(ctx, path)
	require.NoError(t, err)
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{TxLockMode: sqliteutil.TxLockImmediate}), sqliteutil.Options{})
	require.NoError(t, err)
	plan := domains.UserStatePlan()
	plan.Target, plan.Steps, plan.Validate = 1, plan.Steps[:1], nil
	require.NoError(t, migration.EnsureDomain(ctx, db, plan, migration.EnsureOptions{}))
	_, key, err := CanonicalScope(root, testScope("work"))
	require.NoError(t, err)
	fixture := &Store{db: db, writeMu: &sharedWriter{}}
	require.NoError(t, fixture.withWrite(ctx, func(tx *sql.Tx) error {
		if err := saveScope(ctx, tx, key, 7, false); err != nil {
			return err
		}
		return setValues(ctx, tx, key, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)})
	}))
	require.NoError(t, db.Close())
	release()
	store := openTestStore(t, root)
	state, err := store.Read(ctx, testScope("work"))
	require.NoError(t, err)
	require.EqualValues(t, 7, state.Revision)
	require.False(t, state.MigrationClosed)
	require.JSONEq(t, `"compact"`, string(state.Values["density"]))
	_, err = store.ResetInstance(ctx, testScope("work"), 7)
	require.NoError(t, err)
}

func TestOpenRefusesSchemaDriftWithoutReplacingDurablePreferences(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	store := openTestStore(t, root)
	_, err := store.Patch(ctx, testScope("work"), 0, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)}, nil)
	require.NoError(t, err)
	require.NoError(t, store.withWrite(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `ALTER TABLE view_preferences RENAME TO original_preferences`)
		return err
	}))
	opened, err := Open(ctx, root)
	if opened != nil {
		_ = opened.Close()
	}
	var drift *migration.ErrSchemaDrift
	require.ErrorAs(t, err, &drift)
	var original string
	require.NoError(t, store.reader.QueryRowContext(ctx, `SELECT value_json FROM original_preferences WHERE preference_key='density'`).Scan(&original))
	require.JSONEq(t, `"compact"`, original)
}
