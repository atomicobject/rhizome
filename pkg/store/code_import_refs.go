package store

import (
	"context"
	"database/sql"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

// ReplaceIntelImportRefsForPathsBatch replaces import-ref rows for multiple paths in a single transaction.
func (s *Store) ReplaceIntelImportRefsForPathsBatch(ctx context.Context, batches map[string][]codeanchor.ImportRefRow) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_import_refs")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts := []string{
			`DROP TABLE IF EXISTS temp_intel_import_ref_paths;`,
			`DROP TABLE IF EXISTS temp_intel_import_ref_batch;`,
			`CREATE TEMP TABLE temp_intel_import_ref_paths (
				src_path TEXT PRIMARY KEY
			) WITHOUT ROWID;`,
			`CREATE TEMP TABLE temp_intel_import_ref_batch (
				src_path TEXT NOT NULL,
				module TEXT NOT NULL,
				PRIMARY KEY (src_path, module)
			) WITHOUT ROWID;`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		defer func() {
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_import_ref_paths`)
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_import_ref_batch`)
		}()

		pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_import_ref_paths(src_path) VALUES (?)`)
		if err != nil {
			return err
		}
		defer pathStmt.Close()

		batchStmt, err := tx.PrepareContext(ctx, `
			INSERT OR IGNORE INTO temp_intel_import_ref_batch (src_path, module)
			VALUES (?, ?)
		`)
		if err != nil {
			return err
		}
		defer batchStmt.Close()

		for rawPath, rows := range batches {
			path := normalizeCodeLookupPath(rawPath)
			if path == "" {
				continue
			}
			if _, err := pathStmt.ExecContext(ctx, path); err != nil {
				return err
			}
			for _, row := range rows {
				if strings.TrimSpace(row.Module) == "" {
					continue
				}
				if _, err := batchStmt.ExecContext(ctx, path, row.Module); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_import_refs
			WHERE src_path IN (SELECT src_path FROM temp_intel_import_ref_paths)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_import_refs (src_path, module)
			SELECT src_path, module
			FROM temp_intel_import_ref_batch
		`); err != nil {
			return err
		}
		return nil
	})
}
