package sqlite

import "context"

func createExternalTargetSchema(ctx context.Context, db execer) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intel_external_targets (
			external_id INTEGER PRIMARY KEY,
			handle TEXT NOT NULL UNIQUE CHECK (handle != ''),
			ecosystem TEXT NOT NULL CHECK (ecosystem != ''),
			module TEXT NOT NULL CHECK (module != ''),
			symbol_path TEXT NOT NULL,
			target_kind TEXT NOT NULL CHECK (target_kind IN ('module', 'symbol', 'type', 'runtime_builtin', 'runtime_global')),
			UNIQUE (ecosystem, module, symbol_path, target_kind)
		) STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_external_target_map (
			raw_target_id INTEGER PRIMARY KEY,
			external_id INTEGER NOT NULL,
			UNIQUE (raw_target_id, external_id),
			FOREIGN KEY(raw_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE,
			FOREIGN KEY(external_id) REFERENCES intel_external_targets(external_id) ON DELETE CASCADE
		) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_external_target_map_external
			ON intel_external_target_map(external_id, raw_target_id);`,
		`CREATE TABLE IF NOT EXISTS intel_external_symbol_evidence (
			src_file_id INTEGER NOT NULL,
			owner_fqn TEXT NOT NULL,
			ref_kind TEXT NOT NULL,
			raw_target_id INTEGER NOT NULL,
			external_id INTEGER NOT NULL,
				evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('esm_named', 'esm_default', 'esm_namespace', 'esm_type', 'esm_side_effect', 'cjs_named', 'cjs_default', 'cjs_namespace', 'cjs_type', 'cjs_side_effect', 'runtime_builtin', 'runtime_global')),
			confidence TEXT NOT NULL CHECK (confidence IN ('low', 'medium', 'high')),
			imported_name TEXT NOT NULL DEFAULT '',
			local_name TEXT NOT NULL DEFAULT '',
			manifest_path TEXT NOT NULL DEFAULT '',
			declared_range TEXT NOT NULL DEFAULT '',
				version_scope TEXT NOT NULL DEFAULT '' CHECK (version_scope IN ('', 'dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies')),
			PRIMARY KEY (src_file_id, owner_fqn, ref_kind, raw_target_id),
			FOREIGN KEY(src_file_id, owner_fqn, ref_kind, raw_target_id)
				REFERENCES intel_symbol_refs(src_file_id, owner_fqn, ref_kind, dst_target_id) ON DELETE CASCADE,
			FOREIGN KEY(raw_target_id, external_id)
				REFERENCES intel_external_target_map(raw_target_id, external_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_external_symbol_evidence_external
			ON intel_external_symbol_evidence(external_id, src_file_id, raw_target_id);`,
		`CREATE TABLE IF NOT EXISTS intel_external_import_evidence (
			src_path TEXT NOT NULL,
			module TEXT NOT NULL,
			binding_ordinal INTEGER NOT NULL CHECK (binding_ordinal >= 0),
			external_id INTEGER NOT NULL,
				evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('esm_named', 'esm_default', 'esm_namespace', 'esm_type', 'esm_side_effect', 'cjs_named', 'cjs_default', 'cjs_namespace', 'cjs_type', 'cjs_side_effect', 'runtime_builtin', 'runtime_global')),
			confidence TEXT NOT NULL CHECK (confidence IN ('low', 'medium', 'high')),
			imported_name TEXT NOT NULL DEFAULT '',
			local_name TEXT NOT NULL DEFAULT '',
			manifest_path TEXT NOT NULL DEFAULT '',
			declared_range TEXT NOT NULL DEFAULT '',
				version_scope TEXT NOT NULL DEFAULT '' CHECK (version_scope IN ('', 'dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies')),
			PRIMARY KEY (src_path, module, binding_ordinal),
			FOREIGN KEY(external_id) REFERENCES intel_external_targets(external_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_external_import_evidence_external
			ON intel_external_import_evidence(external_id, src_path, module);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
