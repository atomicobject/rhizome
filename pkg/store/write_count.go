package store

import (
	"context"
	"strings"
	"sync/atomic"
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

// bumpWriteGeneration advances the write counter. Writes that run before
// the counter's migration (baseline bootstrap, embedding cache repair) find
// no table; with no counter to read, readers already treat the generation as
// unwitnessed, so there is nothing to advance.
func bumpWriteGeneration(ctx context.Context, db execer) error {
	_, err := db.ExecContext(ctx, `UPDATE write_generation SET value = value + 1 WHERE id = 1`)
	if err != nil && strings.Contains(err.Error(), "no such table: write_generation") {
		return nil
	}
	return err
}

// WriteGeneration reads the counter every withWrite and withWriteTx advances,
// inside the write's transaction where it has one. Unlike the process-wide
// sequence it witnesses store writes from other processes, and unlike
// PRAGMA data_version it ignores the embedding stores, which share the
// database file but write through their own runtime. Values are compared for
// equality only.
func (s *Store) WriteGeneration(ctx context.Context) (int64, error) {
	var value int64
	err := s.db.QueryRowContext(ctx, `SELECT value FROM write_generation WHERE id = 1`).Scan(&value)
	return value, err
}
