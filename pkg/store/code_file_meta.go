package store

import (
	"context"
	"database/sql"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
	"time"
)

// UpsertFileMeta updates per-file index metadata without touching symbol rows.
// Used to avoid wiping previously indexed data on transient parse failures/timeouts.
func (s *Store) UpsertFileMeta(ctx context.Context, meta codeanchor.FileMeta) error {
	path := strings.TrimSpace(meta.Path)
	if path == "" {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_file_meta")
	status := strings.TrimSpace(string(meta.ParseStatus))
	if status == "" {
		status = string(codeanchor.ParseOK)
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				lang=excluded.lang,
				hash=excluded.hash,
				indexer_version=excluded.indexer_version,
				parse_status=excluded.parse_status,
				mtime=excluded.mtime
		`, path, meta.Lang, meta.Hash, codeanchor.IndexerVersion, status, time.Now().Unix())
		if err != nil {
			return err
		}
		return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, nil, []codeanchor.FileMeta{meta})
	})
}
