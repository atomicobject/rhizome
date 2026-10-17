package store

import (
	"context"
	"fmt"
	"strings"
)

type HotspotFileRow struct {
	File        string
	Calls       int
	Deps        int
	UsedSymbols int
	// Fallback marks rows surfaced via the sparse-graph fallback path. See
	// SparseCallGraphThreshold for the gating mechanism.
	Fallback bool
}

// HotspotFilesOptions tunes HotspotFiles behavior.
type HotspotFilesOptions struct {
	Limit             int
	Langs             []string
	FallbackThreshold *float64
}

// HotspotFiles ranks caller files by fan-out (distinct dependency packages) and call volume.
// See HotspotPackages rationale for the sparse-graph fallback semantics.
func (s *Store) HotspotFiles(ctx context.Context, opts HotspotFilesOptions) ([]HotspotFileRow, error) {
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
		// Lang filter applied via JOIN to intel_code_anchors (resolved path) /
		// intel_symbol_ref_targets (fallback path). For uniformity we wrap the
		// final SELECT to filter — needs the lang column to be present in the
		// SELECT list. The query below carries a `lang` column from both sides.
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
				caller.path AS file,
				COALESCE(sym.pkg, '') AS pkg,
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
			  AND caller.path IS NOT NULL AND caller.path != ''
			  AND callee.fqn IS NOT NULL AND callee.fqn != ''
		),
		resolved_files AS (
			SELECT
				lang,
				file,
				COUNT(*) AS calls,
				COUNT(DISTINCT pkg) AS deps,
				COUNT(DISTINCT fqn) AS used_symbols,
				0 AS fallback
			FROM call_rows
			WHERE fqn IS NOT NULL
			GROUP BY lang, file
		),
		fallback_files AS (
			SELECT
				t.dst_lang AS lang,
				f.path AS file,
				COUNT(*) AS calls,
				COUNT(DISTINCT t.dst_pkg) AS deps,
				COUNT(DISTINCT t.dst_fqn) AS used_symbols,
				1 AS fallback
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
			JOIN sparse_langs sl ON sl.lang = t.dst_lang
			WHERE r.ref_kind = 'calls' AND t.dst_fqn IS NOT NULL AND t.dst_fqn != ''
			GROUP BY t.dst_lang, f.path
		),
		all_files AS (
			SELECT lang, file, calls, deps, used_symbols, fallback FROM resolved_files
			UNION ALL
			SELECT lang, file, calls, deps, used_symbols, fallback FROM fallback_files
		),
		ranked_files AS (
			SELECT lang, file, calls, deps, used_symbols, fallback
			FROM all_files f1
			WHERE NOT EXISTS (
				SELECT 1 FROM all_files f2
				WHERE f2.lang = f1.lang
				  AND f2.file = f1.file
				  AND (
				    f2.deps > f1.deps
				    OR (f2.deps = f1.deps AND f2.calls > f1.calls)
				    OR (f2.deps = f1.deps AND f2.calls = f1.calls AND f2.used_symbols > f1.used_symbols)
				    OR (f2.deps = f1.deps AND f2.calls = f1.calls AND f2.used_symbols = f1.used_symbols AND f2.fallback < f1.fallback)
				  )
			)
		)
		SELECT file, calls, deps, used_symbols, fallback
		FROM ranked_files
		`+langFilter+`
		ORDER BY deps DESC, calls DESC, fallback ASC, file ASC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HotspotFileRow
	for rows.Next() {
		var r HotspotFileRow
		var fallback int
		if err := rows.Scan(&r.File, &r.Calls, &r.Deps, &r.UsedSymbols, &fallback); err != nil {
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
