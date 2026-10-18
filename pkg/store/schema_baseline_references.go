package store

import (
	"context"
)

func intelBaselineStep23(ctx context.Context, db execer) error { // v23 -> v24
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intel_symbol_ref_files (
				file_id INTEGER PRIMARY KEY,
				path    TEXT NOT NULL UNIQUE
			) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_files_path ON intel_symbol_ref_files(path);`,
		`CREATE TABLE IF NOT EXISTS intel_symbol_ref_targets (
				target_id INTEGER PRIMARY KEY,
				dst_lang  TEXT NOT NULL,
				dst_pkg   TEXT NOT NULL,
				dst_name  TEXT NOT NULL,
				dst_fqn   TEXT NOT NULL,
				dst_member INTEGER NOT NULL DEFAULT 0 CHECK (dst_member IN (0, 1)),
				UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
			) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_name ON intel_symbol_ref_targets(dst_lang, dst_name);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_fqn ON intel_symbol_ref_targets(dst_lang, dst_fqn);`,
		`CREATE TABLE IF NOT EXISTS intel_symbol_refs (
				src_file_id    INTEGER NOT NULL,
				owner_symbol_id INTEGER,
				owner_fqn      TEXT NOT NULL,
				ref_kind       TEXT NOT NULL,
				dst_target_id  INTEGER NOT NULL,
				PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
				FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_dst_target ON intel_symbol_refs(dst_target_id, src_file_id, owner_symbol_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_owner ON intel_symbol_refs(owner_symbol_id, src_file_id, dst_target_id) WHERE owner_symbol_id IS NOT NULL;`,
		`CREATE TABLE IF NOT EXISTS intel_import_refs (
				src_path TEXT NOT NULL,
				module   TEXT NOT NULL,
				PRIMARY KEY (src_path, module)
			);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_import_refs_module ON intel_import_refs(module, src_path);`,
		`CREATE TABLE IF NOT EXISTS intel_module_defs (
				src_path TEXT NOT NULL,
				lang     TEXT NOT NULL,
				module   TEXT NOT NULL,
				PRIMARY KEY (src_path, lang, module)
			);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_module_defs_src ON intel_module_defs(src_path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_module_defs_module ON intel_module_defs(lang, module, src_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
