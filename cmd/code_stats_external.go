package cmd

import (
	"database/sql"
	"fmt"
)

// ClassificationCountSet is the exhaustive, disjoint reference-classification
// vocabulary. Fields are intentionally explicit so consumers do not have to
// infer a missing class from a sparse map.
type ClassificationCountSet struct {
	LocalResolved        int `json:"local_resolved"`
	ExternalClassified   int `json:"external_classified"`
	RuntimeGlobalBuiltin int `json:"runtime_global_builtin"`
	Unknown              int `json:"unknown"`
}

// ReferenceKindClassificationStats reports use counts separately from target
// identity counts. Multiple raw spellings can therefore converge on one
// canonical external target without hiding their use-site fan-in.
type ReferenceKindClassificationStats struct {
	Associations  ClassificationCountSet `json:"associations"`
	UniqueTargets ClassificationCountSet `json:"uniqueTargets"`
}

// ReferenceClassificationStats reports all durable reference associations for
// one source language, both overall and split by the persisted relation kind.
type ReferenceClassificationStats struct {
	Associations  ClassificationCountSet                      `json:"associations"`
	UniqueTargets ClassificationCountSet                      `json:"uniqueTargets"`
	ByKind        map[string]ReferenceKindClassificationStats `json:"byKind"`
}

func queryReferenceClassifications(db *sql.DB, stats *CodeStats) error {
	hasExternalSchema, err := externalReferenceStatsSchemaAvailable(db)
	if err != nil {
		return err
	}
	if !hasExternalSchema {
		return queryLegacyReferenceClassifications(db, stats)
	}

	// RATIONALE: classification is derived in one bounded aggregate query over
	// durable association/evidence tables. A current source-backed definition
	// wins even if stale external evidence is still present. External target IDs
	// provide canonical N:1 identity; local/unknown symbol targets use the
	// normalized raw target ID. Imports retain their existing module-level local
	// identity and binding-level external association contract.
	rows, err := db.Query(`
		WITH classified(lang, relation_kind, class, target_key) AS MATERIALIZED (
			SELECT COALESCE(f.lang, rt.dst_lang), sr.ref_kind,
				CASE
					WHEN s.id IS NOT NULL THEN 'local_resolved'
					WHEN et.target_kind IN ('runtime_builtin','runtime_global') THEN 'runtime_global_builtin'
					WHEN se.evidence_kind IN ('runtime_builtin','runtime_global') THEN 'runtime_global_builtin'
					WHEN se.evidence_kind IS NOT NULL THEN 'external_classified'
					ELSE 'unknown'
				END,
				CASE
					WHEN s.id IS NOT NULL THEN 'local-symbol:' || sr.dst_target_id
					WHEN se.external_id IS NOT NULL THEN 'external:' || se.external_id
					ELSE 'unknown-symbol:' || sr.dst_target_id
				END
			FROM intel_symbol_refs sr
			JOIN intel_symbol_ref_targets rt ON rt.target_id=sr.dst_target_id
			JOIN intel_symbol_ref_files rf ON rf.file_id=sr.src_file_id
			LEFT JOIN files f ON f.path=rf.path
			LEFT JOIN symbols s ON s.lang=rt.dst_lang AND s.fqn=rt.dst_fqn
			LEFT JOIN intel_external_symbol_evidence se
			  ON se.src_file_id=sr.src_file_id
			 AND se.owner_fqn=sr.owner_fqn
			 AND se.ref_kind=sr.ref_kind
			 AND se.raw_target_id=sr.dst_target_id
			LEFT JOIN intel_external_targets et ON et.external_id=se.external_id
			WHERE sr.ref_kind IN ('calls','type_ref','member_ref')

			UNION ALL

			SELECT f.lang, 'imports',
				CASE WHEN EXISTS (
					SELECT 1 FROM intel_module_defs md
					WHERE md.lang=f.lang AND md.module=ir.module
				) THEN 'local_resolved' ELSE 'unknown' END,
				CASE WHEN EXISTS (
					SELECT 1 FROM intel_module_defs md
					WHERE md.lang=f.lang AND md.module=ir.module
				) THEN 'local-import:' || ir.module
				ELSE 'unknown-import:' || ir.module END
			FROM intel_import_refs ir
			JOIN files f ON f.path=ir.src_path
			WHERE NOT EXISTS (
				SELECT 1 FROM intel_external_import_evidence ie
				WHERE ie.src_path=ir.src_path AND ie.module=ir.module
			)

			UNION ALL

			SELECT f.lang, 'imports',
				CASE
					WHEN EXISTS (
						SELECT 1 FROM intel_module_defs md
						WHERE md.lang=f.lang AND md.module=ie.module
					) THEN 'local_resolved'
					WHEN et.target_kind IN ('runtime_builtin','runtime_global') THEN 'runtime_global_builtin'
					WHEN ie.evidence_kind IN ('runtime_builtin','runtime_global') THEN 'runtime_global_builtin'
					WHEN ie.evidence_kind IS NOT NULL THEN 'external_classified'
					ELSE 'unknown'
				END,
				CASE
					WHEN EXISTS (
						SELECT 1 FROM intel_module_defs md
						WHERE md.lang=f.lang AND md.module=ie.module
					) THEN 'local-import:' || ie.module
					WHEN ie.external_id IS NOT NULL THEN 'external:' || ie.external_id
					ELSE 'unknown-import:' || ie.module
				END
			FROM intel_external_import_evidence ie
			JOIN files f ON f.path=ie.src_path
			JOIN intel_external_targets et ON et.external_id=ie.external_id
		), grouped AS (
			SELECT lang, relation_kind, class,
				COUNT(*) AS associations,
				COUNT(DISTINCT target_key) AS unique_targets
			FROM classified
			GROUP BY lang, relation_kind, class
			UNION ALL
			SELECT lang, '*', class,
				COUNT(*) AS associations,
				COUNT(DISTINCT target_key) AS unique_targets
			FROM classified
			GROUP BY lang, class
		)
		SELECT lang, relation_kind, class, associations, unique_targets
		FROM grouped
		ORDER BY lang, relation_kind, class`)
	if err != nil {
		return fmt.Errorf("query reference classifications: %w", err)
	}
	defer rows.Close()

	if stats.ReferenceClassifications == nil {
		stats.ReferenceClassifications = map[string]ReferenceClassificationStats{}
	}
	for rows.Next() {
		var lang, relationKind, class string
		var associations, uniqueTargets int
		if err := rows.Scan(&lang, &relationKind, &class, &associations, &uniqueTargets); err != nil {
			return err
		}
		langStats := stats.ReferenceClassifications[lang]
		if langStats.ByKind == nil {
			langStats.ByKind = map[string]ReferenceKindClassificationStats{}
		}
		if relationKind == "*" {
			setClassificationCount(&langStats.Associations, class, associations)
			setClassificationCount(&langStats.UniqueTargets, class, uniqueTargets)
		} else {
			kindStats := langStats.ByKind[relationKind]
			setClassificationCount(&kindStats.Associations, class, associations)
			setClassificationCount(&kindStats.UniqueTargets, class, uniqueTargets)
			langStats.ByKind[relationKind] = kindStats
		}
		stats.ReferenceClassifications[lang] = langStats
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ensureExplicitReferenceClassificationVocabulary(stats)
	return nil
}

func externalReferenceStatsSchemaAvailable(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name IN (
			'intel_external_targets',
			'intel_external_symbol_evidence',
			'intel_external_import_evidence'
		)`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("inspect reference classification schema: %w", err)
	}
	return count == 3, nil
}

func queryLegacyReferenceClassifications(db *sql.DB, stats *CodeStats) error {
	// WHY: `rzm code stats` is a read-only diagnostic and may be run before the
	// next index rebuild migrates an existing database. Pre-v59 databases have
	// the local reference tables but no external evidence tables, so preserve
	// useful local/unknown counts instead of failing or mutating the index.
	rows, err := db.Query(`
		WITH classified(lang, relation_kind, class, target_key) AS MATERIALIZED (
			SELECT COALESCE(f.lang, rt.dst_lang), sr.ref_kind,
				CASE WHEN s.id IS NOT NULL THEN 'local_resolved' ELSE 'unknown' END,
				CASE WHEN s.id IS NOT NULL THEN 'local-symbol:' || sr.dst_target_id
					ELSE 'unknown-symbol:' || sr.dst_target_id END
			FROM intel_symbol_refs sr
			JOIN intel_symbol_ref_targets rt ON rt.target_id=sr.dst_target_id
			JOIN intel_symbol_ref_files rf ON rf.file_id=sr.src_file_id
			LEFT JOIN files f ON f.path=rf.path
			LEFT JOIN symbols s ON s.lang=rt.dst_lang AND s.fqn=rt.dst_fqn
			WHERE sr.ref_kind IN ('calls','type_ref','member_ref')

			UNION ALL

			SELECT f.lang, 'imports',
				CASE WHEN EXISTS (
					SELECT 1 FROM intel_module_defs md
					WHERE md.lang=f.lang AND md.module=ir.module
				) THEN 'local_resolved' ELSE 'unknown' END,
				CASE WHEN EXISTS (
					SELECT 1 FROM intel_module_defs md
					WHERE md.lang=f.lang AND md.module=ir.module
				) THEN 'local-import:' || ir.module
					ELSE 'unknown-import:' || ir.module END
			FROM intel_import_refs ir
			JOIN files f ON f.path=ir.src_path
		), grouped AS (
			SELECT lang, relation_kind, class, COUNT(*) associations,
				COUNT(DISTINCT target_key) unique_targets
			FROM classified GROUP BY lang, relation_kind, class
			UNION ALL
			SELECT lang, '*', class, COUNT(*) associations,
				COUNT(DISTINCT target_key) unique_targets
			FROM classified GROUP BY lang, class
		)
		SELECT lang, relation_kind, class, associations, unique_targets
		FROM grouped ORDER BY lang, relation_kind, class`)
	if err != nil {
		return fmt.Errorf("query legacy reference classifications: %w", err)
	}
	defer rows.Close()

	if stats.ReferenceClassifications == nil {
		stats.ReferenceClassifications = map[string]ReferenceClassificationStats{}
	}
	for rows.Next() {
		var lang, relationKind, class string
		var associations, uniqueTargets int
		if err := rows.Scan(&lang, &relationKind, &class, &associations, &uniqueTargets); err != nil {
			return err
		}
		langStats := stats.ReferenceClassifications[lang]
		if langStats.ByKind == nil {
			langStats.ByKind = map[string]ReferenceKindClassificationStats{}
		}
		if relationKind == "*" {
			setClassificationCount(&langStats.Associations, class, associations)
			setClassificationCount(&langStats.UniqueTargets, class, uniqueTargets)
		} else {
			kindStats := langStats.ByKind[relationKind]
			setClassificationCount(&kindStats.Associations, class, associations)
			setClassificationCount(&kindStats.UniqueTargets, class, uniqueTargets)
			langStats.ByKind[relationKind] = kindStats
		}
		stats.ReferenceClassifications[lang] = langStats
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ensureExplicitReferenceClassificationVocabulary(stats)
	return nil
}

func ensureExplicitReferenceClassificationVocabulary(stats *CodeStats) {
	// Languages are indexed independently of whether they emit durable refs.
	// Preserve that distinction by returning an explicit all-zero slice instead
	// of omitting a language and making zero look like unavailable data.
	for lang := range stats.Languages {
		if _, ok := stats.ReferenceClassifications[lang]; !ok {
			stats.ReferenceClassifications[lang] = ReferenceClassificationStats{}
		}
	}
	// Keep the relation vocabulary explicit even when a language has no rows of
	// a particular kind. This makes zero distinguishable from unsupported.
	for lang, langStats := range stats.ReferenceClassifications {
		if langStats.ByKind == nil {
			langStats.ByKind = map[string]ReferenceKindClassificationStats{}
		}
		for _, kind := range []string{"calls", "type_ref", "member_ref", "imports"} {
			if _, ok := langStats.ByKind[kind]; !ok {
				langStats.ByKind[kind] = ReferenceKindClassificationStats{}
			}
		}
		stats.ReferenceClassifications[lang] = langStats
	}
}

func setClassificationCount(counts *ClassificationCountSet, class string, value int) {
	switch class {
	case "local_resolved":
		counts.LocalResolved = value
	case "external_classified":
		counts.ExternalClassified = value
	case "runtime_global_builtin":
		counts.RuntimeGlobalBuiltin = value
	case "unknown":
		counts.Unknown = value
	}
}
