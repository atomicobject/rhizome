package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"os"
	"path/filepath"
)

// OpenOptions controls SQLite store open behavior.
type OpenOptions struct {
	// Context carries diagnostics and cancellation through schema initialization.
	// Nil uses context.Background for compatibility with path-only callers.
	Context context.Context
	// TxLockMode sets sqlite _txlock mode (supported: immediate, exclusive).
	TxLockMode string
	// Pool controls SQLite connection pooling.
	Pool sqliteutil.Options
}

// Open opens or creates the SQLite store at the provided path.
func Open(path string) (*Store, error) {
	return OpenWithOptions(path, OpenOptions{})
}

// OpenWithOptions opens or creates the SQLite store at the provided path.
func OpenWithOptions(path string, opts OpenOptions) (*Store, error) {
	return openWithOptionsAtSchemaVersion(path, opts, currentSchemaVersion)
}

// OpenReadOnlyExisting opens a current, previously validated index without
// creating directories, migrating schema, repairing state, or permitting writes.
func OpenReadOnlyExisting(path string, ctx context.Context, pool sqliteutil.Options) (*Store, error) {
	return openValidatedExisting(path, ctx, pool, true)
}

// OpenSessionStoreExisting opens a current, previously validated index for
// session persistence without creating directories, migrating schema, repairing
// state, or running an integrity check. Callers must expose this handle only to
// the session-dedupe boundary; indexed retrieval uses OpenReadOnlyExisting.
func OpenSessionStoreExisting(path string, ctx context.Context, pool sqliteutil.Options) (SessionDedupeHandle, error) {
	store, err := openValidatedExisting(path, ctx, pool, false)
	if err != nil {
		return nil, err
	}
	return newBoundedSessionDedupeHandle(store), nil
}

func openValidatedExisting(path string, ctx context.Context, pool sqliteutil.Options, readOnly bool) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{
		ReadOnly:      readOnly,
		ExistingWrite: !readOnly,
		// Managed readers must not wait inside SQLite, including during open
		// before context-aware validation. Session writes retain a short wait.
		NoBusyWait:    readOnly,
		BusyTimeoutMs: 100,
	}), pool)
	if err != nil {
		return nil, err
	}
	store := &Store{
		db:        db,
		closeFn:   db.Close,
		writeInfo: writeStats{label: "intel-store"},
		readOnly:  readOnly,
	}
	if err := store.validateExistingSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func openWithOptionsAtSchemaVersion(path string, opts OpenOptions, schemaVersion int) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	openCtx := opts.Context
	if openCtx == nil {
		openCtx = context.Background()
	}
	// One creator or migrator per database file, held from before the first
	// connection (the WAL switch) through schema validation.
	releaseSchemaLock, err := sqliteutil.LockSchemaInit(openCtx, path)
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{
		TxLockMode: opts.TxLockMode,
	}), opts.Pool)
	if err != nil {
		return nil, err
	}

	store := &Store{
		db:        db,
		closeFn:   db.Close,
		writeInfo: writeStats{label: "intel-store"},
	}
	if err := store.ensureSchemaWithRecovery(openCtx, schemaVersion); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// OpenWithDB opens the store using an existing SQLite handle.
func OpenWithDB(db *sql.DB) (*Store, error) {
	return OpenWithDBContext(context.Background(), db)
}

// OpenWithDBContext opens the store using an existing SQLite handle while
// retaining caller diagnostics and cancellation during schema initialization.
func OpenWithDBContext(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlite db is required")
	}
	store := &Store{
		db:        db,
		closeFn:   func() error { return nil },
		writeInfo: writeStats{label: "intel-store"},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	releaseSchemaLock, err := sqliteutil.LockSchemaInitForDB(ctx, db)
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock()
	if err := store.ensureSchemaWithRecovery(ctx, currentSchemaVersion); err != nil {
		return nil, err
	}
	return store, nil
}

// Close releases resources.
func (s *Store) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// Checkpoint flushes WAL writes to the main database file.
// Use truncate=true for aggressive checkpoint that truncates the WAL.
func (s *Store) Checkpoint(ctx context.Context, truncate bool) error {
	return sqliteutil.Checkpoint(ctx, s.db, truncate)
}
