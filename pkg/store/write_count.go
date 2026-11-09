package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// writeSequence is a sequence lock over every withWrite and withWriteTx in
// this process (SPEC-0136 R4). Its low 32 bits count write transactions that
// are open, its high 32 bits count finished ones. A write adds 1 when it
// starts and 1<<32-1 when it returns, success or not, while its write mutex
// is still held, so the value only grows and every start and finish changes
// it. A reader that sees no open write and the same value before and after
// its read saw no commit in between, including one whose finish has not been
// counted yet. It is process-wide because several Store handles share one
// database file; the open count, not an odd value, marks open writes because
// handles on different files can write at once.
// ponytail: one sequence for every database this process writes, so a write
// to one vault invalidates cached reads of another; key it by file if a
// process ever serves several busy vaults.
var writeSequence atomic.Uint64

const writesOpenMask = 1<<32 - 1

func beginWrite() { writeSequence.Add(1) }

func endWrite() { writeSequence.Add(writesOpenMask) }

// WriteSequence reports the write sequence. Callers compare values for
// equality only, and must not trust a read made while WritesOpen.
func WriteSequence() uint64 {
	return writeSequence.Load()
}

// WritesOpen reports whether a write transaction was open when sequence was
// read.
func WritesOpen(sequence uint64) bool {
	return sequence&writesOpenMask != 0
}

// DataVersion reads PRAGMA data_version on one connection reserved for it.
// The value changes whenever another connection, in this process or another,
// commits to the database, so it witnesses writes the process-wide sequence
// cannot see (a note operation or validation projection run by another
// process). Values are compared for equality only.
type DataVersion struct {
	mu   sync.Mutex
	db   *sql.DB
	conn *sql.Conn
}

// OpenDataVersion opens a read-only connection to the store's main database
// file that only DataVersion.Read uses.
func (s *Store) OpenDataVersion(ctx context.Context) (*DataVersion, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("store is not open")
	}
	var seq int
	var name, file string
	if err := s.db.QueryRowContext(ctx, `SELECT seq, name, file FROM pragma_database_list WHERE name = 'main'`).Scan(&seq, &name, &file); err != nil {
		return nil, fmt.Errorf("locate database file: %w", err)
	}
	if file == "" {
		return nil, errors.New("database has no file")
	}
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(file, sqliteutil.DSNOptions{ReadOnly: true}), sqliteutil.Options{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return nil, err
	}
	// One pinned connection: data_version compares commits against the
	// connection that read it, so it must never change connections.
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DataVersion{db: db, conn: conn}, nil
}

// Read returns the connection's current data_version.
func (d *DataVersion) Read(ctx context.Context) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var version int64
	err := d.conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&version)
	return version, err
}

// Close releases the reserved connection.
func (d *DataVersion) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return errors.Join(d.conn.Close(), d.db.Close())
}
