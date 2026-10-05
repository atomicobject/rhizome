package sqliteutil

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrityCheckFailed_Error(t *testing.T) {
	err := IntegrityCheckFailed{Result: "page 123 is corrupt"}
	assert.Equal(t, "sqlite integrity check: page 123 is corrupt", err.Error())
}

func TestQuickIntegrityCheck_ReturnsOKForValidDB(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", DSN(dbPath))
	require.NoError(t, err)
	defer db.Close()

	// Create a table to ensure the DB is valid
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	err = QuickIntegrityCheck(db)
	assert.NoError(t, err)
}

func TestQuickIntegrityCheck_ReturnsCorruptionForCorruptDB(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "corrupt.db")

	// Create a valid DB first
	db, err := sql.Open("sqlite3", DSN(dbPath))
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// Corrupt it by writing garbage to the middle
	data, err := os.ReadFile(dbPath)
	require.NoError(t, err)
	require.Greater(t, len(data), 200)
	for i := 100; i < 200; i++ {
		data[i] = 0xFF
	}
	require.NoError(t, os.WriteFile(dbPath, data, 0o644))

	// Open and check: SQLite can report corruption during the query itself.
	db2, err := sql.Open("sqlite3", DSN(dbPath))
	require.NoError(t, err)
	defer db2.Close()

	err = QuickIntegrityCheck(db2)
	require.Error(t, err)
	assert.True(t, IsCorruptError(err), "expected corruption, got %v", err)
}

func TestQuickIntegrityCheck_ExclusiveLockReturnsBusy(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "locked.db")
	db1, err := sql.Open("sqlite3", DSN(dbPath)+"&_locking_mode=EXCLUSIVE")
	require.NoError(t, err)
	defer db1.Close()

	_, err = db1.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	// Keep the exclusive connection open while another connection checks it.
	_, err = db1.Exec("INSERT INTO test VALUES (1)")
	require.NoError(t, err)

	db2, err := sql.Open("sqlite3", DSNWithOptions(dbPath, DSNOptions{NoBusyWait: true}))
	require.NoError(t, err)
	defer db2.Close()

	start := time.Now()
	err = QuickIntegrityCheck(db2)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 10*time.Second, "no-wait contention should finish promptly")
	require.Error(t, err)
	assert.True(t, IsBusyOrLocked(err), "expected SQLite contention, got %v", err)
	assert.False(t, IsCorruptError(err), "busy error must not be classified as corruption")
}

func TestIsCorruptError_IntegrityCheckFailed(t *testing.T) {
	t.Parallel()

	err := IntegrityCheckFailed{Result: "page 123 is corrupt"}
	assert.True(t, IsCorruptError(err))

	// Wrapped error should also be detected via errors.As
	wrapped := fmt.Errorf("open failed: %w", err)
	assert.True(t, IsCorruptError(wrapped), "wrapped IntegrityCheckFailed should be detected")
}

func TestIsCorruptError_SQLiteErrorCodes(t *testing.T) {
	t.Parallel()

	// Test SQLITE_CORRUPT
	corruptErr := sqlite3.Error{Code: sqlite3.ErrCorrupt}
	assert.True(t, IsCorruptError(corruptErr))

	// Test SQLITE_NOTADB
	notADBErr := sqlite3.Error{Code: sqlite3.ErrNotADB}
	assert.True(t, IsCorruptError(notADBErr))

	// Test other codes should NOT be corruption
	busyErr := sqlite3.Error{Code: sqlite3.ErrBusy}
	assert.False(t, IsCorruptError(busyErr))

	lockedErr := sqlite3.Error{Code: sqlite3.ErrLocked}
	assert.False(t, IsCorruptError(lockedErr))
}

func TestIsCorruptError_StringMatching(t *testing.T) {
	t.Parallel()

	// These strings should be detected as corruption
	assert.True(t, IsCorruptError(errors.New("database disk image is malformed")))
	assert.True(t, IsCorruptError(errors.New("malformed database schema")))
	assert.True(t, IsCorruptError(errors.New("file is not a database")))

	// These should NOT be detected as corruption
	assert.False(t, IsCorruptError(errors.New("database is locked")))
	assert.False(t, IsCorruptError(errors.New("database busy")))
	assert.False(t, IsCorruptError(errors.New("context deadline exceeded")))

	// The old "database integrity check failed" string should NOT match
	// (we now use typed errors for this)
	assert.False(t, IsCorruptError(errors.New("database integrity check failed: database is locked")))
}

func TestIsCorruptError_Nil(t *testing.T) {
	t.Parallel()
	assert.False(t, IsCorruptError(nil))
}
