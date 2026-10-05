package sqliteutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLockSchemaInitIsExclusiveUntilReleased(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing", "nested", "db.sqlite")
	release, err := LockSchemaInit(context.Background(), dbPath)
	require.NoError(t, err)
	defer func() {
		if release != nil {
			release()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = LockSchemaInit(ctx, dbPath)
	require.ErrorIs(t, err, context.DeadlineExceeded, "a second holder waits while the first holds the lock")

	waitCtx, stopWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopWait()
	acquired := make(chan error, 1)
	go func() {
		second, err := LockSchemaInit(waitCtx, dbPath)
		if err == nil {
			second()
		}
		acquired <- err
	}()
	release()
	release = nil
	require.NoError(t, <-acquired, "the waiter must acquire after release")
}

func TestLockSchemaInitContendsAcrossDatabaseAliases(t *testing.T) {
	for _, tc := range []struct {
		name            string
		directoryAlias  bool
		missingDatabase bool
	}{
		{name: "file"},
		{name: "directory", directoryAlias: true},
		{name: "directory_missing_database", directoryAlias: true, missingDatabase: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "db.sqlite")
			if !tc.missingDatabase {
				require.NoError(t, os.WriteFile(dbPath, nil, 0o600))
			}
			alias := filepath.Join(dir, "alias.sqlite")
			target := dbPath
			if tc.directoryAlias {
				target = dir
				alias = filepath.Join(t.TempDir(), "alias")
			}
			if err := os.Symlink(target, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if tc.directoryAlias {
				alias = filepath.Join(alias, "db.sqlite")
			}
			if !tc.missingDatabase {
				require.Equal(t, DSN(dbPath), DSN(alias))
			}

			for _, firstPath := range []string{dbPath, alias} {
				name := "real_holds_alias_waits"
				secondPath := alias
				if firstPath == alias {
					name = "alias_holds_real_waits"
					secondPath = dbPath
				}
				t.Run(name, func(t *testing.T) {
					release, err := LockSchemaInit(context.Background(), firstPath)
					require.NoError(t, err)
					defer func() {
						if release != nil {
							release()
						}
					}()

					ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
					defer cancel()
					second, err := LockSchemaInit(ctx, secondPath)
					if second != nil {
						second()
					}
					require.ErrorIs(t, err, context.DeadlineExceeded)

					release()
					release = nil
					waitCtx, stopWait := context.WithTimeout(context.Background(), 5*time.Second)
					defer stopWait()
					second, err = LockSchemaInit(waitCtx, secondPath)
					require.NoError(t, err)
					second()
				})
			}
		})
	}
}

func TestLockSchemaInitForDBContendsWithThePathLock(t *testing.T) {
	// A shared-pool open must exclude an own-pool open of the same file even
	// when SQLite reports the path differently (resolved symlinks, Windows
	// short names).
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := OpenDSN(DSN(dbPath), Options{})
	require.NoError(t, err)
	defer db.Close()

	reported, err := MainDatabasePath(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, mustEvalSymlinks(t, dbPath), mustEvalSymlinks(t, reported))

	release, err := LockSchemaInit(context.Background(), dbPath)
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = LockSchemaInitForDB(ctx, db)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLockSchemaInitForDBContendsWithAliasPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := OpenDSN(DSN(dbPath), Options{})
	require.NoError(t, err)
	defer db.Close()
	alias := filepath.Join(filepath.Dir(dbPath), "alias.sqlite")
	if err := os.Symlink(dbPath, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.Equal(t, DSN(dbPath), DSN(alias))

	for _, pathHolds := range []bool{true, false} {
		name := "alias_holds_pool_waits"
		if !pathHolds {
			name = "pool_holds_alias_waits"
		}
		t.Run(name, func(t *testing.T) {
			var release func()
			var err error
			if pathHolds {
				release, err = LockSchemaInit(context.Background(), alias)
			} else {
				release, err = LockSchemaInitForDB(context.Background(), db)
			}
			require.NoError(t, err)
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			var second func()
			if pathHolds {
				second, err = LockSchemaInitForDB(ctx, db)
			} else {
				second, err = LockSchemaInit(ctx, alias)
			}
			if second != nil {
				second()
			}
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}
