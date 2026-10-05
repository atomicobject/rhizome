package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// deleteIntelReverseIndexByPathsTx removes all reverse-index rows owned by a
// set of source paths, then prunes raw targets no remaining reference uses.
// pathQuery is an internal SELECT expression and must not contain user input.
func deleteIntelReverseIndexByPathsTx(ctx context.Context, tx *sql.Tx, pathQuery string, args ...any) error {
	if err := deleteIntelReverseIndexRowsByPathsTx(ctx, tx, pathQuery, args...); err != nil {
		return err
	}
	return pruneOrphanReverseIndexTargetsTx(ctx, tx)
}

func deleteIntelReverseIndexRowsByPathsTx(ctx context.Context, tx *sql.Tx, pathQuery string, args ...any) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM intel_external_import_evidence WHERE src_path IN (%s)`, pathQuery), args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM intel_symbol_refs
		WHERE src_file_id IN (
			SELECT file_id FROM intel_symbol_ref_files WHERE path IN (%s)
		)
	`, pathQuery), args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM intel_symbol_ref_files
		WHERE path IN (%s)
		  AND NOT EXISTS (
			SELECT 1 FROM intel_symbol_refs WHERE src_file_id = intel_symbol_ref_files.file_id
		  )
	`, pathQuery), args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM intel_import_refs WHERE src_path IN (%s)`, pathQuery), args...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM intel_module_defs WHERE src_path IN (%s)`, pathQuery), args...); err != nil {
		return err
	}
	return nil
}

func deleteIntelReverseIndexByPathBatchesTx(ctx context.Context, tx *sql.Tx, paths []string) error {
	const batchSize = 400
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		values := strings.TrimSuffix(strings.Repeat("(?),", len(batch)), ",")
		args := make([]any, len(batch))
		for i, path := range batch {
			args[i] = normalizeCodeLookupPath(path)
		}
		if err := deleteIntelReverseIndexRowsByPathsTx(ctx, tx, `SELECT column1 FROM (VALUES `+values+`)`, args...); err != nil {
			return err
		}
	}
	return pruneOrphanReverseIndexTargetsTx(ctx, tx)
}

func pruneOrphanReverseIndexTargetsTx(ctx context.Context, tx *sql.Tx) error {
	if err := pruneOrphanExternalTargetsTx(ctx, tx); err != nil {
		return err
	}
	return pruneOrphanIntelSymbolRefTargetsTx(ctx, tx)
}

func pruneOrphanIntelSymbolRefTargetsTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM intel_symbol_ref_targets
		WHERE NOT EXISTS (
			SELECT 1
			FROM intel_symbol_refs
			WHERE intel_symbol_refs.dst_target_id = intel_symbol_ref_targets.target_id
		)
	`)
	return err
}
