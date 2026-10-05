package sqlite

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// SparseCallGraphThreshold gates the fallback path in the hotspot + doc_coverage
// reports. Below this resolution-rate (callsResolved / callsExtracted, per language),
// the queries also include rows sourced from intel_symbol_refs (raw extracted refs
// that may not have anchor-resolved on either end).
//
// WHY: 0.60 chosen empirically from wp-tst measurements (2026-06-15). PHP at 0.41
// post-EFF-0037 still produced JS-only hotspots/doc_coverage output because the
// anchor-join scoring path collapses to zero authority when most calls don't resolve.
// TS at 0.0036 hits the same wall. The rhizome repo's own Go index sits near 1.0
// and is unaffected. 0.60 is conservative — anything materially below it indicates
// a sparse-graph language where the fallback signal is more useful than the
// resolved-only signal. Override per-invocation via the FallbackThreshold field on
// the options struct or the --fallback-threshold CLI flag. See EFF-0038 + SPEC-0073.
const SparseCallGraphThreshold = 0.60

type DocCoverageRow struct {
	Lang     string
	Kind     string
	FQN      string
	Path     string
	Calls    int
	Callers  int
	Mentions int
	Links    int
	Resolved bool
	// Fallback is true when this row was surfaced via the sparse-graph fallback
	// path (intel_symbol_refs-derived rather than intel_edges-derived). Downstream
	// consumers (assessor skill, dashboards) use this to distinguish resolved-edge
	// rankings from fallback rankings.
	Fallback bool
}

type DocCoverageOptions struct {
	Limit int
	// Langs restricts results to the named language codes (e.g. "php", "go"). Empty = all.
	Langs []string
	// FallbackThreshold overrides SparseCallGraphThreshold when non-nil. Per-invocation
	// tuning for the sparse-graph fallback gating.
	FallbackThreshold *float64
	// PathPrefixes applies a prefix match against caller paths (vault-relative).
	PathPrefixes []string
	// FQNs narrows results to specific fully-qualified symbols.
	FQNs []string
}

// DocCoverage returns the most-used called symbols, joined with doc mention counts when resolvable.
//
// Usage is derived from `intel_edges(kind='calls')`; documentation coverage comes from `mentions` edges
// in `intel_edges` and requires a matching `intel_code_anchors` row (by lang+fqn).
//
// RATIONALE: When a language's call-resolution rate (callsResolved / callsExtracted from
// intel_symbol_refs) falls below the threshold (default SparseCallGraphThreshold, override
// via opts.FallbackThreshold), the query supplements its scoring with rows derived from
// intel_symbol_refs. This is the sparse-graph fallback: the existing anchor-join path
// produces zero rows for sparse-graph languages (e.g. wp-tst PHP at 0.41 — the anchor
// joins drop most calls because neither end resolves to an anchor). Surfacing the raw
// extracted refs gives the assessor skill a meaningful ranking even when the resolved-
// edge call graph is thin. Languages above threshold use the existing query path unchanged.
func (s *Store) DocCoverage(ctx context.Context, opts DocCoverageOptions) ([]DocCoverageRow, error) {
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

	callWhere := []string{
		"e.kind = 'calls'",
		"callee.fqn IS NOT NULL AND callee.fqn != ''",
	}
	// Path filter shared between the resolved path and the fallback path. Each path
	// applies it to its own caller-file column (caller.path vs. intel_symbol_ref_files.path)
	// so the user-visible filter is consistent regardless of which scoring branch runs.
	var pathPrefixes []string
	for _, p := range opts.PathPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = string(paths.NormalizeCode(p))
		if p == "" {
			continue
		}
		pathPrefixes = append(pathPrefixes, p)
	}
	if len(pathPrefixes) > 0 {
		var pathConds []string
		for _, p := range pathPrefixes {
			pathConds = append(pathConds, "caller.path LIKE ?")
			args = append(args, p+"%")
		}
		callWhere = append(callWhere, fmt.Sprintf("(%s)", strings.Join(pathConds, " OR ")))
	}
	callWhereClause := strings.Join(callWhere, " AND ")

	var langFilter string
	if len(opts.Langs) > 0 {
		placeholders := strings.Repeat("?,", len(opts.Langs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		langFilter = fmt.Sprintf(" AND u.lang IN (%s)", placeholders)
	}

	// Fallback-side path filter binds: same prefixes against intel_symbol_ref_files.path.
	var fallbackPathFilter string
	if len(pathPrefixes) > 0 {
		var conds []string
		for _, p := range pathPrefixes {
			conds = append(conds, "f.path LIKE ?")
			args = append(args, p+"%")
		}
		fallbackPathFilter = " AND (" + strings.Join(conds, " OR ") + ")"
	}

	// Lang filter args after threshold (so the order matches the SQL).
	for _, l := range opts.Langs {
		args = append(args, l)
	}

	var fqnFilter string
	if len(opts.FQNs) > 0 {
		placeholders := strings.Repeat("?,", len(opts.FQNs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		fqnFilter = fmt.Sprintf(" AND u.fqn IN (%s)", placeholders)
		for _, f := range opts.FQNs {
			args = append(args, f)
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH lang_resolution AS (
			-- Per-language resolution rate from intel_symbol_refs; mirrors rzm code stats.
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
			-- Languages where the fallback path should activate.
			SELECT lang FROM lang_resolution
			WHERE extracted > 0 AND (resolved / extracted) < ?
		),
		call_usage AS (
			SELECT
				callee.lang AS lang,
				callee.fqn AS fqn,
				COUNT(*) AS calls,
				COUNT(DISTINCT caller.path) AS callers,
				0 AS fallback
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE `+callWhereClause+`
			GROUP BY callee.lang, callee.fqn
		),
		fallback_usage AS (
			SELECT
				t.dst_lang AS lang,
				t.dst_fqn AS fqn,
				COUNT(*) AS calls,
				COUNT(DISTINCT r.src_file_id) AS callers,
				1 AS fallback
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			JOIN sparse_langs sl ON sl.lang = t.dst_lang
			LEFT JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
			WHERE r.ref_kind = 'calls' AND t.dst_fqn IS NOT NULL AND t.dst_fqn != ''`+fallbackPathFilter+`
			GROUP BY t.dst_lang, t.dst_fqn
		),
		all_usage AS (
			SELECT lang, fqn, calls, callers, fallback FROM call_usage
			UNION ALL
			SELECT lang, fqn, calls, callers, fallback FROM fallback_usage
		),
		ranked_usage AS (
			-- One row per (lang, fqn): keep whichever signal has the stronger
			-- usage count. Ties prefer resolved rows so high-resolution languages
			-- keep their original behavior.
			SELECT lang, fqn, calls, callers, fallback
			FROM all_usage u1
			WHERE NOT EXISTS (
			        SELECT 1 FROM all_usage u2
			        WHERE u2.lang = u1.lang
			          AND u2.fqn = u1.fqn
			          AND (
			            u2.calls > u1.calls
			            OR (u2.calls = u1.calls AND u2.callers > u1.callers)
			            OR (u2.calls = u1.calls AND u2.callers = u1.callers AND u2.fallback < u1.fallback)
			          )
			      )
		),
		mentions AS (
			SELECT a.anchor_id AS anchor_id, COUNT(*) AS mentions
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
			GROUP BY a.anchor_id
		),
		doc_links_counts AS (
			SELECT dst_id AS anchor_id, COUNT(*) AS links
			FROM doc_links
			WHERE dst_kind = 'anchor'
			GROUP BY dst_id
		)
		SELECT
			u.lang,
			COALESCE(a.kind, '') AS kind,
			u.fqn,
			COALESCE(a.path, '') AS path,
			u.calls,
			u.callers,
			COALESCE(m.mentions, 0) AS mentions,
			COALESCE(dl.links, 0) AS links,
			CASE WHEN a.anchor_id IS NULL THEN 0 ELSE 1 END AS resolved,
			u.fallback
		FROM ranked_usage u
		LEFT JOIN intel_code_anchors a
			ON a.lang = u.lang AND a.fqn = u.fqn
		LEFT JOIN mentions m
			ON m.anchor_id = a.anchor_id
		LEFT JOIN doc_links_counts dl
			ON dl.anchor_id = a.anchor_id
		WHERE u.fqn IS NOT NULL
		`+langFilter+fqnFilter+`
		ORDER BY u.calls DESC, resolved DESC, u.fallback ASC, mentions ASC, u.fqn ASC
		LIMIT ?
	`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DocCoverageRow
	for rows.Next() {
		var r DocCoverageRow
		var resolved, fallback int
		if err := rows.Scan(&r.Lang, &r.Kind, &r.FQN, &r.Path, &r.Calls, &r.Callers, &r.Mentions, &r.Links, &resolved, &fallback); err != nil {
			return nil, err
		}
		r.Resolved = resolved != 0
		r.Fallback = fallback != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type HotspotPackageRow struct {
	Lang              string
	Pkg               string
	Calls             int
	Callers           int
	UsedSymbols       int
	DocumentedSymbols int
	// Fallback marks rows surfaced via the sparse-graph fallback path. See
	// SparseCallGraphThreshold for the gating mechanism.
	Fallback bool
}

type ComplexityRow struct {
	Lang      string
	Kind      string
	FQN       string
	Path      string
	SpanLines int
	CallsIn   int
	Callers   int
	CallsOut  int
	Callees   int
	Score     float64
}

type ComplexityOptions struct {
	Limit int
	// PathPrefixes applies a prefix match against intel_code_anchors.path (relative or absolute).
	PathPrefixes []string
	// MinLines filters out anchors with spans shorter than this value.
	MinLines int
}

// HotspotPackagesOptions tunes HotspotPackages behavior.
type HotspotPackagesOptions struct {
	Limit int
	// Langs restricts results to the named language codes. Empty = all.
	Langs []string
	// FallbackThreshold overrides SparseCallGraphThreshold when non-nil.
	FallbackThreshold *float64
}

// RationaleAttentionOptions controls the rationale review report.
type RationaleAttentionOptions struct {
	Limit int
	// PathPrefixes applies a prefix match against rationale paths (vault-relative).
	PathPrefixes []string
}

// RationaleAttentionRow is a deterministic finding for comments/symbols that
// need human or research-backed documentation attention.
type RationaleAttentionRow struct {
	Path      string
	SymbolFQN string
	Kind      string
	Line      int64
	Content   string
	Calls     int
	Callers   int
	Mentions  int
	Links     int
	Attention int
	Reasons   []string
}

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

// ComplexityHotspots ranks symbol-level hotspots by size and call fan-in/out.
func (s *Store) ComplexityHotspots(ctx context.Context, opts ComplexityOptions) ([]ComplexityRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	anchorWhere := []string{
		"a.fqn IS NOT NULL AND a.fqn != ''",
		"a.kind IN ('function', 'method')",
	}
	var args []any
	if len(opts.PathPrefixes) > 0 {
		var pathConds []string
		for _, p := range opts.PathPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			pathConds = append(pathConds, "a.path LIKE ?")
			args = append(args, p+"%")
		}
		if len(pathConds) > 0 {
			anchorWhere = append(anchorWhere, fmt.Sprintf("(%s)", strings.Join(pathConds, " OR ")))
		}
	}
	anchorWhereClause := strings.Join(anchorWhere, " AND ")

	minLinesClause := ""
	if opts.MinLines > 0 {
		minLinesClause = "WHERE b.span_lines >= ?"
		args = append(args, opts.MinLines)
	}

	query := fmt.Sprintf(`
		WITH anchor_base AS (
			SELECT
				a.anchor_id,
				a.lang,
				a.kind,
				a.fqn,
				a.path,
				CASE
					WHEN a.end_line >= a.start_line AND a.start_line > 0 THEN (a.end_line - a.start_line + 1)
					ELSE 1
				END AS span_lines
			FROM intel_code_anchors a
			WHERE %s
		),
		call_in AS (
			SELECT
				callee.lang AS lang,
				callee.fqn AS fqn,
				COUNT(*) AS calls_in,
				COUNT(DISTINCT caller.path) AS callers
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			  AND callee.lang IS NOT NULL AND callee.lang != ''
			  AND callee.fqn IS NOT NULL AND callee.fqn != ''
			GROUP BY callee.lang, callee.fqn
		),
		call_out AS (
			SELECT
				caller.lang AS lang,
				caller.fqn AS fqn,
				COUNT(*) AS calls_out,
				COUNT(DISTINCT callee.fqn) AS callees
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			  AND caller.fqn IS NOT NULL AND caller.fqn != ''
			  AND callee.fqn IS NOT NULL AND callee.fqn != ''
			GROUP BY caller.lang, caller.fqn
		)
		SELECT
			b.lang,
			b.kind,
			b.fqn,
			b.path,
			b.span_lines,
			COALESCE(ci.calls_in, 0) AS calls_in,
			COALESCE(ci.callers, 0) AS callers,
			COALESCE(co.calls_out, 0) AS calls_out,
			COALESCE(co.callees, 0) AS callees,
			(b.span_lines * (1 + COALESCE(ci.callers, 0)) * (1 + (COALESCE(co.callees, 0) / 5.0))) AS score
		FROM anchor_base b
		LEFT JOIN call_in ci
			ON ci.lang = b.lang AND ci.fqn = b.fqn
		LEFT JOIN call_out co
			ON co.lang = b.lang AND co.fqn = b.fqn
		%s
		ORDER BY score DESC, b.span_lines DESC, callers DESC, callees DESC, b.path ASC, b.fqn ASC
		LIMIT ?
	`, anchorWhereClause, minLinesClause)

	rows, err := s.db.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ComplexityRow
	for rows.Next() {
		var r ComplexityRow
		if err := rows.Scan(
			&r.Lang, &r.Kind, &r.FQN, &r.Path, &r.SpanLines,
			&r.CallsIn, &r.Callers, &r.CallsOut, &r.Callees, &r.Score,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) RationaleAttention(ctx context.Context, opts RationaleAttentionOptions) ([]RationaleAttentionRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	where := []string{"1=1"}
	var args []any
	if len(opts.PathPrefixes) > 0 {
		var pathConds []string
		for _, p := range opts.PathPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			p = string(paths.NormalizeCode(p))
			if p == "" {
				continue
			}
			pathConds = append(pathConds, "r.path LIKE ?")
			args = append(args, p+"%")
		}
		if len(pathConds) > 0 {
			where = append(where, fmt.Sprintf("(%s)", strings.Join(pathConds, " OR ")))
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH call_in AS (
			SELECT
				callee.anchor_id,
				COUNT(*) AS calls,
				COUNT(DISTINCT caller.path) AS callers
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			GROUP BY callee.anchor_id
		),
		mentions AS (
			SELECT a.anchor_id AS anchor_id, COUNT(*) AS mentions
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
			GROUP BY a.anchor_id
		),
		doc_links_counts AS (
			SELECT dst_id AS anchor_id, COUNT(*) AS links
			FROM doc_links
			WHERE dst_kind = 'anchor'
			GROUP BY dst_id
		),
		rationale_anchor_stats AS (
			SELECT
				r.rationale_id,
				MAX(COALESCE(ci.calls, 0)) AS calls,
				MAX(COALESCE(ci.callers, 0)) AS callers,
				MAX(COALESCE(m.mentions, 0)) AS mentions,
				MAX(COALESCE(dl.links, 0)) AS links
			FROM intel_rationale r
			LEFT JOIN intel_code_anchors a
			  ON r.symbol_fqn IS NOT NULL
			 AND r.symbol_fqn != ''
			 AND (a.fqn = r.symbol_fqn OR a.fqn LIKE ('%.' || r.symbol_fqn))
			LEFT JOIN call_in ci ON ci.anchor_id = a.anchor_id
			LEFT JOIN mentions m ON m.anchor_id = a.anchor_id
			LEFT JOIN doc_links_counts dl ON dl.anchor_id = a.anchor_id
			GROUP BY r.rationale_id
		)
		SELECT
			r.path,
			COALESCE(r.symbol_fqn, '') AS symbol_fqn,
			r.kind,
			r.start_line,
			r.content,
			COALESCE(ras.calls, 0) AS calls,
			COALESCE(ras.callers, 0) AS callers,
			COALESCE(ras.mentions, 0) AS mentions,
			COALESCE(ras.links, 0) AS links
		FROM intel_rationale r
		LEFT JOIN rationale_anchor_stats ras ON ras.rationale_id = r.rationale_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY
			CASE r.kind
				WHEN 'fixme' THEN 0
				WHEN 'todo' THEN 1
				WHEN 'hack' THEN 2
				WHEN 'important' THEN 3
				ELSE 4
			END,
			callers DESC,
			calls DESC,
			r.path ASC,
			r.start_line ASC
		LIMIT ?
	`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RationaleAttentionRow, 0, limit)
	for rows.Next() {
		var r RationaleAttentionRow
		if err := rows.Scan(&r.Path, &r.SymbolFQN, &r.Kind, &r.Line, &r.Content, &r.Calls, &r.Callers, &r.Mentions, &r.Links); err != nil {
			return nil, err
		}
		r.Content = trimAnalyticsSnippet(r.Content, 240)
		r.Reasons, r.Attention = rationaleAttentionReasons(r)
		if len(r.Reasons) == 0 {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func rationaleAttentionReasons(r RationaleAttentionRow) ([]string, int) {
	var reasons []string
	score := 0
	switch strings.ToLower(strings.TrimSpace(r.Kind)) {
	case "todo", "fixme":
		reasons = append(reasons, "todo_or_fixme")
		score += 5
	case "hack":
		reasons = append(reasons, "hack")
		score += 4
	case "important":
		reasons = append(reasons, "important_marker")
		score += 3
	}
	if strings.TrimSpace(r.SymbolFQN) == "" {
		reasons = append(reasons, "file_level_rationale")
		score++
	}
	if r.Callers >= 2 || r.Calls >= 3 {
		reasons = append(reasons, "high_fanin_symbol")
		score += 3
	}
	if strings.TrimSpace(r.SymbolFQN) != "" && r.Mentions == 0 && r.Links == 0 {
		reasons = append(reasons, "undocumented_symbol")
		score += 2
	}
	return reasons, score
}

func trimAnalyticsSnippet(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if limit <= 0 || len(s) <= limit {
		return s
	}
	trimAt := limit
	for trimAt > 0 && !utf8.RuneStart(s[trimAt]) {
		trimAt--
	}
	return s[:trimAt] + "..."
}

func (s *Store) ensureAnalyticsTables(ctx context.Context) error {
	// Cheap existence check that yields a helpful error instead of "no such table" deep in queries.
	_, err := s.db.ExecContext(ctx, `SELECT 1 FROM intel_code_anchors LIMIT 1`)
	if err != nil {
		return fmt.Errorf("intel tables unavailable (run `rzm index`): %w", err)
	}
	_, err = s.db.ExecContext(ctx, `SELECT 1 FROM intel_edges LIMIT 1`)
	if err != nil {
		return fmt.Errorf("intel edges unavailable (run `rzm index`): %w", err)
	}
	_, err = s.db.ExecContext(ctx, `SELECT 1 FROM intel_fts LIMIT 1`)
	if err != nil {
		return fmt.Errorf("intel FTS unavailable (run `rzm index`): %w", err)
	}
	return nil
}

// DocStatsForFQNs returns documentation coverage stats for the provided FQNs, keyed by fqn.
// Requires intel tables (see ensureAnalyticsTables).
func (s *Store) DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error) {
	if len(fqns) == 0 {
		return map[string]codeanchor.DocStats{}, nil
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	placeholders := strings.Repeat("?,", len(fqns))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]any, 0, 1+len(fqns))
	args = append(args, lang)
	for _, f := range fqns {
		args = append(args, f)
	}

	query := fmt.Sprintf(`
		WITH mentions AS (
			SELECT a.anchor_id AS anchor_id, COUNT(*) AS mentions
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
			GROUP BY a.anchor_id
		),
		doc_links_counts AS (
			SELECT dst_id AS anchor_id, COUNT(*) AS links
			FROM doc_links
			WHERE dst_kind = 'anchor'
			GROUP BY dst_id
		)
		SELECT
			a.fqn,
			COALESCE(a.kind, '') AS kind,
			COALESCE(a.path, '') AS path,
			COALESCE(m.mentions, 0) AS mentions,
			COALESCE(dl.links, 0) AS links,
			CASE WHEN a.anchor_id IS NULL THEN 0 ELSE 1 END AS resolved
		FROM intel_code_anchors a
		LEFT JOIN mentions m
			ON m.anchor_id = a.anchor_id
		LEFT JOIN doc_links_counts dl
			ON dl.anchor_id = a.anchor_id
		WHERE a.lang = ?
		  AND a.fqn IN (%s)
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]codeanchor.DocStats, len(fqns))
	for rows.Next() {
		var fqn, kind, path string
		var mentions, links int
		var resolvedInt int
		if err := rows.Scan(&fqn, &kind, &path, &mentions, &links, &resolvedInt); err != nil {
			return nil, err
		}
		out[fqn] = codeanchor.DocStats{
			Kind:     kind,
			Path:     path,
			Mentions: mentions,
			Links:    links,
			Resolved: resolvedInt != 0,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SymbolMetaForFQNs returns symbol metadata for the provided FQNs, keyed by fqn.
func (s *Store) SymbolMetaForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.SymbolMeta, error) {
	if len(fqns) == 0 {
		return map[string]codeanchor.SymbolMeta{}, nil
	}

	placeholders := strings.Repeat("?,", len(fqns))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]any, 0, 1+len(fqns))
	args = append(args, lang)
	for _, f := range fqns {
		args = append(args, f)
	}

	query := fmt.Sprintf(`
		SELECT fqn, kind, file, COALESCE(pkg, ''), name
		FROM symbols
		WHERE lang = ?
		  AND fqn IN (%s)
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]codeanchor.SymbolMeta, len(fqns))
	for rows.Next() {
		var fqn, kind, file, pkg, name string
		if err := rows.Scan(&fqn, &kind, &file, &pkg, &name); err != nil {
			return nil, err
		}
		out[fqn] = codeanchor.SymbolMeta{
			Lang:     codeanchor.Lang(lang),
			Kind:     codeanchor.SymbolKind(kind),
			File:     file,
			Pkg:      pkg,
			Name:     name,
			FQN:      fqn,
			Exported: isExportedGoName(name),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func isExportedGoName(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}
