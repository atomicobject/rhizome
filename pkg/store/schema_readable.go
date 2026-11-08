package store

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// CheckReadable reports, without changing data, whether this build can read
// the Intel schema of the existing index at path: the schema the runtime's
// readiness gate needs. It returns *migration.ErrFutureSchema when a newer
// Rhizome wrote it; an index without schema state is readable, since this
// build creates or resets it. Embedding domains are not checked. Like any
// SQLite reader, it may leave the WAL sidecar files in place.
func CheckReadable(ctx context.Context, path string) error {
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{ReadOnly: true, NoBusyWait: true, BusyTimeoutMs: 100}), sqliteutil.Options{})
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = (&Store{db: db}).classifyIntelBaseline(ctx, currentSchemaVersion)
	return err
}
