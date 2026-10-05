package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// ReplaceExternalEvidenceForPathsBatch atomically replaces sparse external
// evidence for all supplied source paths. Inputs use natural keys; catalog,
// raw-target, and association IDs are resolved set-wise inside the transaction.
func (s *Store) ReplaceExternalEvidenceForPathsBatch(ctx context.Context, batches map[string]codeanchor.ExternalEvidenceBatch) error {
	if len(batches) == 0 {
		return nil
	}
	// Validate before entering the shared writer lane so malformed public input
	// fails without taking the process-wide write mutex. The transaction helper
	// revalidates because internal batch callers invoke it directly after raw-ref
	// replacement in an already-open transaction.
	if err := validateExternalEvidenceBatches(batches); err != nil {
		return err
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_external_evidence")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return replaceExternalEvidenceForPathsBatchTx(ctx, tx, batches)
	})
}

// replaceExternalEvidenceForPathsBatchTx runs after the durable raw-reference
// replacement in the same transaction. That ordering lets natural-key
// evidence resolve set-wise without worker-side IDs or per-file lookups.
func replaceExternalEvidenceForPathsBatchTx(ctx context.Context, tx *sql.Tx, batches map[string]codeanchor.ExternalEvidenceBatch) error {
	if len(batches) == 0 {
		return nil
	}
	if err := validateExternalEvidenceBatches(batches); err != nil {
		return err
	}
	if err := createExternalEvidenceTempSchema(ctx, tx); err != nil {
		return err
	}
	defer dropExternalEvidenceTempSchema(tx)

	pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_external_paths(src_path) VALUES (?)`)
	if err != nil {
		return err
	}
	defer pathStmt.Close()
	symbolStmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO temp_external_symbols (
			src_path, owner_fqn, ref_kind, dst_lang, dst_pkg, dst_name, dst_fqn,
			handle, ecosystem, module, symbol_path, target_kind, evidence_kind,
			confidence, imported_name, local_name, manifest_path, declared_range, version_scope
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer symbolStmt.Close()
	importStmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO temp_external_imports (
			src_path, module, binding_ordinal, handle, ecosystem, target_module, symbol_path,
			target_kind, evidence_kind, confidence, imported_name, local_name,
			manifest_path, declared_range, version_scope
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer importStmt.Close()

	for rawPath, batch := range batches {
		path, err := externalStoragePath(rawPath, "source path")
		if err != nil {
			return err
		}
		if _, err := pathStmt.ExecContext(ctx, path); err != nil {
			return err
		}
		for _, row := range batch.Symbols {
			manifestPath, declaredRange, scope := externalVersionColumns(row.Evidence.Version)
			if _, err := symbolStmt.ExecContext(ctx,
				path, row.OwnerFQN, string(row.RefKind), string(row.Raw.DstLang), row.Raw.DstPkg, row.Raw.DstName, row.Raw.DstFQN,
				row.Target.Handle, string(row.Target.Ecosystem), row.Target.Module, row.Target.SymbolPath, string(row.Target.Kind), string(row.Evidence.Kind),
				string(row.Evidence.Confidence), row.Evidence.ImportedName, row.Evidence.LocalName, manifestPath, declaredRange, scope,
			); err != nil {
				return err
			}
		}
		for _, row := range batch.Imports {
			manifestPath, declaredRange, scope := externalVersionColumns(row.Evidence.Version)
			if _, err := importStmt.ExecContext(ctx,
				path, row.Module, row.BindingOrdinal, row.Target.Handle, string(row.Target.Ecosystem), row.Target.Module, row.Target.SymbolPath,
				string(row.Target.Kind), string(row.Evidence.Kind), string(row.Evidence.Confidence), row.Evidence.ImportedName, row.Evidence.LocalName,
				manifestPath, declaredRange, scope,
			); err != nil {
				return err
			}
		}
	}
	var unmatched int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM temp_external_symbols b
			WHERE NOT EXISTS (
				SELECT 1 FROM intel_symbol_ref_files f
				JOIN intel_symbol_ref_targets r ON r.dst_lang=b.dst_lang AND r.dst_pkg=b.dst_pkg AND r.dst_name=b.dst_name AND r.dst_fqn=b.dst_fqn
				JOIN intel_symbol_refs sr ON sr.src_file_id=f.file_id AND sr.owner_fqn=b.owner_fqn AND sr.ref_kind=b.ref_kind AND sr.dst_target_id=r.target_id
				WHERE f.path=b.src_path
			)`).Scan(&unmatched); err != nil {
		return err
	}
	if unmatched != 0 {
		return fmt.Errorf("external symbol evidence has %d unmatched durable symbol-reference associations", unmatched)
	}
	var intraBatchConflicts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
			SELECT dst_lang, dst_pkg, dst_name, dst_fqn
			FROM temp_external_symbols
			GROUP BY dst_lang, dst_pkg, dst_name, dst_fqn
			HAVING COUNT(DISTINCT handle) > 1
		)`).Scan(&intraBatchConflicts); err != nil {
		return err
	}
	if intraBatchConflicts != 0 {
		var lang, pkg, name, fqn, handles string
		if err := tx.QueryRowContext(ctx, `SELECT dst_lang, dst_pkg, dst_name, dst_fqn, group_concat(DISTINCT handle)
			FROM temp_external_symbols
			GROUP BY dst_lang, dst_pkg, dst_name, dst_fqn
			HAVING COUNT(DISTINCT handle) > 1
			ORDER BY dst_lang, dst_pkg, dst_name, dst_fqn LIMIT 1`).Scan(&lang, &pkg, &name, &fqn, &handles); err != nil {
			return err
		}
		return fmt.Errorf("external symbol evidence contains %d conflicting raw-target mappings; first conflict %s:%s:%s (%s) -> %s",
			intraBatchConflicts, lang, pkg, name, fqn, handles)
	}
	var conflictingMappings int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM temp_external_symbols b
			JOIN intel_symbol_ref_targets r ON r.dst_lang=b.dst_lang AND r.dst_pkg=b.dst_pkg AND r.dst_name=b.dst_name AND r.dst_fqn=b.dst_fqn
			JOIN intel_external_target_map m ON m.raw_target_id=r.target_id
			JOIN intel_external_targets e ON e.external_id=m.external_id
			WHERE e.handle != b.handle`).Scan(&conflictingMappings); err != nil {
		return err
	}
	if conflictingMappings != 0 {
		return fmt.Errorf("external symbol evidence conflicts with %d existing raw-target mappings", conflictingMappings)
	}
	var conflictingCatalog int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
			SELECT b.handle FROM (
				SELECT handle, ecosystem, module, symbol_path, target_kind FROM temp_external_symbols
				UNION SELECT handle, ecosystem, target_module, symbol_path, target_kind FROM temp_external_imports
			) b JOIN intel_external_targets e ON e.handle=b.handle
			WHERE e.ecosystem != b.ecosystem OR e.module != b.module OR e.symbol_path != b.symbol_path OR e.target_kind != b.target_kind
			UNION ALL
			SELECT b.handle FROM (
				SELECT handle, ecosystem, module, symbol_path, target_kind FROM temp_external_symbols
				UNION SELECT handle, ecosystem, target_module, symbol_path, target_kind FROM temp_external_imports
			) b JOIN intel_external_targets e
			  ON e.ecosystem=b.ecosystem AND e.module=b.module AND e.symbol_path=b.symbol_path AND e.target_kind=b.target_kind
			WHERE e.handle != b.handle
		)`).Scan(&conflictingCatalog); err != nil {
		return err
	}
	if conflictingCatalog != 0 {
		return fmt.Errorf("external evidence conflicts with %d existing catalog identities", conflictingCatalog)
	}

	for _, stmt := range externalEvidenceReplaceStatements() {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("replace external evidence: %w", err)
		}
	}
	return pruneOrphanExternalTargetsTx(ctx, tx)
}

func validateExternalEvidenceBatches(batches map[string]codeanchor.ExternalEvidenceBatch) error {
	validateTarget := func(target codeanchor.ExternalTarget) error {
		canonical, err := codeanchor.CanonicalizeExternalTarget(target)
		if err != nil {
			return err
		}
		if canonical != target {
			return fmt.Errorf("external target is not canonical: got %q, want %q", target.Handle, canonical.Handle)
		}
		return nil
	}
	for path, batch := range batches {
		if _, err := externalStoragePath(path, "source path"); err != nil {
			return err
		}
		for _, row := range batch.Symbols {
			if err := validateTarget(row.Target); err != nil {
				return err
			}
			// OwnerFQN is intentionally allowed to be empty: parser-emitted refs at
			// file/module top level use the empty owner in the durable ref key.
			if row.RefKind == "" || row.Raw.DstLang == "" || row.Raw.DstName == "" || row.Raw.DstFQN == "" {
				return fmt.Errorf("external symbol evidence requires a complete association key")
			}
			if err := codeanchor.ValidateExternalEvidence(row.Target, row.Evidence); err != nil {
				return err
			}
			if row.Evidence.Version != nil {
				if _, err := externalStoragePath(row.Evidence.Version.ManifestPath, "manifest path"); err != nil {
					return err
				}
			}
		}
		for _, row := range batch.Imports {
			if err := validateTarget(row.Target); err != nil {
				return err
			}
			if row.Module == "" || row.BindingOrdinal < 0 {
				return fmt.Errorf("external import evidence requires module, non-negative ordinal, kind, and confidence")
			}
			if err := codeanchor.ValidateExternalEvidence(row.Target, row.Evidence); err != nil {
				return err
			}
			if row.Evidence.Version != nil {
				if _, err := externalStoragePath(row.Evidence.Version.ManifestPath, "manifest path"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func externalVersionColumns(version *codeanchor.ExternalVersionProvenance) (string, string, string) {
	if version == nil {
		return "", "", ""
	}
	manifestPath, _ := externalStoragePath(version.ManifestPath, "manifest path")
	return manifestPath, version.DeclaredRange, string(version.Scope)
}

func createExternalEvidenceTempSchema(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS temp_external_paths`,
		`DROP TABLE IF EXISTS temp_external_symbols`,
		`DROP TABLE IF EXISTS temp_external_imports`,
		`CREATE TEMP TABLE temp_external_paths (src_path TEXT PRIMARY KEY) WITHOUT ROWID`,
		`CREATE TEMP TABLE temp_external_symbols (
			src_path TEXT NOT NULL, owner_fqn TEXT NOT NULL, ref_kind TEXT NOT NULL,
			dst_lang TEXT NOT NULL, dst_pkg TEXT NOT NULL, dst_name TEXT NOT NULL, dst_fqn TEXT NOT NULL,
			handle TEXT NOT NULL, ecosystem TEXT NOT NULL, module TEXT NOT NULL, symbol_path TEXT NOT NULL,
			target_kind TEXT NOT NULL, evidence_kind TEXT NOT NULL, confidence TEXT NOT NULL,
			imported_name TEXT NOT NULL, local_name TEXT NOT NULL, manifest_path TEXT NOT NULL,
			declared_range TEXT NOT NULL, version_scope TEXT NOT NULL,
			PRIMARY KEY (src_path, owner_fqn, ref_kind, dst_lang, dst_pkg, dst_name, dst_fqn, handle)
		) WITHOUT ROWID`,
		`CREATE TEMP TABLE temp_external_imports (
			src_path TEXT NOT NULL, module TEXT NOT NULL, binding_ordinal INTEGER NOT NULL,
			handle TEXT NOT NULL, ecosystem TEXT NOT NULL, target_module TEXT NOT NULL, symbol_path TEXT NOT NULL,
			target_kind TEXT NOT NULL, evidence_kind TEXT NOT NULL, confidence TEXT NOT NULL,
			imported_name TEXT NOT NULL, local_name TEXT NOT NULL, manifest_path TEXT NOT NULL,
			declared_range TEXT NOT NULL, version_scope TEXT NOT NULL,
			PRIMARY KEY (src_path, module, binding_ordinal)
		) WITHOUT ROWID`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func dropExternalEvidenceTempSchema(tx *sql.Tx) {
	_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_external_imports`)
	_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_external_symbols`)
	_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_external_paths`)
}

func externalEvidenceReplaceStatements() []string {
	return []string{
		`DELETE FROM intel_external_import_evidence WHERE src_path IN (SELECT src_path FROM temp_external_paths)`,
		`DELETE FROM intel_external_symbol_evidence WHERE src_file_id IN (
			SELECT f.file_id FROM intel_symbol_ref_files f JOIN temp_external_paths p ON p.src_path = f.path
		)`,
		`INSERT OR IGNORE INTO intel_external_targets(handle, ecosystem, module, symbol_path, target_kind)
		 SELECT handle, ecosystem, module, symbol_path, target_kind FROM temp_external_symbols
		 UNION SELECT handle, ecosystem, target_module, symbol_path, target_kind FROM temp_external_imports`,
		`INSERT OR IGNORE INTO intel_external_target_map(raw_target_id, external_id)
		 SELECT r.target_id, e.external_id
		 FROM temp_external_symbols b
		 JOIN intel_symbol_ref_targets r ON r.dst_lang=b.dst_lang AND r.dst_pkg=b.dst_pkg AND r.dst_name=b.dst_name AND r.dst_fqn=b.dst_fqn
		 JOIN intel_external_targets e ON e.handle=b.handle`,
		`INSERT INTO intel_external_symbol_evidence(
			src_file_id, owner_fqn, ref_kind, raw_target_id, external_id, evidence_kind,
			confidence, imported_name, local_name, manifest_path, declared_range, version_scope)
		 SELECT f.file_id, b.owner_fqn, b.ref_kind, r.target_id, e.external_id, b.evidence_kind,
			b.confidence, b.imported_name, b.local_name, b.manifest_path, b.declared_range, b.version_scope
		 FROM temp_external_symbols b
		 JOIN intel_symbol_ref_files f ON f.path=b.src_path
		 JOIN intel_symbol_ref_targets r ON r.dst_lang=b.dst_lang AND r.dst_pkg=b.dst_pkg AND r.dst_name=b.dst_name AND r.dst_fqn=b.dst_fqn
		 JOIN intel_external_targets e ON e.handle=b.handle
		 JOIN intel_external_target_map m ON m.raw_target_id=r.target_id AND m.external_id=e.external_id
		 JOIN intel_symbol_refs sr ON sr.src_file_id=f.file_id AND sr.owner_fqn=b.owner_fqn AND sr.ref_kind=b.ref_kind AND sr.dst_target_id=r.target_id`,
		`INSERT INTO intel_external_import_evidence(
			src_path, module, binding_ordinal, external_id, evidence_kind, confidence,
			imported_name, local_name, manifest_path, declared_range, version_scope)
		 SELECT b.src_path, b.module, b.binding_ordinal, e.external_id, b.evidence_kind, b.confidence,
			b.imported_name, b.local_name, b.manifest_path, b.declared_range, b.version_scope
		 FROM temp_external_imports b JOIN intel_external_targets e ON e.handle=b.handle`,
	}
}

func pruneOrphanExternalTargetsTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_external_target_map
		WHERE NOT EXISTS (SELECT 1 FROM intel_external_symbol_evidence e
			WHERE e.raw_target_id=intel_external_target_map.raw_target_id
			  AND e.external_id=intel_external_target_map.external_id)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM intel_external_targets
		WHERE NOT EXISTS (SELECT 1 FROM intel_external_target_map m WHERE m.external_id=intel_external_targets.external_id)
		  AND NOT EXISTS (SELECT 1 FROM intel_external_import_evidence i WHERE i.external_id=intel_external_targets.external_id)`)
	return err
}

// PruneOrphanExternalTargets removes unused raw mappings and catalog rows.
func (s *Store) PruneOrphanExternalTargets(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.prune_external_targets")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error { return pruneOrphanExternalTargetsTx(ctx, tx) })
}

// ExternalTargets returns the canonical pathless catalog in deterministic order.
func (s *Store) ExternalTargets(ctx context.Context) ([]codeanchor.ExternalTargetRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT external_id, handle, ecosystem, module, symbol_path, target_kind
		FROM intel_external_targets ORDER BY handle`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.ExternalTargetRow
	for rows.Next() {
		var row codeanchor.ExternalTargetRow
		if err := rows.Scan(&row.ID, &row.Handle, &row.Ecosystem, &row.Module, &row.SymbolPath, &row.Kind); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ExternalImportEvidenceForPath reads explicit import evidence without mixing it
// into the local-module intel_import_refs family.
func (s *Store) ExternalImportEvidenceForPath(ctx context.Context, path string) ([]codeanchor.ExternalImportEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT module, binding_ordinal, external_id, evidence_kind, confidence,
		imported_name, local_name, manifest_path, declared_range, version_scope
		FROM intel_external_import_evidence WHERE src_path=? ORDER BY module, binding_ordinal`, normalizeCodeLookupPath(path))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.ExternalImportEvidence
	for rows.Next() {
		var row codeanchor.ExternalImportEvidence
		var kind, confidence, manifestPath, declaredRange, scope string
		row.Association.SrcPath = normalizeCodeLookupPath(path)
		if err := rows.Scan(&row.Association.Module, &row.Association.BindingOrdinal, &row.ExternalID,
			&kind, &confidence, &row.Evidence.ImportedName, &row.Evidence.LocalName,
			&manifestPath, &declaredRange, &scope); err != nil {
			return nil, err
		}
		row.Evidence.Kind = codeanchor.ExternalEvidenceKind(kind)
		row.Evidence.Confidence = codeanchor.ExternalConfidence(confidence)
		if manifestPath != "" || declaredRange != "" || scope != "" {
			row.Evidence.Version = &codeanchor.ExternalVersionProvenance{
				ManifestPath: manifestPath, DeclaredRange: declaredRange, Scope: codeanchor.ExternalVersionScope(scope),
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ExternalReferenceClassificationsForPath is the Phase 1 sparse-evidence proof
// reader: it derives local/external precedence only for associations that have
// qualifying external evidence. It is not the Phase 3 four-way all-reference
// projection and therefore does not enumerate local-only or unknown refs.
func (s *Store) ExternalReferenceClassificationsForPath(ctx context.Context, path string) ([]codeanchor.ExternalReferenceClassificationRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.path, se.owner_fqn, se.ref_kind, se.raw_target_id,
		       et.external_id, et.handle, et.ecosystem, et.module, et.symbol_path, et.target_kind,
		       se.evidence_kind, se.confidence, se.imported_name, se.local_name,
		       se.manifest_path, se.declared_range, se.version_scope,
		       EXISTS (
		         SELECT 1 FROM symbols s
		         JOIN intel_symbol_ref_targets rt ON rt.target_id=se.raw_target_id
		         WHERE s.lang=rt.dst_lang AND s.fqn=rt.dst_fqn
		       ) AS local_resolved
		FROM intel_external_symbol_evidence se
		JOIN intel_symbol_ref_files f ON f.file_id=se.src_file_id
		JOIN intel_external_targets et ON et.external_id=se.external_id
		WHERE f.path=?
		ORDER BY se.owner_fqn, se.ref_kind, se.raw_target_id`, normalizeCodeLookupPath(path))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.ExternalReferenceClassificationRow
	for rows.Next() {
		var row codeanchor.ExternalReferenceClassificationRow
		var evidenceKind, confidence, manifestPath, declaredRange, scope string
		var localResolved bool
		if err := rows.Scan(
			&row.Association.SrcPath, &row.Association.OwnerFQN, &row.Association.RefKind, &row.Association.RawTargetID,
			&row.Target.ID, &row.Target.Handle, &row.Target.Ecosystem, &row.Target.Module, &row.Target.SymbolPath, &row.Target.Kind,
			&evidenceKind, &confidence, &row.Evidence.ImportedName, &row.Evidence.LocalName,
			&manifestPath, &declaredRange, &scope, &localResolved,
		); err != nil {
			return nil, err
		}
		row.Evidence.Kind = codeanchor.ExternalEvidenceKind(evidenceKind)
		row.Evidence.Confidence = codeanchor.ExternalConfidence(confidence)
		if manifestPath != "" || declaredRange != "" || scope != "" {
			row.Evidence.Version = &codeanchor.ExternalVersionProvenance{
				ManifestPath: manifestPath, DeclaredRange: declaredRange, Scope: codeanchor.ExternalVersionScope(scope),
			}
		}
		row.Class = codeanchor.DeriveExternalReferenceClass(localResolved, row.Target.Kind, &row.Evidence)
		out = append(out, row)
	}
	return out, rows.Err()
}
