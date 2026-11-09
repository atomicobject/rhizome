package store

import "sync/atomic"

// writeCount advances after every withWrite and withWriteTx returns, success or
// not, while the write mutex is still held, so a reader that observes the new
// count also observes the commit (SPEC-0136 R4). It is process-wide because
// several Store handles share one database file.
// ponytail: one counter for every database this process writes, so a write to
// one vault invalidates cached reads of another; key it by file if a process
// ever serves several busy vaults.
var writeCount atomic.Uint64

// WriteCount reports how many store write transactions have finished in this
// process. Callers compare values for equality only.
func WriteCount() uint64 {
	return writeCount.Load()
}
