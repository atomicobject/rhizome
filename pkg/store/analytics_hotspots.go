package store

import (
	"context"
	"fmt"
	"strings"
)

// HotspotPackages ranks dependencies by fan-in (distinct caller files) and call volume.
// DocumentedSymbols counts how many used symbols can be resolved to an anchor with at least one mention.
//
// RATIONALE: Sparse-graph fallback gates: for languages whose resolution rate falls below
// the threshold, package rankings are supplemented from intel_symbol_refs so the language
// still surfaces. See DocCoverage rationale for the broader context.
func (s *Store) HotspotPackages(ctx context.Context, opts HotspotPackagesOptions) ([]HotspotPackageRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	threshold := SparseCallGraphThreshold
	if opts.FallbackThreshold != nil {
		threshold = *opts.FallbackThreshold
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	args := []any{threshold}
	var langFilter string
	if len(opts.Langs) > 0 {
		placeholders := strings.Repeat("?,", len(opts.Langs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		langFilter = fmt.Sprintf(" WHERE lang IN (%s)", placeholders)
		for _, l := range opts.Langs {
			args = append(args, l)
		}
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `
		WITH lang_resolution AS (
			SELECT
				t.dst_lang AS lang,
				CAST(COUNT(*) AS REAL) AS extracted,
				CAST(SUM(CASE WHEN sym.id IS NOT NULL THEN 1 ELSE 0 END) AS REAL) AS resolved
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			LEFT JOIN symbols sym ON sym.lang = t.dst_lang AND sym.fqn = t.dst_fqn
			WHERE r.ref_kind = 'calls'
			GROUP BY t.dst_lang
		),
		sparse_langs AS (
			SELECT lang FROM lang_resolution WHERE extracted > 0 AND (resolved / extracted) < ?
		),
		call_rows AS (
			SELECT
				callee.lang AS lang,
				COALESCE(sym.pkg, '') AS pkg,
				caller.path AS caller_file,
				callee.fqn AS fqn
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			LEFT JOIN symbols sym ON sym.fqn = callee.fqn
			WHERE e.kind = 'calls'
			  AND callee.lang IS NOT NULL AND callee.lang != ''
			  AND callee.fqn IS NOT NULL AND callee.fqn != ''
		),
		pkg_stats AS (
			SELECT
				lang, pkg,
				COUNT(*) AS calls,
				COUNT(DISTINCT caller_file) AS callers,
				COUNT(DISTINCT fqn) AS used_symbols,
				0 AS fallback
			FROM call_rows
			WHERE fqn IS NOT NULL
			GROUP BY lang, pkg
		),
		used_symbol_docs AS (
			SELECT DISTINCT
				cr.lang, cr.pkg, cr.fqn,
				CASE WHEN COUNT(e.dst_row_id) > 0 THEN 1 ELSE 0 END AS documented
			FROM call_rows cr
			LEFT JOIN intel_code_anchors a
				ON a.lang = cr.lang AND a.fqn = cr.fqn
			LEFT JOIN intel_edges e
				ON e.kind = 'mentions'
			   AND e.dst_type = 'anchor'
			   AND e.dst_row_id = a.id
			WHERE cr.fqn IS NOT NULL
			GROUP BY cr.lang, cr.pkg, cr.fqn
		),
		pkg_docs AS (
			SELECT lang, pkg, SUM(documented) AS documented_symbols
			FROM used_symbol_docs GROUP BY lang, pkg
		),
		fallback_pkg_stats AS (
			SELECT
				t.dst_lang AS lang,
				t.dst_pkg AS pkg,
				COUNT(*) AS calls,
				COUNT(DISTINCT r.src_file_id) AS callers,
				COUNT(DISTINCT t.dst_fqn) AS used_symbols,
				1 AS fallback
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			JOIN sparse_langs sl ON sl.lang = t.dst_lang
			WHERE r.ref_kind = 'calls' AND t.dst_fqn IS NOT NULL AND t.dst_fqn != ''
			GROUP BY t.dst_lang, t.dst_pkg
		),
		all_stats AS (
			SELECT lang, pkg, calls, callers, used_symbols, fallback FROM pkg_stats
			UNION ALL
			SELECT lang, pkg, calls, callers, used_symbols, fallback FROM fallback_pkg_stats
			WHERE NOT EXISTS (
				SELECT 1 FROM pkg_stats ps
				WHERE ps.lang = fallback_pkg_stats.lang AND ps.pkg = fallback_pkg_stats.pkg
			)
		)
		SELECT lang, pkg, calls, callers, used_symbols,
			COALESCE((SELECT documented_symbols FROM pkg_docs d WHERE d.lang = all_stats.lang AND d.pkg = all_stats.pkg), 0) AS documented_symbols,
			fallback
		FROM all_stats
		`+langFilter+`
		ORDER BY callers DESC, calls DESC, fallback ASC, pkg ASC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HotspotPackageRow
	for rows.Next() {
		var r HotspotPackageRow
		var fallback int
		if err := rows.Scan(&r.Lang, &r.Pkg, &r.Calls, &r.Callers, &r.UsedSymbols, &r.DocumentedSymbols, &fallback); err != nil {
			return nil, err
		}
		r.Fallback = fallback != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
