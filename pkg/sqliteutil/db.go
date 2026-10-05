package sqliteutil

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// Options controls connection pooling for SQLite handles.
type Options struct {
	// MaxOpenConns limits concurrent connections. Defaults to 4 when unset or <=0.
	MaxOpenConns int
	// MaxIdleConns defaults to MaxOpenConns when unset or <0.
	MaxIdleConns int
	// ConnMaxIdleTime closes idle connections after the duration when >0.
	ConnMaxIdleTime time.Duration
}

// DSNOptions controls SQLite DSN generation.
type DSNOptions struct {
	// TxLockMode sets sqlite _txlock mode (supported: immediate, exclusive).
	// Empty leaves SQLite's default transaction start behavior unchanged.
	TxLockMode string
	// BusyTimeoutMs overrides sqlite _busy_timeout in milliseconds when >0.
	// Non-positive values retain the 30-second default.
	BusyTimeoutMs int
	// NoBusyWait disables the native SQLite busy wait, taking precedence over
	// BusyTimeoutMs. Callers receive contention errors immediately.
	NoBusyWait bool
	// ReadOnly opens an existing database without permitting writes.
	ReadOnly bool
	// ExistingWrite opens an existing database with writes enabled. Unlike the
	// default mode, SQLite must not create a missing database.
	ExistingWrite bool
}

// OpenDSN opens a SQLite database using the provided DSN and applies common pooling defaults.
// Caller is responsible for ensuring the DSN sets the desired pragmas (WAL, busy_timeout, etc.).
func OpenDSN(dsn string, opts Options) (*sql.DB, error) {
	if err := validateWALDSN(dsn); err != nil {
		return nil, err
	}
	if err := EnsureSQLiteVec(); err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriverName(), dsn)
	if err != nil {
		return nil, err
	}
	if err := verifySQLiteVec(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	maxOpen := opts.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 4
	}
	maxIdle := opts.MaxIdleConns
	if maxIdle < 0 {
		maxIdle = 0
	} else if maxIdle == 0 {
		maxIdle = maxOpen
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	if opts.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(opts.ConnMaxIdleTime)
	}
	return db, nil
}

const sqliteMmapSizeEnv = "RHIZOME_SQLITE_MMAP_SIZE"

const (
	TxLockImmediate = "immediate"
	TxLockExclusive = "exclusive"
	defaultMmapSize = int64(1 << 30) // 1 GiB
)

func normalizeTxLockMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case TxLockImmediate, TxLockExclusive:
		return mode
	default:
		return ""
	}
}

func mmapSizeFromEnv() int64 {
	raw := strings.TrimSpace(os.Getenv(sqliteMmapSizeEnv))
	if raw == "" {
		return defaultMmapSize
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return defaultMmapSize
	}
	return v
}

// DSN builds a SQLite DSN with common pragmas for mattn/go-sqlite3.
func DSN(path string) string {
	return DSNWithOptions(path, DSNOptions{})
}

// DSNWithOptions builds a SQLite DSN with common pragmas and optional overrides.
func DSNWithOptions(path string, opts DSNOptions) string {
	if resolved := paths.ResolveSymlinks(path); resolved != "" {
		path = resolved.String()
	}
	path = filepath.ToSlash(path)

	u := &url.URL{Scheme: "file"}
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
			path = "/" + path
		}
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u.Path = path

	// mattn/go-sqlite3 uses specific DSN keys for pragmas
	q := url.Values{}
	busyTimeout := opts.BusyTimeoutMs
	if busyTimeout <= 0 {
		busyTimeout = 30000
	}
	if opts.NoBusyWait {
		busyTimeout = 0
	}
	q.Set("_busy_timeout", strconv.Itoa(busyTimeout))
	q.Set("_foreign_keys", "on")
	q.Set("_cache_size", "10000")
	if opts.ReadOnly {
		q.Set("mode", "ro")
		q.Set("_query_only", "on")
	} else {
		if opts.ExistingWrite {
			q.Set("mode", "rw")
		}
		q.Set("_journal_mode", "WAL")
		q.Set("_synchronous", "NORMAL")
		if txLock := normalizeTxLockMode(opts.TxLockMode); txLock != "" {
			q.Set("_txlock", txLock)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Checkpoint performs a WAL checkpoint to flush pending writes to the main database file.
// Use PASSIVE mode for non-blocking checkpoint (skips if readers are active).
// Use TRUNCATE mode for aggressive checkpoint that truncates the WAL file.
func Checkpoint(ctx context.Context, db *sql.DB, truncate bool) error {
	mode := "PASSIVE"
	if truncate {
		mode = "TRUNCATE"
	}
	_, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint("+mode+")")
	return err
}
