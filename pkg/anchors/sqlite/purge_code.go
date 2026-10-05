package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// PurgeCodeFilesNotIn deletes code file summary rows (and dependent symbol/scope rows)
// for files not present in keepRelPaths.
//
// keepRelPaths must use the same vault-relative path format stored in the `files` table.
// When allowEmpty is true, an empty keepRelPaths list means "purge everything".
func (s *Store) PurgeCodeFilesNotIn(ctx context.Context, keepRelPaths []string, allowEmpty bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(keepRelPaths) == 0 && !allowEmpty {
		// Safety: avoid deleting everything if the caller failed to discover files.
		return nil
	}

	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_keep_code_files`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_keep_code_files (path TEXT PRIMARY KEY)`); err != nil {
			return err
		}

		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_keep_code_files(path) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, p := range keepRelPaths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			p = string(paths.NormalizeCode(p))
			if p == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, p); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_code_files`)
		if _, err := tx.ExecContext(ctx, `
			CREATE TEMP TABLE temp_stale_code_files AS
			SELECT path FROM files
			WHERE path NOT IN (SELECT path FROM temp_keep_code_files)
		`); err != nil {
			return err
		}

		// Common case: no stale files. Avoid expensive DELETE scans over large tables.
		var hasStale int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM temp_stale_code_files LIMIT 1`).Scan(&hasStale); err != nil && err != sql.ErrNoRows {
			return err
		}
		if hasStale != 1 {
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_code_files`)
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_keep_code_files`)
			return nil
		}

		// Prune file-derived tables in dependency-safe order.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM super_edges
			WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM temp_stale_code_files))
			   OR parent_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM temp_stale_code_files))
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM annotations
			WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM temp_stale_code_files))
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM anchor_scopes
			WHERE symbol_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM temp_stale_code_files))
			   OR call_file IN (SELECT path FROM temp_stale_code_files)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file IN (SELECT path FROM temp_stale_code_files)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path IN (SELECT path FROM temp_stale_code_files)`); err != nil {
			return err
		}

		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_code_files`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_keep_code_files`)
		return nil
	})
}

// PurgeIntelCodeNotInPaths deletes intel code rows for code paths not present in keepRelPaths.
// This includes anchors, reverse-index rows, FTS rows, and rationale rows. keepRelPaths must be
// vault-root relative and slash-separated (the format used in intel tables). When allowEmpty is
// true, an empty keepRelPaths list means "purge everything".
func (s *Store) PurgeIntelCodeNotInPaths(ctx context.Context, keepRelPaths []string, allowEmpty bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(keepRelPaths) == 0 && !allowEmpty {
		return nil
	}

	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_keep_intel_code_paths`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_keep_intel_code_paths (path TEXT PRIMARY KEY)`); err != nil {
			return err
		}

		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_keep_intel_code_paths(path) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, p := range keepRelPaths {
			p = string(paths.NormalizeCode(strings.TrimSpace(p)))
			if p == "" || p == "." {
				continue
			}
			if _, err := stmt.ExecContext(ctx, p); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_intel_code_paths`)
		if _, err := tx.ExecContext(ctx, `
			CREATE TEMP TABLE temp_stale_intel_code_paths AS
			SELECT DISTINCT path FROM intel_code_anchors
			WHERE path NOT IN (SELECT path FROM temp_keep_intel_code_paths)
			UNION
			SELECT DISTINCT path FROM intel_rationale
			WHERE path NOT IN (SELECT path FROM temp_keep_intel_code_paths)
			UNION
			SELECT DISTINCT sf.path
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
			WHERE sf.path NOT IN (SELECT path FROM temp_keep_intel_code_paths)
			UNION
			SELECT DISTINCT src_path AS path FROM intel_import_refs
			WHERE src_path NOT IN (SELECT path FROM temp_keep_intel_code_paths)
			UNION
			SELECT DISTINCT src_path AS path FROM intel_module_defs
			WHERE src_path NOT IN (SELECT path FROM temp_keep_intel_code_paths)
			UNION
			SELECT DISTINCT src_path AS path FROM intel_external_import_evidence
			WHERE src_path NOT IN (SELECT path FROM temp_keep_intel_code_paths)
		`); err != nil {
			return err
		}

		// Common case: no stale anchors. Avoid expensive DELETE scans over large tables.
		var hasStale int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM temp_stale_intel_code_paths LIMIT 1`).Scan(&hasStale); err != nil && err != sql.ErrNoRows {
			return err
		}
		if hasStale != 1 {
			if allowEmpty {
				if err := pruneOrphanReverseIndexTargetsTx(ctx, tx); err != nil {
					return err
				}
			}
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_intel_code_paths`)
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_keep_intel_code_paths`)
			return nil
		}

		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_intel_code_anchor_ids`)
		if _, err := tx.ExecContext(ctx, `
			CREATE TEMP TABLE temp_stale_intel_code_anchor_ids AS
			SELECT id AS anchor_row_id, anchor_id
			FROM intel_code_anchors
			WHERE path IN (SELECT path FROM temp_stale_intel_code_paths)
		`); err != nil {
			return err
		}

		if err := deleteIntelReverseIndexByPathsTx(ctx, tx, `SELECT path FROM temp_stale_intel_code_paths`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_rationale
			WHERE path IN (SELECT path FROM temp_stale_intel_code_paths)
		`); err != nil {
			return err
		}

		// Remove edges (mentions/defines/calls) that reference stale code anchors.
		if _, err := tx.ExecContext(ctx, `
				DELETE FROM intel_edges
				WHERE (src_type = 'anchor' AND src_row_id IN (SELECT anchor_row_id FROM temp_stale_intel_code_anchor_ids))
				   OR (dst_type = 'anchor' AND dst_row_id IN (SELECT anchor_row_id FROM temp_stale_intel_code_anchor_ids))
			`); err != nil {
			return err
		}

		// Remove FTS rows via the rowid mapping table (no giant IN lists).
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_fts
			WHERE rowid IN (
				SELECT r.fts_rowid
				FROM intel_fts_rowid r
				JOIN temp_stale_intel_code_anchor_ids s ON s.anchor_id = r.item_id
				WHERE r.item_type = 'anchor'
			)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_fts_rowid
			WHERE item_type = 'anchor'
			  AND item_id IN (SELECT anchor_id FROM temp_stale_intel_code_anchor_ids)
		`); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_code_anchors
			WHERE id IN (SELECT anchor_row_id FROM temp_stale_intel_code_anchor_ids)
		`); err != nil {
			return err
		}

		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_intel_code_anchor_ids`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_stale_intel_code_paths`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_keep_intel_code_paths`)
		return nil
	})
}
