package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

func queryLanguageCounts(ctx context.Context, db *sql.DB, stats *CodeStats) error {
	rows, err := db.QueryContext(ctx, `SELECT lang, COUNT(*) AS symbols, COUNT(DISTINCT file) AS files FROM symbols GROUP BY lang`)
	if err != nil {
		return fmt.Errorf("query language counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var symbols, files int
		if err := rows.Scan(&lang, &symbols, &files); err != nil {
			return err
		}
		stats.Languages[lang] = LanguageCounts{Symbols: symbols, Files: files}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	modRows, err := db.QueryContext(ctx, `SELECT lang, COUNT(*) FROM intel_module_defs GROUP BY lang`)
	if err != nil {
		return fmt.Errorf("query module counts: %w", err)
	}
	defer modRows.Close()
	for modRows.Next() {
		var lang string
		var modules int
		if err := modRows.Scan(&lang, &modules); err != nil {
			return err
		}
		lc := stats.Languages[lang]
		lc.Modules = modules
		stats.Languages[lang] = lc
	}
	return modRows.Err()
}

func queryEdgeCategories(ctx context.Context, db *sql.DB, stats *CodeStats) error {
	// RATIONALE: the LEFT JOIN distinguishes "extracted" (we saw the call in the
	// AST) from "resolved" (the callee is also an indexed symbol). The diff is
	// the diagnostic signal — extracted but not resolved = either a real
	// extraction/resolution gap, or known-unindexable noise (language built-ins,
	// framework hook strings) the audit catalog filters out.
	callsQuery := `
		SELECT
			t.dst_lang AS lang,
			COUNT(*) AS extracted,
			SUM(CASE WHEN s.id IS NOT NULL THEN 1 ELSE 0 END) AS resolved
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
		LEFT JOIN symbols s ON s.lang = t.dst_lang AND s.fqn = t.dst_fqn
		WHERE r.ref_kind = ?
		GROUP BY t.dst_lang
	`

	rows, err := db.QueryContext(ctx, callsQuery, "calls")
	if err != nil {
		return fmt.Errorf("query call edges: %w", err)
	}
	for rows.Next() {
		var lang string
		var extracted, resolved int
		if err := rows.Scan(&lang, &extracted, &resolved); err != nil {
			rows.Close()
			return err
		}
		ec := stats.Edges[lang]
		ec.CallsExtracted = extracted
		ec.CallsResolved = resolved
		ec.CallsUnresolved = extracted - resolved
		if extracted > 0 {
			ec.ResolutionRate = float64(resolved) / float64(extracted)
		}
		stats.Edges[lang] = ec
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	memberRows, err := db.QueryContext(ctx, `
		SELECT t.dst_lang AS lang, COUNT(*) AS member_refs
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
		WHERE r.ref_kind = 'member_ref'
		GROUP BY t.dst_lang
	`)
	if err != nil {
		return fmt.Errorf("query member refs: %w", err)
	}
	for memberRows.Next() {
		var lang string
		var n int
		if err := memberRows.Scan(&lang, &n); err != nil {
			memberRows.Close()
			return err
		}
		ec := stats.Edges[lang]
		ec.MemberRefs = n
		stats.Edges[lang] = ec
	}
	memberRows.Close()
	if err := memberRows.Err(); err != nil {
		return err
	}

	// Imports are per-source-path; group by file extension via the files table.
	importRows, err := db.QueryContext(ctx, `
		SELECT f.lang, COUNT(*) AS imports
		FROM intel_import_refs i
		JOIN files f ON f.path = i.src_path
		GROUP BY f.lang
	`)
	if err != nil {
		return fmt.Errorf("query imports: %w", err)
	}
	defer importRows.Close()
	for importRows.Next() {
		var lang string
		var n int
		if err := importRows.Scan(&lang, &n); err != nil {
			return err
		}
		ec := stats.Edges[lang]
		ec.Imports = n
		stats.Edges[lang] = ec
	}
	return importRows.Err()
}

func queryTopUnresolvedCallees(ctx context.Context, db *sql.DB, stats *CodeStats, topN int, exclude map[string]bool) error {
	// Pull more rows than topN so we can drop excluded names without truncating
	// the user-visible list below the requested cap. fetchLimit is max(topN*4,
	// topN+50); actual LIMIT is fetchLimit*8 (~32-48x topN) so per-lang
	// filtering still leaves enough rows after catalog exclusions.
	fetchLimit := topN * 4
	if fetchLimit < topN+50 {
		fetchLimit = topN + 50
	}
	rows, err := db.QueryContext(ctx, `
		SELECT t.dst_lang, t.dst_name, COUNT(*) AS calls
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
		LEFT JOIN symbols s ON s.lang = t.dst_lang AND s.fqn = t.dst_fqn
		WHERE r.ref_kind = 'calls' AND s.id IS NULL
		GROUP BY t.dst_lang, t.dst_name
		ORDER BY calls DESC
		LIMIT ?
	`, fetchLimit*8)
	if err != nil {
		return fmt.Errorf("query unresolved callees: %w", err)
	}
	defer rows.Close()

	perLang := map[string][]UnresolvedCallee{}
	for rows.Next() {
		var lang, name string
		var calls int
		if err := rows.Scan(&lang, &name, &calls); err != nil {
			return err
		}
		if exclude != nil && exclude[name] {
			continue
		}
		perLang[lang] = append(perLang[lang], UnresolvedCallee{Name: name, Calls: calls})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for lang, list := range perLang {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Calls > list[j].Calls })
		if len(list) > topN {
			list = list[:topN]
		}
		stats.UnresolvedCallees[lang] = list
	}
	return nil
}

func queryByDirectory(ctx context.Context, db *sql.DB, stats *CodeStats) error {
	rows, err := db.QueryContext(ctx, `
		SELECT
			CASE
				WHEN instr(file, '/') > 0 THEN substr(file, 1, instr(file, '/') - 1)
				ELSE '.'
			END AS dir,
			lang,
			COUNT(DISTINCT file) AS files
		FROM symbols
		GROUP BY dir, lang
		ORDER BY files DESC
		LIMIT 50
	`)
	if err != nil {
		return fmt.Errorf("query by directory: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var dir, lang string
		var files int
		if err := rows.Scan(&dir, &lang, &files); err != nil {
			return err
		}
		stats.ByDirectory[lang] = append(stats.ByDirectory[lang], DirectoryDistribution{
			Dir:   dir,
			Lang:  lang,
			Files: files,
		})
	}
	return rows.Err()
}
