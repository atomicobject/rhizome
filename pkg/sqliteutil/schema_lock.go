package sqliteutil

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// schemaLockSuffix names the sidecar that serializes creation and migration of
// a database file. It sits next to the database like the WAL and SHM files.
const schemaLockSuffix = ".init.lock"

// errSchemaLockBusy is returned by tryLockFile while another handle holds the
// lock.
var errSchemaLockBusy = errors.New("schema lock held")

// LockSchemaInit serializes database creation and schema migration across
// processes and connection pools. SQLite alone cannot: the switch into WAL
// mode and a deferred migration transaction both upgrade a read lock to a
// write lock, which SQLite refuses immediately instead of waiting for the
// busy timeout, and two migrators that read the same schema version apply
// the same non-idempotent steps. Callers hold the lock from before the first
// connection opens until the schema is validated, so a concurrent opener
// finds a finished database.
//
// The wait is unbounded but honours ctx; the lock releases when the holder
// exits, even by crash. A filesystem that refuses the lock degrades to the
// unserialized open rather than failing.
func LockSchemaInit(ctx context.Context, dbPath string) (release func(), err error) {
	if dbPath == "" {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath, err := schemaLockPath(dbPath)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := tryLockFile(file)
		if err == nil {
			return func() {
				_ = unlockFile(file)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, errSchemaLockBusy) {
			// Best effort: an unsupported filesystem keeps the pre-lock behavior.
			_ = file.Close()
			return func() {}, nil
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// schemaLockPath uses the DSN's database identity, falling back to the
// canonical parent directory when the database does not exist yet.
func schemaLockPath(dbPath string) (string, error) {
	dbPath = paths.ResolveSymlinks(dbPath).String()
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Join(dir, filepath.Base(dbPath)+schemaLockSuffix), nil
}

// LockSchemaInitForDB is LockSchemaInit for a pool that is already open, as
// when a second store domain bootstraps its schema on a shared unified
// database. In-memory databases need no lock.
func LockSchemaInitForDB(ctx context.Context, db *sql.DB) (release func(), err error) {
	path, err := MainDatabasePath(ctx, db)
	if err != nil {
		return nil, err
	}
	return LockSchemaInit(ctx, path)
}

// MainDatabasePath reports the file behind the pool's main database, or ""
// for an in-memory or temporary database.
func MainDatabasePath(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("sqlite db is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file sql.NullString
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name.String == "main" {
			return file.String, rows.Err()
		}
	}
	return "", rows.Err()
}
