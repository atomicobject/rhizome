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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/spf13/cobra"
)

// CodeStats is the JSON output contract for `rzm code stats`.
type CodeStats struct {
	Vault     string `json:"vault"`
	IndexPath string `json:"indexPath"`
	semdb.CodeStats
}

func (commandOptions *codeInvocationOptions) makeCodeStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show structured per-language indexer coverage statistics",
		Long: `Outputs structured statistics from the code index: per-language symbol/file/module counts, edge resolution rates, top-N unresolved callees, and directory file distribution.

JSON is the default output (machine-readable, agent-consumable). Use --text for a compact human-readable summary.

The --exclude-from <catalog> flag accepts a markdown coverage catalog (see SPEC-0070) and filters names listed under language-builtin, framework-noise, or un-resolvable-pattern sections from the unresolvedCallees output. Used by audit skills to surface real-gap signal without language built-in noise. This filter is purely diagnostic; underlying index data is unchanged.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultPath, codeCfg, err := commandOptions.loadVaultAndCodeConfig(cmd)
			if err != nil {
				return err
			}
			if !codeCfg.Enabled {
				return fmt.Errorf("code index not enabled in this vault's config")
			}
			if _, err := os.Stat(codeCfg.IndexPath); err != nil {
				return fmt.Errorf("code index file not found at %s; run `rzm index` first", codeCfg.IndexPath)
			}

			excludeSet, err := loadExcludeCatalog(commandOptions.codeStatsExcludeFrom)
			if err != nil {
				return err
			}
			result, err := semdb.ReadCodeStats(cmd.Context(), codeCfg.IndexPath, semdb.CodeStatsOptions{
				UnresolvedTop: commandOptions.codeStatsUnresolvedTop,
				ExcludeNames:  excludeSet,
			})
			if err != nil {
				return err
			}
			stats := CodeStats{Vault: vaultPath, IndexPath: codeCfg.IndexPath, CodeStats: result}

			if commandOptions.codeStatsText {
				writeTextOutput(os.Stdout, &stats)
				return nil
			}
			return writeJSONOutput(os.Stdout, &stats)
		},
	}
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
