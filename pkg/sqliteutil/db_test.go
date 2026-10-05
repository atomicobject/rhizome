package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
)

func txLockFromDSN(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	return u.Query().Get("_txlock")
}

func TestDSNWithOptions_UsesExplicitTxLock(t *testing.T) {
	dsn := DSNWithOptions("/tmp/test.db", DSNOptions{TxLockMode: TxLockImmediate})
	if got := txLockFromDSN(t, dsn); got != TxLockImmediate {
		t.Fatalf("expected txlock %q, got %q", TxLockImmediate, got)
	}
}

func TestDSNWithOptions_InvalidTxLockIsIgnored(t *testing.T) {
	dsn := DSNWithOptions("/tmp/test.db", DSNOptions{TxLockMode: "bad"})
	if got := txLockFromDSN(t, dsn); got != "" {
		t.Fatalf("expected empty txlock, got %q", got)
	}
}

func TestDSNWithOptions_ReadOnlyUsesOpenExistingQueryOnlyMode(t *testing.T) {
	dsn := DSNWithOptions("/tmp/test.db", DSNOptions{
		ReadOnly:   true,
		TxLockMode: TxLockExclusive,
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	query := u.Query()
	if got := query.Get("mode"); got != "ro" {
		t.Fatalf("expected mode=ro, got %q", got)
	}
	if got := query.Get("_query_only"); got != "on" {
		t.Fatalf("expected query_only=on, got %q", got)
	}
	if got := query.Get("_journal_mode"); got != "" {
		t.Fatalf("read-only open must not set journal mode, got %q", got)
	}
	if got := query.Get("_txlock"); got != "" {
		t.Fatalf("read-only open must not set transaction write lock, got %q", got)
	}
}

func TestDSNWithOptions_ExistingWriteRequiresExistingDatabase(t *testing.T) {
	dsn := DSNWithOptions("/tmp/test.db", DSNOptions{ExistingWrite: true})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	query := u.Query()
	if got := query.Get("mode"); got != "rw" {
		t.Fatalf("expected mode=rw, got %q", got)
	}
	if got := query.Get("_query_only"); got != "" {
		t.Fatalf("existing-write open must permit writes, got query_only=%q", got)
	}
}

func TestDSNIgnoresLegacyTxLockEnvironmentVariable(t *testing.T) {
	t.Setenv("RHIZOME_SQLITE_TXLOCK", TxLockExclusive)
	dsn := DSN("/tmp/test.db")
	if got := txLockFromDSN(t, dsn); got != "" {
		t.Fatalf("expected empty txlock, got %q", got)
	}
}

func TestOpenDSN_DefaultMmapSizeApplied(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mmap-default.db")
	t.Setenv(sqliteMmapSizeEnv, "")

	db, err := OpenDSN(DSN(dbPath), Options{MaxOpenConns: 2})
	if err != nil {
		t.Fatalf("open dsn: %v", err)
	}
	defer db.Close()

	var conns []*sql.Conn
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("open conn %d: %v", i+1, err)
		}
		conns = append(conns, conn)
		var mmapSize int64
		if err := conn.QueryRowContext(ctx, `PRAGMA mmap_size;`).Scan(&mmapSize); err != nil {
			t.Fatalf("query mmap_size on conn %d: %v", i+1, err)
		}
		if mmapSize != 1<<30 {
			t.Fatalf("expected mmap_size=%d on conn %d, got %d", 1<<30, i+1, mmapSize)
		}
	}
}

func TestOpenDSN_MmapSizeOverrideFromEnv(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mmap-override.db")
	t.Setenv(sqliteMmapSizeEnv, "0")

	db, err := OpenDSN(DSN(dbPath), Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open dsn: %v", err)
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open conn: %v", err)
	}
	defer conn.Close()

	var mmapSize int64
	if err := conn.QueryRowContext(ctx, `PRAGMA mmap_size;`).Scan(&mmapSize); err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmapSize != 0 {
		t.Fatalf("expected mmap_size=0 with override, got %d", mmapSize)
	}
}

func TestOpenDSN_LoadsSQLiteVec(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vec.db")
	db, err := OpenDSN(DSN(dbPath), Options{})
	if err != nil {
		t.Fatalf("open dsn: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	probe := []byte{0x00, 0x00, 0x80, 0x3f}
	var dist float64
	if err := db.QueryRowContext(context.Background(), `SELECT vec_distance_cosine(?, ?)`, probe, probe).Scan(&dist); err != nil {
		t.Fatalf("sqlite-vec query: %v", err)
	}
	if dist != 0 {
		t.Fatalf("expected cosine distance 0, got %f", dist)
	}
}

func TestOpenDSN_SQLiteIncludesWALResetCorruptionFix(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sqlite-version.db")
	db, err := OpenDSN(DSN(dbPath), Options{})
	if err != nil {
		t.Fatalf("open dsn: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var version string
	if err := db.QueryRowContext(context.Background(), `SELECT sqlite_version()`).Scan(&version); err != nil {
		t.Fatalf("query sqlite version: %v", err)
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		t.Fatalf("parse sqlite version %q: %v", version, err)
	}
	if major < 3 || (major == 3 && (minor < 51 || (minor == 51 && patch < 3))) {
		t.Fatalf("SQLite %s predates the WAL-reset corruption fix in 3.51.3", version)
	}
}
