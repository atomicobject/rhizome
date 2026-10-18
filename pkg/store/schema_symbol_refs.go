package store

import (
	"context"
	"database/sql"
)

func addSymbolRefTargetMemberSchema(ctx context.Context, tx *sql.Tx) error {
	hasMember, err := tableHasColumnQuery(ctx, tx, "intel_symbol_ref_targets", "dst_member")
	if err != nil {
		return err
	}
	memberProjection := `CASE WHEN lower(dst_lang) = 'php' AND instr(dst_fqn, '::') > 0 THEN 1 ELSE 0 END`
	if hasMember {
		memberProjection = "dst_member"
	}
	execAll := func(stmts ...string) error {
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}
	// External mappings and symbol evidence reference both tables being rebuilt.
	// Move them first, then recreate and copy them before dropping the v68 graph.
	if err := execAll(
		`ALTER TABLE intel_external_symbol_evidence RENAME TO intel_external_symbol_evidence_v68`,
		`ALTER TABLE intel_external_target_map RENAME TO intel_external_target_map_v68`,
		`ALTER TABLE intel_symbol_refs RENAME TO intel_symbol_refs_v68`,
		`ALTER TABLE intel_symbol_ref_targets RENAME TO intel_symbol_ref_targets_v68`,
		`CREATE TABLE intel_symbol_ref_targets (
			target_id INTEGER PRIMARY KEY,
			dst_lang TEXT NOT NULL,
			dst_pkg TEXT NOT NULL,
			dst_name TEXT NOT NULL,
			dst_fqn TEXT NOT NULL,
			dst_member INTEGER NOT NULL DEFAULT 0 CHECK (dst_member IN (0, 1)),
			UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
		) STRICT`,
		`CREATE TABLE intel_symbol_refs (
			src_file_id INTEGER NOT NULL,
			owner_symbol_id INTEGER,
			owner_fqn TEXT NOT NULL,
			ref_kind TEXT NOT NULL,
			dst_target_id INTEGER NOT NULL,
			PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
			FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT`,
		`INSERT INTO intel_symbol_ref_targets(target_id, dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
		 SELECT target_id, dst_lang, dst_pkg, dst_name, dst_fqn, `+memberProjection+`
		 FROM intel_symbol_ref_targets_v68`,
		`INSERT INTO intel_symbol_refs(src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id)
		 SELECT src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id
		 FROM intel_symbol_refs_v68`,
	); err != nil {
		return err
	}
	if err := createExternalTargetSchema(ctx, tx); err != nil {
		return err
	}
	if err := execAll(
		`INSERT INTO intel_external_target_map(raw_target_id, external_id)
		 SELECT raw_target_id, external_id FROM intel_external_target_map_v68`,
		`INSERT INTO intel_external_symbol_evidence(
			src_file_id, owner_fqn, ref_kind, raw_target_id, external_id,
			evidence_kind, confidence, imported_name, local_name, manifest_path, declared_range, version_scope
		 ) SELECT src_file_id, owner_fqn, ref_kind, raw_target_id, external_id,
		          evidence_kind, confidence, imported_name, local_name, manifest_path, declared_range, version_scope
		   FROM intel_external_symbol_evidence_v68`,
		`DROP TABLE intel_external_symbol_evidence_v68`,
		`DROP TABLE intel_external_target_map_v68`,
		`DROP TABLE intel_symbol_refs_v68`,
		`DROP TABLE intel_symbol_ref_targets_v68`,
		`CREATE INDEX idx_intel_symbol_ref_targets_name ON intel_symbol_ref_targets(dst_lang, dst_name)`,
		`CREATE INDEX idx_intel_symbol_ref_targets_fqn ON intel_symbol_ref_targets(dst_lang, dst_fqn)`,
		`CREATE INDEX idx_intel_symbol_refs_dst_target ON intel_symbol_refs(dst_target_id, src_file_id, owner_symbol_id)`,
		`CREATE INDEX idx_intel_symbol_refs_owner ON intel_symbol_refs(owner_symbol_id, src_file_id, dst_target_id) WHERE owner_symbol_id IS NOT NULL`,
	); err != nil {
		return err
	}
	return createExternalTargetSchema(ctx, tx)
}
