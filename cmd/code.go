package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	codePythonRoots []string
	codeIndexPath   string
)

var codeCmd = &cobra.Command{
	Use:   "code",
	Short: "Code-to-note linking via Tree-sitter indexing",
}

var codeIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Build or update the code intel index (anchors, symbols, calls) without embeddings",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
		if err != nil {
			return err
		}
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		return indexing.RunCodeIndexCommand(cmd.Context(), indexing.CodeIndexCommandOptions{
			VaultPath:  vaultPath,
			VaultDef:   vaultDef,
			CodeConfig: codeCfg,
			ErrWriter:  cmd.ErrOrStderr(),
		})
	},
}

var codeIndexAnchorsCmd = &cobra.Command{
	Use:   "index-anchors",
	Short: "Refresh code anchor definitions from notes (no code scan, no embeddings)",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
		if err != nil {
			return err
		}
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		return indexing.RunCodeAnchorsCommand(cmd.Context(), indexing.CodeAnchorsCommandOptions{
			VaultPath:  vaultPath,
			VaultDef:   vaultDef,
			CodeConfig: codeCfg,
			ErrWriter:  cmd.ErrOrStderr(),
		})
	},
}

var codeContextCmd = &cobra.Command{
	Use:   "context [files...]",
	Short: "Get notes linked to the specified code files",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}

		store, cleanup, err := requireIntelStore(vaultPath, codeCfg)
		if err != nil {
			return err
		}
		defer cleanup()

		tailIdx := codeanchor.NewPathTailIndex(5)
		if paths, err := store.IndexedFilePaths(cmd.Context()); err == nil {
			for _, p := range paths {
				tailIdx.Add(p)
			}
		}

		service, _ := newCodeAnchorService(codeAnchorServiceConfig{
			VaultPath:       vaultPath,
			CodeCfg:         codeCfg,
			Store:           store,
			TailIndex:       tailIdx,
			IncludeIndexers: true,
		})
		vaultPaths, err := paths.NewVaultPaths(vaultPath)
		if err != nil || vaultPaths.Root() == "" {
			return fmt.Errorf("invalid vault path %q", vaultPath)
		}
		ctx := cmd.Context()
		if !service.HasAnyIndexer() {
			fmt.Fprintln(os.Stderr, "Warning: no code indexers available (build may lack cgo/tree-sitter); new code files will be skipped.")
		}

		var results []codeanchor.FileContext
		for _, f := range args {
			_, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, f)
			if err != nil || abs == "" {
				return fmt.Errorf("invalid code path %q", f)
			}
			absPath := abs.String()

			// Index the file if not already indexed. The interactive context command
			// needs same-run symbols/calls before resolving anchors, unlike explain
			// which is intentionally read-only below.
			lang := detectCodeLang(absPath)
			if lang != "" && service.HasIndexer(lang) {
				content, err := os.ReadFile(absPath)
				if err != nil {
					return err
				}
				if err := service.IndexCodeFile(ctx, lang, absPath, content); err != nil {
					if errors.Is(err, codeanchor.ErrUnsupportedLanguage) {
						return fmt.Errorf("code indexer for %s files is unavailable in this build (missing tree-sitter/cgo)", lang)
					}
					return err
				}
				// Ensure anchor scopes reflect freshly indexed symbols/calls for this request.
				if err := service.RecomputeAnchorScopes(ctx); err != nil {
					return err
				}
			} else if lang != "" && !service.HasIndexer(lang) {
				fmt.Fprintf(os.Stderr, "Warning: skipping indexing for %s (%s indexer unavailable); serving existing index data if present.\n", absPath, lang)
			}

			fc, err := service.NotesForFile(ctx, absPath)
			if err != nil {
				return err
			}
			results = append(results, fc)
		}

		payload := map[string]any{
			"contexts":    results,
			"count":       len(results),
			"generatedAt": time.Now().Format(time.RFC3339),
		}
		enc, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(enc))
		return nil
	},
}

var codeValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate code anchor frontmatter in notes without indexing",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, _, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}

		ctx := cmd.Context()
		total, invalid, err := validateNotes(ctx, vaultPath)
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Validated %d notes (%d invalid)\n", total, invalid)
		if invalid > 0 {
			return fmt.Errorf("code validate: %d note(s) have invalid anchors", invalid)
		}
		return nil
	},
}

var codeExplainJSON bool

type codeExplainResult struct {
	Input       string                  `json:"input"`
	Kind        string                  `json:"kind"` // "note" | "file"
	Note        *codeanchor.Note        `json:"note,omitempty"`
	FileContext *codeanchor.FileContext `json:"fileContext,omitempty"`
	Error       string                  `json:"error,omitempty"`
}

func runCodeAnchorsExplain(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	results := make([]codeExplainResult, 0, len(args))
	var noteInputs []actions.CodeExplainInput
	var filePaths []string
	ownershipVault, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return fmt.Errorf("resolve configured ownership: %w", err)
	}
	indexer, err := newNoteMetadataIndexer()
	if err != nil {
		return err
	}
	runtime, err := indexer.FormatRuntime()
	if err != nil {
		return err
	}
	inputs, err := actions.ClassifyCodeExplainInputs(ownershipVault, runtime, args)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if input.Kind == actions.CodeExplainNote {
			noteInputs = append(noteInputs, input)
			continue
		}
		filePaths = append(filePaths, input.Path)
	}

	// Ownership is resolved from the configured vault before this read boundary.
	vaultPath := ownershipVault.BasePath()
	var vaultPaths paths.VaultPaths
	if vaultPath != "" {
		vaultPaths, _ = paths.NewVaultPaths(vaultPath)
	}
	for _, input := range noteInputs {
		p := input.Path
		rel, abs, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, p)
		inputLabel := p
		if err != nil || rel == "" || abs == "" {
			if filepath.IsAbs(p) || vaultPaths.Root() == "" {
				abs = paths.AbsFromInputWithVaultPaths(vaultPaths, vaultPath, p)
			}
		}
		if abs == "" {
			results = append(results, codeExplainResult{Input: inputLabel, Kind: "note", Error: "invalid note path"})
			continue
		}
		inputLabel = abs.String()
		content, err := os.ReadFile(abs.String())
		if err != nil {
			results = append(results, codeExplainResult{Input: inputLabel, Kind: "note", Error: err.Error()})
			continue
		}
		note, markdown, err := markdownCodeAnchorParseCompat(abs.String(), string(content), input.Descriptor.ID)
		if !markdown {
			results = append(results, codeExplainResult{
				Input: inputLabel,
				Kind:  "note",
				Error: fmt.Sprintf("code anchor explanation is not supported for note format %q", input.Descriptor.ID),
			})
			continue
		}
		if err != nil {
			results = append(results, codeExplainResult{Input: inputLabel, Kind: "note", Error: err.Error()})
			continue
		}
		results = append(results, codeExplainResult{Input: inputLabel, Kind: "note", Note: &note})
	}

	// Explaining file matches requires the code index (notes + anchors).
	if len(filePaths) > 0 {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		store, cleanup, err := requireIntelStore(vaultPath, codeCfg)
		if err != nil {
			return err
		}
		defer cleanup()
		vaultPaths, _ := paths.NewVaultPaths(vaultPath)
		if vaultPaths.Root() == "" {
			return fmt.Errorf("invalid vault path %q", vaultPath)
		}

		tailIdx := codeanchor.NewPathTailIndex(5)
		service, _ := newCodeAnchorService(codeAnchorServiceConfig{
			VaultPath: vaultPath,
			CodeCfg:   codeCfg,
			Store:     store,
			TailIndex: tailIdx,
		})

		// Pre-populate the tail index from whatever is already indexed.
		// This avoids mutating a previously-correct index into a less-resolved one
		// when indexing TS/JS files with alias-style imports (e.g. "@/src/...").
		if idx := service.TailIndex(); idx != nil {
			vaultPaths, _ := paths.NewVaultPaths(vaultPath)
			if indexed, err := store.IndexedFilePaths(ctx); err == nil {
				for _, p := range indexed {
					if vaultPaths.Root() == "" {
						idx.Add(p)
						continue
					}
					abs, err := vaultPaths.AbsCode(paths.NormalizeCode(p))
					if err != nil || abs == "" {
						continue
					}
					idx.Add(abs.String())
				}
			}
		}

		// Resolve file paths without re-indexing; explain is a read-only query command.
		// This makes it safe to diagnose stale/missing matches without mutating the
		// index and hiding the state the user is trying to inspect.
		resolvedFiles := make([]string, 0, len(filePaths))
		for _, p := range filePaths {
			_, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, p)
			inputLabel := p
			if abs != "" {
				inputLabel = abs.String()
			}
			if err != nil || abs == "" {
				results = append(results, codeExplainResult{Input: inputLabel, Kind: "file", Error: "invalid code path"})
				continue
			}
			resolvedFiles = append(resolvedFiles, abs.String())
		}

		for _, absPath := range resolvedFiles {
			fc, err := service.NotesForFile(ctx, absPath)
			if err != nil {
				results = append(results, codeExplainResult{Input: absPath, Kind: "file", Error: err.Error()})
				continue
			}
			results = append(results, codeExplainResult{Input: absPath, Kind: "file", FileContext: &fc})
		}
	}

	if codeExplainJSON {
		payload := map[string]any{
			"results":     results,
			"count":       len(results),
			"generatedAt": time.Now().Format(time.RFC3339),
		}
		enc, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(enc))
		return nil
	}

	for _, res := range results {
		switch res.Kind {
		case "note":
			fmt.Fprintf(cmd.OutOrStdout(), "Note: %s\n", res.Input)
			if res.Error != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Error: %s\n\n", res.Error)
				continue
			}
			note := res.Note
			fmt.Fprintf(cmd.OutOrStdout(), "  Title: %s\n", note.Title)
			if len(note.DefinedAnchors) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  Anchors: (none)")
				fmt.Fprintln(cmd.OutOrStdout())
				continue
			}
			if len(note.DefinedAnchors) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  Defined anchors:")
				for _, a := range note.DefinedAnchors {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s (%s)%s\n", a.Label, a.Kind, formatAnchorDetail(a))
				}
			}
			fmt.Fprintln(cmd.OutOrStdout())
		case "file":
			fmt.Fprintf(cmd.OutOrStdout(), "File: %s\n", res.Input)
			if res.Error != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Error: %s\n\n", res.Error)
				continue
			}
			fc := res.FileContext
			if len(fc.Anchors) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  Anchors: (none)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "  Anchors: %s\n", strings.Join(fc.Anchors, ", "))
			}
			if len(fc.Notes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  Notes: (none)")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "  Notes:")
				sort.Slice(fc.Notes, func(i, j int) bool { return fc.Notes[i].Path < fc.Notes[j].Path })
				for _, n := range fc.Notes {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s (%s)\n", n.Path, n.Title)
				}
			}
			if len(fc.Trace) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  Trace:")
				for _, tr := range fc.Trace {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s: %s (%s)\n", tr.AnchorLabel, tr.Reason, tr.Target)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	return nil
}

// markdownCodeAnchorParseCompat is the explicit Markdown-only adapter for
// legacy code-anchor front matter. Projectable non-Markdown formats must
// provide indexed anchor facts; they must not enter codeanchor.ParseNote.
func markdownCodeAnchorParseCompat(path, content string, format noteformat.FormatID) (codeanchor.Note, bool, error) {
	if format != noteformat.FormatID("markdown") {
		return codeanchor.Note{}, false, nil
	}
	note, err := codeanchor.ParseNote(path, content)
	return note, true, err
}

func formatAnchorDetail(a codeanchor.Anchor) string {
	switch a.Kind {
	case codeanchor.AnchorPath:
		if a.PathPrefix != "" {
			return fmt.Sprintf(" dir=%q", a.PathPrefix)
		}
	case codeanchor.AnchorGlob:
		if len(a.Globs) == 1 {
			return fmt.Sprintf(" glob=%q", a.Globs[0])
		}
		if len(a.Globs) > 1 {
			return fmt.Sprintf(" globs=%q", strings.Join(a.Globs, ", "))
		}
	case codeanchor.AnchorAnnotation:
		if a.Ann != nil {
			return fmt.Sprintf(" decorator=%s%s", formatSymbolRef(a.Ann.Symbol), formatArgFilters(a.Ann.ArgFilters))
		}
	default:
		if a.BaseSym != nil {
			return fmt.Sprintf(" symbol=%s", formatSymbolRef(*a.BaseSym))
		}
	}
	return ""
}

func formatSymbolRef(r codeanchor.SymbolRef) string {
	out := ""
	if r.Lang != "" {
		out += string(r.Lang) + ":"
	}
	if r.Pkg != "" {
		out += r.Pkg + "."
	}
	out += r.Name
	return out
}

func formatArgFilters(filters map[string]string) string {
	if len(filters) == 0 {
		return ""
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, filters[k]))
	}
	return " args={" + strings.Join(parts, ", ") + "}"
}

var codeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show code index status",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}

		fmt.Printf("Vault: %s\n", vaultPath)
		fmt.Printf("Enabled: %v\n", codeCfg.Enabled)
		fmt.Printf("Index: %s\n", codeCfg.IndexPath)
		fmt.Printf("Python roots: %v\n", codeCfg.PythonRoots)
		fmt.Printf("Go roots: %v\n", codeCfg.GoRoots)
		fmt.Printf("C# roots: %v\n", codeCfg.CSharpRoots)
		fmt.Printf("PHP roots: %v\n", codeCfg.PHPRoots)

		if !codeCfg.Enabled {
			return nil
		}

		info, err := os.Stat(codeCfg.IndexPath)
		if err != nil {
			fmt.Printf("Index file: not found\n")
			return nil
		}
		fmt.Printf("Index size: %d bytes\n", info.Size())
		fmt.Printf("Index modified: %s\n", info.ModTime().Format(time.RFC3339))

		// Show indexer version status
		fmt.Printf("Current indexer: %s\n", codeanchor.IndexerVersion)
		if store, cleanup, err := obsidian.OpenIntelStoreFromConfig(vaultPath, codeCfg, true); err == nil && store != nil {
			defer cleanup()
			storedVersion, hasVersion, _ := store.IndexerVersion(cmd.Context())
			if hasVersion {
				fmt.Printf("Index built with: %s\n", storedVersion)
				if storedVersion != codeanchor.IndexerVersion {
					fmt.Printf("  (outdated - consider running 'code index')\n")
				}
			} else {
				fmt.Printf("Index built with: unknown (pre-versioning)\n")
			}
		}

		return nil
	},
}

var codeEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable code indexing for this vault",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		codeCfg.Enabled = true
		if err := obsidian.SaveCodeConfig(vaultPath, codeCfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Code indexing enabled. Run `rzm index` to build the index.")
		return nil
	},
}

var codeDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable code indexing for this vault",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}
		codeCfg.Enabled = false
		if err := obsidian.SaveCodeConfig(vaultPath, codeCfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Code indexing disabled.")
		return nil
	},
}

var codeSymbolsCmd = &cobra.Command{
	Use:   "symbols [files...]",
	Short: "List indexed symbols (FQNs) for code files",
	Long: `List the fully-qualified names (FQNs) of symbols indexed from code files.
Useful for discovering what symbols exist when defining anchors.

If the file is not yet indexed, it will be indexed first (if the language is supported).`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath, codeCfg, err := loadVaultAndCodeConfig(cmd)
		if err != nil {
			return err
		}

		store, cleanup, err := requireIntelStore(vaultPath, codeCfg)
		if err != nil {
			return err
		}
		defer cleanup()

		tailIdx := codeanchor.NewPathTailIndex(5)
		service, _ := newCodeAnchorService(codeAnchorServiceConfig{
			VaultPath:       vaultPath,
			CodeCfg:         codeCfg,
			Store:           store,
			TailIndex:       tailIdx,
			IncludeIndexers: true,
		})
		ctx := cmd.Context()
		vaultPaths, err := paths.NewVaultPaths(vaultPath)
		if err != nil || vaultPaths.Root() == "" {
			return fmt.Errorf("invalid vault path %q", vaultPath)
		}

		type fileSymbols struct {
			File    string   `json:"file"`
			Symbols []string `json:"symbols"`
		}

		var results []fileSymbols
		for _, f := range args {
			_, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, f)
			if err != nil || abs == "" {
				return fmt.Errorf("invalid code path %q", f)
			}
			absPath := abs.String()

			// Index the file if not already indexed so the command can be used to
			// discover the exact indexed FQNs needed by code-anchor frontmatter.
			lang := detectCodeLang(absPath)
			if lang != "" && service.HasIndexer(lang) {
				content, err := os.ReadFile(absPath)
				if err != nil {
					return fmt.Errorf("read %s: %w", f, err)
				}
				if err := service.IndexCodeFile(ctx, lang, absPath, content); err != nil {
					if !errors.Is(err, codeanchor.ErrUnsupportedLanguage) {
						return fmt.Errorf("index %s: %w", f, err)
					}
				}
			}

			symbols, err := service.SymbolsForFile(ctx, absPath)
			if err != nil {
				return fmt.Errorf("symbols for %s: %w", f, err)
			}
			sort.Strings(symbols)
			results = append(results, fileSymbols{File: absPath, Symbols: symbols})
		}

		enc, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(enc))
		return nil
	},
}

func loadVaultAndCodeConfig(cmd *cobra.Command) (string, codeanchor.Config, error) {
	vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
	if err != nil {
		return "", codeanchor.Config{}, err
	}
	vaultPath := vaultDef.BasePath()

	codeCfg, err := obsidian.LoadCodeConfig(vaultPath)
	if err != nil {
		return "", codeanchor.Config{}, err
	}
	applyCodeOverrides(cmd, &codeCfg)
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
	return vaultPath, codeCfg, nil
}

func applyCodeOverrides(cmd *cobra.Command, cfg *codeanchor.Config) {
	if cmd.Flags().Changed("index-path") {
		cfg.IndexPath = codeIndexPath
	}
	if cmd.Flags().Changed("code-root") {
		cfg.PythonRoots = codePythonRoots
	}
}

func resolveCodeRoot(vaultPath, root string) (string, error) {
	return codeintel.ResolveRoot(vaultPath, root)
}

func validateNotes(ctx context.Context, root string) (int, int, error) {
	return codeintel.ValidateNotes(ctx, root)
}

func detectCodeLang(path string) codeanchor.Lang {
	return codeintel.DetectCodeLang(path)
}

func init() {
	codeCmd.PersistentFlags().StringVar(&codeIndexPath, "index-path", "", "Path to code index database")
	codeCmd.PersistentFlags().StringSliceVar(&codePythonRoots, "code-root", nil, "Code roots to index (Python)")
	codeCmd.PersistentFlags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")

	codeCmd.AddCommand(codeIndexCmd)
	codeCmd.AddCommand(codeIndexAnchorsCmd)
	codeCmd.AddCommand(codeContextCmd)
	codeCmd.AddCommand(codeValidateCmd)
	codeAnchorsCmd.AddCommand(codeAnchorsListCmd)
	codeAnchorsListCmd.Flags().BoolVar(&codeAnchorsListJSON, "json", false, "Output results as JSON")
	codeAnchorsListCmd.Flags().IntVar(&codeAnchorsListLimit, "limit", 10, "Max instances shown per anchor")
	codeAnchorsExplainCmd.Flags().BoolVar(&codeExplainJSON, "json", false, "Output results as JSON")
	codeAnchorsCmd.AddCommand(codeAnchorsExplainCmd)
	codeAnchorsCmd.AddCommand(codeAnchorsMatchCmd)
	codeAnchorsMatchCmd.Flags().StringVar(&codeAnchorsMatchKind, "kind", "", "Selector kind (symbol, decorator, calls, path, glob)")
	codeAnchorsMatchCmd.Flags().StringVar(&codeAnchorsMatchValue, "value", "", "Selector value (kind-specific)")
	codeAnchorsMatchCmd.Flags().BoolVar(&codeAnchorsMatchJSON, "json", false, "Output results as JSON")
	codeAnchorsMatchCmd.Flags().IntVar(&codeAnchorsMatchLimit, "limit", 50, "Max instances shown per anchor")
	codeAnchorsCmd.AddCommand(codeAnchorsValidateCmd)
	codeAnchorsValidateCmd.Flags().BoolVar(&codeAnchorsValidateJSON, "json", false, "Output results as JSON")
	codeCmd.AddCommand(codeAnchorsCmd)
	codeCmd.AddCommand(codeStatusCmd)
	codeStatsCmd.Flags().BoolVar(&codeStatsText, "text", false, "Output a compact human-readable summary instead of JSON")
	codeStatsCmd.Flags().IntVar(&codeStatsUnresolvedTop, "unresolved-top", 20, "Maximum unresolved-callee entries per language")
	codeStatsCmd.Flags().StringVar(&codeStatsExcludeFrom, "exclude-from", "", "Path to a coverage catalog markdown file; names in language-builtin/framework-noise/un-resolvable-pattern sections are filtered from unresolvedCallees")
	codeCmd.AddCommand(codeStatsCmd)
	codeCmd.AddCommand(codeEnableCmd)
	codeCmd.AddCommand(codeDisableCmd)
	codeCmd.AddCommand(codeSymbolsCmd)
	codeDocCoverageCmd.Flags().IntVar(&codeDocCoverageLimit, "limit", 25, "Max rows to return")
	codeDocCoverageCmd.Flags().StringSliceVarP(&codeDocCoverageQueries, "query", "q", nil, "Semantic queries to focus coverage on (attach search results)")
	codeDocCoverageCmd.Flags().IntVar(&codeDocCoverageSearchLimit, "search-limit", 50, "Max search results to seed coverage when --query is set")
	codeDocCoverageCmd.Flags().StringSliceVar(&codeDocCoverageLangs, "lang", nil, "Restrict results to the named language(s); repeatable (e.g. --lang php --lang ts)")
	codeDocCoverageCmd.Flags().Float64Var(&codeDocCoverageFallbackThreshold, "fallback-threshold", 0, "Override sparse-graph fallback threshold (default 0.60); 0 keeps default")
	codeCmd.AddCommand(codeDocCoverageCmd)
	codeHotspotsCmd.Flags().IntVar(&codeHotspotsLimit, "limit", 25, "Max rows per section to return")
	codeHotspotsCmd.Flags().StringSliceVar(&codeHotspotsLangs, "lang", nil, "Restrict results to the named language(s); repeatable")
	codeHotspotsCmd.Flags().Float64Var(&codeHotspotsFallbackThreshold, "fallback-threshold", 0, "Override sparse-graph fallback threshold (default 0.60); 0 keeps default")
	codeCmd.AddCommand(codeHotspotsCmd)
	codeComplexityCmd.Flags().IntVar(&codeComplexityLimit, "limit", 25, "Max rows to return")
	codeComplexityCmd.Flags().IntVar(&codeComplexityMinLines, "min-lines", 0, "Minimum span (lines) to include")
	codeCmd.AddCommand(codeComplexityCmd)
	rootCmd.AddCommand(codeCmd)
}
