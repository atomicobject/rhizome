package store

import (
	"context"
	"database/sql"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// ReplaceIntelSymbolRefsForPathsBatch replaces symbol-ref rows for multiple paths in a single transaction.
func (s *Store) ReplaceIntelSymbolRefsForPathsBatch(ctx context.Context, batches map[string][]codeanchor.SymbolRefRow) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_symbol_refs")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts := []string{
			`DROP TABLE IF EXISTS temp_symbol_ref_batch;`,
			`DROP TABLE IF EXISTS temp_symbol_ref_paths;`,
			`CREATE TEMP TABLE temp_symbol_ref_batch (
				src_path TEXT NOT NULL,
				owner_fqn TEXT NOT NULL,
				ref_kind TEXT NOT NULL,
				dst_lang TEXT NOT NULL,
				dst_pkg TEXT NOT NULL,
				dst_name TEXT NOT NULL,
				dst_fqn TEXT NOT NULL,
				dst_member INTEGER NOT NULL
			);`,
			`CREATE TEMP TABLE temp_symbol_ref_paths (
				src_path TEXT PRIMARY KEY
			) WITHOUT ROWID;`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		defer func() {
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_symbol_ref_batch`)
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_symbol_ref_paths`)
		}()

		pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_symbol_ref_paths(src_path) VALUES (?)`)
		if err != nil {
			return err
		}
		defer pathStmt.Close()

		batchStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO temp_symbol_ref_batch (
				src_path, owner_fqn, ref_kind, dst_lang, dst_pkg, dst_name, dst_fqn, dst_member
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
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
				if _, err := batchStmt.ExecContext(ctx,
					path,
					row.OwnerFQN,
					string(row.RefKind),
					string(row.DstLang),
					row.DstPkg,
					row.DstName,
					row.DstFQN,
					row.DstMember,
				); err != nil {
					return err
				}
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_symbol_ref_files(path)
			SELECT src_path
			FROM temp_symbol_ref_paths
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
			SELECT DISTINCT dst_lang, dst_pkg, dst_name, dst_fqn, dst_member
			FROM temp_symbol_ref_batch
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_symbol_refs
			WHERE src_file_id IN (
				SELECT sf.file_id
				FROM intel_symbol_ref_files sf
				JOIN temp_symbol_ref_paths p ON p.src_path = sf.path
			)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_symbol_refs (
				src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id
			)
			SELECT sf.file_id,
			       s.id,
			       b.owner_fqn,
			       b.ref_kind,
			       t.target_id
			FROM temp_symbol_ref_batch b
			JOIN intel_symbol_ref_files sf ON sf.path = b.src_path
			JOIN intel_symbol_ref_targets t
			  ON t.dst_lang = b.dst_lang
			 AND t.dst_pkg = b.dst_pkg
			 AND t.dst_name = b.dst_name
			 AND t.dst_fqn = b.dst_fqn
			 AND t.dst_member = b.dst_member
			LEFT JOIN symbols s ON s.fqn = b.owner_fqn
		`); err != nil {
			return err
		}
		return pruneOrphanReverseIndexTargetsTx(ctx, tx)
	})
}
