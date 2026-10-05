package cmd

// WHY:
// `rzm code stats` is the diagnostic instrument for SPEC-0070 (PHP indexer
// language-coverage audit) and the foundation for future per-language audits.
// It surfaces the extraction-vs-resolution gap that motivates the audit loop:
// tree-sitter extracts call sites, but resolution against the indexed symbol
// table is where edges are lost.
//
// The JSON output shape is the agent-facing contract — `legacy-codebase-assessor`
// (SPEC-0069) and future audit skills consume it. Field-name changes here need
// corresponding updates to those skills and to cmd/code_stats_test.go, which
// locks the contract.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/spf13/cobra"
)

var (
	codeStatsText          bool
	codeStatsUnresolvedTop int
	codeStatsExcludeFrom   string
)

// CodeStats is the JSON output contract for `rzm code stats`.
// IMPORTANT: agent-facing contract. See cmd/code_stats_test.go for the lock.
type CodeStats struct {
	Vault                    string                                  `json:"vault"`
	IndexPath                string                                  `json:"indexPath"`
	Languages                map[string]LanguageCounts               `json:"languages"`
	Edges                    map[string]EdgeCounts                   `json:"edges"`
	ReferenceClassifications map[string]ReferenceClassificationStats `json:"referenceClassifications"`
	UnresolvedCallees        map[string][]UnresolvedCallee           `json:"unresolvedCallees"`
	ByDirectory              map[string][]DirectoryDistribution      `json:"byDirectory"`
}

// LanguageCounts summarizes what got indexed for a single language.
type LanguageCounts struct {
	Symbols int `json:"symbols"`
	Files   int `json:"files"`
	Modules int `json:"modules"`
}

// EdgeCounts measures the extraction-vs-resolution gap for a single language.
// RATIONALE: the LEFT JOIN distinction between extracted and resolved is the
// load-bearing diagnostic signal. CallsExtracted - CallsResolved = gap surface.
type EdgeCounts struct {
	CallsExtracted  int     `json:"callsExtracted"`
	CallsResolved   int     `json:"callsResolved"`
	CallsUnresolved int     `json:"callsUnresolved"`
	ResolutionRate  float64 `json:"resolutionRate"`
	MemberRefs      int     `json:"memberRefs"`
	Imports         int     `json:"imports"`
}

// UnresolvedCallee is one row in the per-language top-N unresolved-callees list.
// NOTE: language built-ins (e.g. PHP `empty`, `isset`, `sprintf`) dominate this
// list. The audit phase classifies them via a coverage catalog and filters via
// the --exclude-from flag.
type UnresolvedCallee struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}

// DirectoryDistribution shows top-level directory file distribution per language.
type DirectoryDistribution struct {
	Dir   string `json:"dir"`
	Lang  string `json:"lang"`
	Files int    `json:"files"`
}

var codeStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show structured per-language indexer coverage statistics",
	Long: `Outputs structured statistics from the code index: per-language symbol/file/module counts, edge resolution rates, top-N unresolved callees, and directory file distribution.

JSON is the default output (machine-readable, agent-consumable). Use --text for a compact human-readable summary.

The --exclude-from <catalog> flag accepts a markdown coverage catalog (see SPEC-0070) and filters names listed under language-builtin, framework-noise, or un-resolvable-pattern sections from the unresolvedCallees output. Used by audit skills to surface real-gap signal without language built-in noise. This filter is purely diagnostic; underlying index data is unchanged.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		if !codeCfg.Enabled {
			return fmt.Errorf("code index not enabled in this vault's config")
		}
		if _, err := os.Stat(codeCfg.IndexPath); err != nil {
			return fmt.Errorf("code index file not found at %s; run `rzm index` first", codeCfg.IndexPath)
		}

		db, err := sqliteutil.OpenDSN(sqliteutil.DSN(codeCfg.IndexPath), sqliteutil.Options{})
		if err != nil {
			return fmt.Errorf("open code index: %w", err)
		}
		defer db.Close()

		stats := CodeStats{
			Vault:                    vaultPath,
			IndexPath:                codeCfg.IndexPath,
			Languages:                map[string]LanguageCounts{},
			Edges:                    map[string]EdgeCounts{},
			ReferenceClassifications: map[string]ReferenceClassificationStats{},
			UnresolvedCallees:        map[string][]UnresolvedCallee{},
			ByDirectory:              map[string][]DirectoryDistribution{},
		}

		if err := queryLanguageCounts(db, &stats); err != nil {
			return err
		}
		if err := queryEdgeCategories(db, &stats); err != nil {
			return err
		}
		if err := queryReferenceClassifications(db, &stats); err != nil {
			return err
		}

		excludeSet, err := loadExcludeCatalog(codeStatsExcludeFrom)
		if err != nil {
			return err
		}
		if err := queryTopUnresolvedCallees(db, &stats, codeStatsUnresolvedTop, excludeSet); err != nil {
			return err
		}
		if err := queryByDirectory(db, &stats); err != nil {
			return err
		}

		if codeStatsText {
			writeTextOutput(os.Stdout, &stats)
			return nil
		}
		return writeJSONOutput(os.Stdout, &stats)
	},
}

func queryLanguageCounts(db *sql.DB, stats *CodeStats) error {
	rows, err := db.Query(`SELECT lang, COUNT(*) AS symbols, COUNT(DISTINCT file) AS files FROM symbols GROUP BY lang`)
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

	modRows, err := db.Query(`SELECT lang, COUNT(*) FROM intel_module_defs GROUP BY lang`)
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

func queryEdgeCategories(db *sql.DB, stats *CodeStats) error {
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

	rows, err := db.Query(callsQuery, "calls")
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

	memberRows, err := db.Query(`
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
	importRows, err := db.Query(`
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

func queryTopUnresolvedCallees(db *sql.DB, stats *CodeStats, topN int, exclude map[string]bool) error {
	// Pull more rows than topN so we can drop excluded names without truncating
	// the user-visible list below the requested cap. fetchLimit is max(topN*4,
	// topN+50); actual LIMIT is fetchLimit*8 (~32-48x topN) so per-lang
	// filtering still leaves enough rows after catalog exclusions.
	fetchLimit := topN * 4
	if fetchLimit < topN+50 {
		fetchLimit = topN + 50
	}
	rows, err := db.Query(`
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

func queryByDirectory(db *sql.DB, stats *CodeStats) error {
	rows, err := db.Query(`
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

// loadExcludeCatalog parses a coverage catalog (markdown) and returns the set
// of names under the language-builtin, framework-noise, and un-resolvable-pattern
// sections. real-gap entries are NOT excluded — those are the audit targets.
//
// Catalog format (per SPEC-0070 US2):
//
//	## language-builtin
//	- name1 — rationale
//	- name2 — rationale
//
//	## framework-noise
//	- ...
//
// The parser is intentionally simple: heading match by H2 text, bullets are
// "- <name>" or "- `<name>`" optionally followed by " — rationale" or similar.
func loadExcludeCatalog(path string) (map[string]bool, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read exclude catalog %s: %w", path, err)
	}
	excludeSections := map[string]bool{
		"language-builtin":      true,
		"framework-noise":       true,
		"un-resolvable-pattern": true,
	}
	excluded := map[string]bool{}
	var currentSection string
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "## ") {
			currentSection = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		if !excludeSections[currentSection] {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		// Strip leading "- ", trim optional backticks, take everything before
		// the first separator (em-dash, " - ", " — ", "(", ":").
		entry := strings.TrimPrefix(line, "- ")
		// Trim backticks if the name was code-spanned.
		entry = strings.Trim(entry, "`")
		// Cut at any of the common rationale separators.
		for _, sep := range []string{" — ", " -- ", " - ", " (", ": ", "`"} {
			if i := strings.Index(entry, sep); i >= 0 {
				entry = entry[:i]
			}
		}
		entry = strings.TrimSpace(strings.Trim(entry, "`"))
		if entry != "" {
			excluded[entry] = true
		}
	}
	return excluded, nil
}

func writeJSONOutput(w io.Writer, stats *CodeStats) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(stats)
}

func writeTextOutput(w io.Writer, stats *CodeStats) {
	fmt.Fprintf(w, "Vault:     %s\n", stats.Vault)
	fmt.Fprintf(w, "Index:     %s\n\n", stats.IndexPath)

	langs := make([]string, 0, len(stats.Languages))
	for lang := range stats.Languages {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	for _, lang := range langs {
		lc := stats.Languages[lang]
		ec := stats.Edges[lang]
		fmt.Fprintf(w, "[%s] symbols=%d files=%d modules=%d\n", lang, lc.Symbols, lc.Files, lc.Modules)
		fmt.Fprintf(w, "       calls extracted=%d resolved=%d unresolved=%d (rate=%.2f)\n",
			ec.CallsExtracted, ec.CallsResolved, ec.CallsUnresolved, ec.ResolutionRate)
		fmt.Fprintf(w, "       member_refs=%d imports=%d\n", ec.MemberRefs, ec.Imports)
		if classified, ok := stats.ReferenceClassifications[lang]; ok {
			fmt.Fprintf(w, "       classified associations local=%d external=%d runtime=%d unknown=%d\n",
				classified.Associations.LocalResolved, classified.Associations.ExternalClassified,
				classified.Associations.RuntimeGlobalBuiltin, classified.Associations.Unknown)
			fmt.Fprintf(w, "       classified targets      local=%d external=%d runtime=%d unknown=%d\n",
				classified.UniqueTargets.LocalResolved, classified.UniqueTargets.ExternalClassified,
				classified.UniqueTargets.RuntimeGlobalBuiltin, classified.UniqueTargets.Unknown)
			for _, kind := range []string{"calls", "type_ref", "member_ref", "imports"} {
				kindStats := classified.ByKind[kind]
				fmt.Fprintf(w, "       %-10s associations=%d/%d/%d/%d targets=%d/%d/%d/%d (local/external/runtime/unknown)\n",
					kind,
					kindStats.Associations.LocalResolved, kindStats.Associations.ExternalClassified,
					kindStats.Associations.RuntimeGlobalBuiltin, kindStats.Associations.Unknown,
					kindStats.UniqueTargets.LocalResolved, kindStats.UniqueTargets.ExternalClassified,
					kindStats.UniqueTargets.RuntimeGlobalBuiltin, kindStats.UniqueTargets.Unknown)
			}
		}
		if unresolved := stats.UnresolvedCallees[lang]; len(unresolved) > 0 {
			fmt.Fprintf(w, "       top unresolved callees:\n")
			limit := 5
			if len(unresolved) < limit {
				limit = len(unresolved)
			}
			for _, u := range unresolved[:limit] {
				fmt.Fprintf(w, "         %-30s %d calls\n", u.Name, u.Calls)
			}
		}
		fmt.Fprintln(w)
	}
}
