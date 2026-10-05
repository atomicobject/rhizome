package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/userstate"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCopyRhizomeIndexSnapshotsTheSourceWhileItIsStillBeingWritten(t *testing.T) {
	t.Parallel()

	sourceDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	targetDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	writer := openTestNotesDB(t, sourceDB, 40)
	defer writer.Close()
	_, err := os.Stat(sourceDB + "-wal")
	require.NoError(t, err, "the writer is open, so the rows live in the WAL")
	require.NoError(t, os.MkdirAll(filepath.Dir(targetDB), 0o755))
	require.NoError(t, os.WriteFile(targetDB, []byte("old-db"), 0o644))
	require.NoError(t, os.WriteFile(targetDB+"-wal", []byte("old-wal"), 0o644))

	_, err = copyRhizomeIndex(context.Background(), sourceDB, targetDB)
	require.NoError(t, err)

	requireNoteCount(t, targetDB, 40)
}

func TestCopyRhizomeIndexFallsBackToOneSnapshotFileWhenCloningFails(t *testing.T) {
	sourceDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	targetDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	writer := openTestNotesDB(t, sourceDB, 12)
	defer writer.Close()
	orig := cloneRhizomeIndex
	defer func() { cloneRhizomeIndex = orig }()
	cloneRhizomeIndex = func(context.Context, string, string) error { return errors.ErrUnsupported }

	cloned, err := copyRhizomeIndex(context.Background(), sourceDB, targetDB)
	require.NoError(t, err)

	require.False(t, cloned)
	requireNoteCount(t, targetDB, 12)
	_, err = os.Stat(targetDB + "-wal")
	require.ErrorIs(t, err, os.ErrNotExist, "a snapshot is one checkpointed file")
}

func TestCopyRhizomeIndexThroughASymlinkedSourceNeverWritesTheSource(t *testing.T) {
	t.Parallel()

	realDB := filepath.Join(t.TempDir(), "real", "db.sqlite")
	require.NoError(t, openTestNotesDB(t, realDB, 4).Close())
	sourceDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourceDB), 0o755))
	require.NoError(t, os.Symlink(realDB, sourceDB))
	targetDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")

	_, err := copyRhizomeIndex(context.Background(), sourceDB, targetDB)
	require.NoError(t, err)
	info, err := os.Lstat(targetDB)
	require.NoError(t, err)
	require.Zero(t, info.Mode()&os.ModeSymlink, "the target must be its own file")
	target, err := sqliteutil.OpenDSN(sqliteutil.DSN(targetDB), sqliteutil.Options{})
	require.NoError(t, err)
	_, err = target.Exec(`INSERT INTO notes (title) VALUES ('target only')`)
	require.NoError(t, err)
	require.NoError(t, target.Close())

	requireNoteCount(t, realDB, 4)
	requireNoteCount(t, targetDB, 5)
}

// A clone taken while one connection commits ten-row transactions and another
// runs passive checkpoints must open as a valid database holding whole
// transactions only.
func TestCloneIntoIsConsistentWhileTheSourceIsWrittenAndCheckpointed(t *testing.T) {
	t.Parallel()

	sourceDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	writer := openTestNotesDB(t, sourceDB, 0)
	defer writer.Close()
	checkpointer, err := sqliteutil.OpenDSN(sqliteutil.DSN(sourceDB), sqliteutil.Options{})
	require.NoError(t, err)
	defer checkpointer.Close()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var background sync.WaitGroup
	background.Add(2)
	go func() {
		defer background.Done()
		for ctx.Err() == nil {
			tx, err := writer.BeginTx(ctx, nil)
			if err != nil {
				continue
			}
			for i := 0; i < 10; i++ {
				_, _ = tx.Exec(`INSERT INTO notes (title) VALUES (?)`, strings.Repeat("n", 500))
			}
			_ = tx.Commit()
			time.Sleep(time.Millisecond)
		}
	}()
	go func() {
		defer background.Done()
		for ctx.Err() == nil {
			_, _ = checkpointer.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < 20; i++ {
		targetDB := filepath.Join(t.TempDir(), "db.sqlite")
		err := sqliteutil.CloneInto(context.Background(), sourceDB, targetDB)
		if errors.Is(err, errors.ErrUnsupported) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EINVAL) {
			stop()
			background.Wait()
			t.Skipf("the test filesystem cannot clone files: %v", err)
		}
		require.NoError(t, err)
		target, err := sqliteutil.OpenDSN(sqliteutil.DSN(targetDB), sqliteutil.Options{})
		require.NoError(t, err)
		var integrity string
		var count int
		require.NoError(t, target.QueryRow(`PRAGMA integrity_check`).Scan(&integrity))
		require.NoError(t, target.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&count))
		require.NoError(t, target.Close())
		require.Equal(t, "ok", integrity)
		require.Zero(t, count%10, "a clone must hold whole transactions")
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	background.Wait()
}

func TestCopyRhizomeIndexRemovesStaleTargetSidecars(t *testing.T) {
	t.Parallel()

	sourceDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	targetDB := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	require.NoError(t, openTestNotesDB(t, sourceDB, 3).Close())
	require.NoError(t, os.MkdirAll(filepath.Dir(targetDB), 0o755))
	require.NoError(t, os.WriteFile(targetDB+"-wal", []byte("stale-wal"), 0o644))
	require.NoError(t, os.WriteFile(targetDB+"-shm", []byte("stale-shm"), 0o644))

	_, err := copyRhizomeIndex(context.Background(), sourceDB, targetDB)
	require.NoError(t, err)

	requireNoteCount(t, targetDB, 3)
	for path, stale := range map[string]string{targetDB + "-wal": "stale-wal", targetDB + "-shm": "stale-shm"} {
		data, err := os.ReadFile(path)
		if !errors.Is(err, os.ErrNotExist) {
			require.NoError(t, err)
			require.NotEqual(t, stale, string(data))
		}
	}
}

func TestRunNewWorktreeRejectsSameDatabasePath(t *testing.T) {
	t.Parallel()

	db := filepath.Join(t.TempDir(), ".rhizome", "db.sqlite")
	err := runNewWorktree(testCobraCommand(), newWorktreeIndexSource{DBPath: db}, newWorktreeIndexTarget{DBPath: db})
	require.EqualError(t, err, "source and target resolve to the same Rhizome database: "+db)
}

func TestRunNewWorktreeCopiesThenIndexesTarget(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	sourceDB := filepath.Join(source, ".rhizome", "db.sqlite")
	targetDB := filepath.Join(target, ".rhizome", "db.sqlite")
	require.NoError(t, openTestNotesDB(t, sourceDB, 5).Close())

	sourceState, err := userstate.Open(context.Background(), source)
	require.NoError(t, err)
	defer sourceState.Close()
	scope := userstate.Scope{ViewID: "native", Context: userstate.Context{Kind: viewconfig.MountKindGroup, Group: "work"}}
	preferences, err := sourceState.Patch(context.Background(), scope, 0, map[string]json.RawMessage{"collapsed": json.RawMessage(`true`)}, nil)
	require.NoError(t, err)

	origRunIndex := newWorktreeRunIndex
	defer func() { newWorktreeRunIndex = origRunIndex }()

	var indexedPath string
	newWorktreeRunIndex = func(_ *cobra.Command, vaultPath string, _ obsidian.VaultDefinition) error {
		indexedPath = vaultPath
		targetState, err := userstate.Open(context.Background(), vaultPath)
		require.NoError(t, err)
		defer targetState.Close()
		copied, err := targetState.Read(context.Background(), scope)
		require.NoError(t, err)
		require.Equal(t, preferences, copied, "preferences are seeded before indexing starts")
		return nil
	}

	cmd := testCobraCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err = runNewWorktree(cmd,
		newWorktreeIndexSource{Root: source, DBPath: sourceDB},
		newWorktreeIndexTarget{Root: target, DBPath: targetDB},
	)
	require.NoError(t, err)
	requireNoteCount(t, targetDB, 5)
	require.Equal(t, target, indexedPath)
	require.Regexp(t, `(Copied|Cloned) Rhizome index from `+regexp.QuoteMeta(sourceDB), stderr.String())
	require.Contains(t, stderr.String(), "Rhizome index is up to date.")
}

func TestRunNewWorktreeWaitsForTargetWriterBeforeReplacingDatabase(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	sourceDB := filepath.Join(source, ".rhizome", "db.sqlite")
	targetDB := filepath.Join(target, ".rhizome", "db.sqlite")
	require.NoError(t, openTestNotesDB(t, sourceDB, 1).Close())
	require.NoError(t, os.MkdirAll(filepath.Dir(targetDB), 0o755))
	require.NoError(t, os.WriteFile(targetDB, []byte("active target"), 0o644))
	require.NoError(t, os.WriteFile(targetDB+"-wal", []byte("active wal"), 0o644))

	releaseWriter, acquired, err := indexlock.TryAcquire(obsidian.IndexLockPath(target))
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseWriter()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	cmd := testCobraCommand()
	cmd.SetContext(ctx)
	err = runNewWorktree(cmd,
		newWorktreeIndexSource{Root: source, DBPath: sourceDB},
		newWorktreeIndexTarget{Root: target, DBPath: targetDB},
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	got, err := os.ReadFile(targetDB)
	require.NoError(t, err)
	require.Equal(t, "active target", string(got))
	got, err = os.ReadFile(targetDB + "-wal")
	require.NoError(t, err)
	require.Equal(t, "active wal", string(got))
}

func TestResolveNewWorktreeSourceUsesLocalConfigRoot(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	nested := filepath.Join(source, "docs", "nested")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, obsidian.SaveLocalConfig(source, obsidian.LocalConfig{
		IndexPath: filepath.Join("cache", "custom.sqlite"),
	}))

	resolved, err := resolveNewWorktreeSource(nested)
	require.NoError(t, err)
	sourceResolved, err := filepath.EvalSymlinks(source)
	require.NoError(t, err)
	require.Equal(t, sourceResolved, resolved.Root)
	require.Equal(t, filepath.Join(sourceResolved, "cache", "custom.sqlite"), resolved.DBPath)
}

// openTestNotesDB creates a WAL-mode database with rows notes and returns the
// open writer so the rows stay in the WAL until it closes.
func openTestNotesDB(t *testing.T, path string, rows int) *sql.DB {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT)`)
	require.NoError(t, err)
	for i := 0; i < rows; i++ {
		_, err = db.Exec(`INSERT INTO notes (title) VALUES (?)`, "note")
		require.NoError(t, err)
	}
	return db
}

func requireNoteCount(t *testing.T, path string, want int) {
	t.Helper()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{ReadOnly: true}), sqliteutil.Options{})
	require.NoError(t, err)
	defer db.Close()
	var got int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&got))
	require.Equal(t, want, got)
}

func testCobraCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())
	return cmd
}
