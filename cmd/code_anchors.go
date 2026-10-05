package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/anchors"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/spf13/cobra"
)

var codeAnchorsCmd = &cobra.Command{
	Use:   "anchors",
	Short: "Inspect code anchors and how they match",
}

var codeAnchorsExplainCmd = &cobra.Command{
	Use:   "explain [paths...]",
	Short: "Explain code-anchor matches for a note or code file",
	Long: `Explain code-anchor configuration and matching.

The command resolves each path through configured vault ownership. Unowned paths
are rejected rather than inferred from their filename extension.

Examples:
  # Explain anchors defined in a note
  rzm code anchors explain <notes-root>/code-anchors/Python.md

  # Explain which notes/anchors apply to a file
  rzm code anchors explain svc/invoice.py

`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCodeAnchorsExplain(cmd, args)
	},
}

var codeAnchorsListJSON bool
var codeAnchorsListLimit int

var codeAnchorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List anchors in the current code index",
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

		ctx := cmd.Context()
		anchors, err := store.Anchors(ctx)
		if err != nil {
			return err
		}
		limit := codeAnchorsListLimit
		if limit <= 0 {
			limit = 10
		}
		items, err := actions.BuildCodeAnchorListItems(ctx, store, anchors, limit)
		if err != nil {
			return err
		}

		if codeAnchorsListJSON {
			payload := map[string]any{
				"anchors":     items,
				"count":       len(items),
				"generatedAt": time.Now().Format(time.RFC3339),
			}
			enc, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(enc))
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Anchors (%d):\n", len(items))
		for _, it := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "- %s kind=%s", it.Label, it.Kind)
			if it.Lang != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " lang=%s", it.Lang)
			}
			if it.SymbolCount > 0 || it.CallCount > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), " scopes(symbols=%d,calls=%d)", it.SymbolCount, it.CallCount)
			}
			if it.PathPrefix != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " dir=%q", it.PathPrefix)
			}
			if len(it.Globs) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), " globs=%d", len(it.Globs))
			}
			if len(it.NotePaths) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), " notes=%d", len(it.NotePaths))
			}
			if it.MatchedFilesCount > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), " matchedFiles=%d", it.MatchedFilesCount)
			}
			fmt.Fprintln(cmd.OutOrStdout())

			if len(it.Symbols) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  symbols (%d):\n", it.SymbolCount)
				for _, s := range it.Symbols {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", s)
				}
			}
			if len(it.CallFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  call files (%d):\n", it.CallCount)
				for _, f := range it.CallFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", f)
				}
			}
			if len(it.DefinitionFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  definition files (%d):\n", len(it.DefinitionFiles))
				for _, f := range it.DefinitionFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", f)
				}
			}
			if len(it.MatchedFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  matched files (%d):\n", it.MatchedFilesCount)
				for _, f := range it.MatchedFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", f)
				}
			}
			if it.Truncated {
				fmt.Fprintln(cmd.OutOrStdout(), "  (truncated; increase --limit)")
			}
		}
		return nil
	},
}

var (
	codeAnchorsMatchJSON  bool
	codeAnchorsMatchLimit int
	codeAnchorsMatchKind  string
	codeAnchorsMatchValue string
)

var codeAnchorsMatchCmd = &cobra.Command{
	Use:   "match [query]",
	Short: "Find anchors that match a selector",
	Long: `Match anchors by a single selector against the current code index.

Supported kinds:
  - symbol     (lang:pkg.Name) -> function/baseClass anchors
  - decorator  (lang:pkg.Decorator) -> annotation anchors (type-only)
  - path       (file path) -> path-prefix anchors (target path; rare)
  - dir        (file path) -> dir: anchors (implemented as glob matching)
  - glob       (file path) -> glob anchors (target path)

If --kind/--value are omitted, treats [query] as an anchor label (or substring) and prints its instances.
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = strings.TrimSpace(args[0])
		}

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

		ctx := cmd.Context()
		limit := codeAnchorsMatchLimit
		if limit <= 0 {
			limit = 50
		}
		kind := strings.ToLower(strings.TrimSpace(codeAnchorsMatchKind))
		value := strings.TrimSpace(codeAnchorsMatchValue)
		if value == "" && query != "" {
			value = query
		}

		var ids []int64
		if kind == "" && strings.TrimSpace(codeAnchorsMatchKind) == "" && strings.TrimSpace(codeAnchorsMatchValue) == "" {
			if query == "" {
				return errors.New("match requires a query argument or --kind/--value")
			}
			if id, ok := store.AnchorIDByLabel(ctx, query); ok {
				ids = []int64{id}
			} else {
				anchors, err := store.Anchors(ctx)
				if err != nil {
					return err
				}
				needle := strings.ToLower(query)
				seen := map[int64]bool{}
				for _, a := range anchors {
					if strings.Contains(strings.ToLower(a.Label), needle) ||
						(a.BaseSym != nil && strings.ToLower(a.BaseSym.Name) == needle) ||
						(a.Ann != nil && strings.ToLower(a.Ann.Symbol.Name) == needle) {
						if !seen[a.ID] {
							seen[a.ID] = true
							ids = append(ids, a.ID)
						}
					}
				}
			}
		} else {
			if kind == "" || value == "" {
				return errors.New("match requires --kind and --value (or a single query argument)")
			}
			switch kind {
			case "symbol":
				ref, err := parseLangSymbolSpec(value)
				if err != nil {
					return err
				}
				ids, err = store.AnchorsMatchingSymbols(ctx, []string{ref.Name}, []string{ref.Pkg}, ref.Lang)
				if err != nil {
					return err
				}
			case "decorator", "annotation":
				ref, err := parseLangSymbolSpec(value)
				if err != nil {
					return err
				}
				ids, err = store.AnchorsMatchingAnnotations(ctx, []codeanchor.SymbolRef{ref})
				if err != nil {
					return err
				}
			case "calls", "call":
				// Backward compatibility: "calls" used to mean "call-site-only".
				// Function anchors now match both definitions and call sites, so treat this as symbol lookup.
				ref, err := parseLangSymbolSpec(value)
				if err != nil {
					return err
				}
				ids, err = store.AnchorsMatchingSymbols(ctx, []string{ref.Name}, []string{ref.Pkg}, ref.Lang)
				if err != nil {
					return err
				}
			case "path", "dir":
				target := value
				if vaultPaths.Root() != "" {
					rel, _, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, target)
					if err != nil {
						return err
					}
					target = rel.String()
				} else {
					target = string(paths.NormalizeCode(target))
				}
				if kind == "dir" {
					ids, err = store.AnchorsByGlobMatch(ctx, target)
				} else {
					ids, err = store.AnchorsByPathPrefix(ctx, target)
				}
				if err != nil {
					return err
				}
			case "glob":
				target := value
				if vaultPaths.Root() != "" {
					rel, _, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, target)
					if err != nil {
						return err
					}
					target = rel.String()
				} else {
					target = string(paths.NormalizeCode(target))
				}
				ids, err = store.AnchorsByGlobMatch(ctx, target)
				if err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported --kind %q", codeAnchorsMatchKind)
			}
		}

		if len(ids) == 0 {
			if kind != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Matched anchors (0) for %s=%q\n", kind, value)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Matched anchors (0) for %q\n", query)
			}
			return nil
		}

		anchors, err := store.AnchorsByIDs(ctx, ids)
		if err != nil {
			return err
		}
		items, err := actions.BuildCodeAnchorListItems(ctx, store, anchors, limit)
		if err != nil {
			return err
		}

		if codeAnchorsMatchJSON {
			payload := map[string]any{
				"query":       query,
				"kind":        kind,
				"value":       value,
				"anchorIDs":   ids,
				"anchors":     items,
				"count":       len(items),
				"generatedAt": time.Now().Format(time.RFC3339),
			}
			enc, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(enc))
			return nil
		}

		if kind != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Matched anchors (%d) for %s=%q:\n", len(items), kind, value)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Matched anchors (%d) for %q:\n", len(items), query)
		}
		for _, it := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "- %s (%s)\n", it.Label, it.Kind)
			for _, p := range it.NotePaths {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", p)
			}
			if len(it.Symbols) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  symbols (%d):\n", it.SymbolCount)
				for _, s := range it.Symbols {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", s)
				}
			}
			if len(it.CallFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  call files (%d):\n", it.CallCount)
				for _, f := range it.CallFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", f)
				}
			}
			if len(it.DefinitionFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  definition files (%d):\n", len(it.DefinitionFiles))
				for _, f := range it.DefinitionFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", f)
				}
			}
			if len(it.MatchedFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  matched files (%d):\n", it.MatchedFilesCount)
				for _, f := range it.MatchedFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", f)
				}
			}
			if it.Truncated {
				fmt.Fprintln(cmd.OutOrStdout(), "  (truncated; increase --limit)")
			}
		}
		return nil
	},
}

func parseLangSymbolSpec(spec string) (codeanchor.SymbolRef, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return codeanchor.SymbolRef{}, errors.New("empty symbol spec")
	}
	lang := codeanchor.Lang("")
	rest := spec
	if idx := strings.Index(spec, ":"); idx > 0 {
		lang = codeanchor.Lang(strings.TrimSpace(spec[:idx]))
		// Accept user-friendly language prefixes (python, golang, javascript, ...).
		lang = codeanchor.Lang(codeanchorLangNormalize(string(lang)))
		rest = spec[idx+1:]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return codeanchor.SymbolRef{}, fmt.Errorf("invalid symbol spec %q", spec)
	}
	pkg := ""
	name := rest
	if dot := strings.LastIndex(rest, "."); dot >= 0 {
		pkg = rest[:dot]
		name = rest[dot+1:]
	}
	if name == "" {
		return codeanchor.SymbolRef{}, fmt.Errorf("invalid symbol spec %q", spec)
	}
	return codeanchor.SymbolRef{Lang: lang, Pkg: pkg, Name: name}, nil
}

func codeanchorLangNormalize(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "python":
		return "py"
	case "golang":
		return "go"
	case "typescript", "javascript":
		return "ts"
	default:
		return strings.TrimSpace(s)
	}
}

var codeAnchorsValidateJSON bool

var codeAnchorsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Check that all code anchors match indexed symbols",
	Long: `Validates code-anchor definitions against the current index.

For each symbol-based anchor (ref:, baseClass:), checks:
1. Exact FQN match exists in the index
2. If not, searches for suffix matches and suggests corrections

Use this to diagnose anchors that aren't matching due to FQN mismatches
(e.g., import-style paths vs file-path-based indexed FQNs).

Exits with code 1 if any anchors fail validation.`,
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

		svc, _ := newCodeAnchorService(codeAnchorServiceConfig{
			VaultPath: vaultPath,
			CodeCfg:   codeCfg,
			Store:     store,
		})

		ctx := cmd.Context()
		results, err := svc.ValidateAnchors(ctx)
		if err != nil {
			return err
		}

		// Separate valid vs issues
		var valid, issues []codeanchor.ValidationResult
		for _, r := range results {
			if r.Status == codeanchor.ValidationValid {
				valid = append(valid, r)
			} else {
				issues = append(issues, r)
			}
		}

		if codeAnchorsValidateJSON {
			type jsonResult struct {
				Label         string   `json:"label"`
				Lang          string   `json:"lang"`
				Status        string   `json:"status"`
				MatchedCount  int      `json:"matchedCount,omitempty"`
				SuffixMatches []string `json:"suffixMatches,omitempty"`
				Message       string   `json:"message,omitempty"`
				Ref           string   `json:"ref,omitempty"`
			}
			jsonResults := make([]jsonResult, 0, len(results))
			for _, r := range results {
				jr := jsonResult{
					Label:         r.Anchor.Label,
					Lang:          string(r.Anchor.Lang),
					Status:        string(r.Status),
					MatchedCount:  r.MatchedCount,
					SuffixMatches: r.SuffixMatches,
					Message:       r.Message,
				}
				if r.Anchor.BaseSym != nil {
					if r.Anchor.BaseSym.Pkg != "" {
						jr.Ref = r.Anchor.BaseSym.Pkg + "." + r.Anchor.BaseSym.Name
					} else {
						jr.Ref = r.Anchor.BaseSym.Name
					}
				}
				jsonResults = append(jsonResults, jr)
			}
			payload := map[string]any{
				"valid":       len(valid),
				"issues":      len(issues),
				"total":       len(results),
				"results":     jsonResults,
				"generatedAt": time.Now().Format(time.RFC3339),
			}
			enc, _ := json.MarshalIndent(payload, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(enc))
			if len(issues) > 0 {
				return fmt.Errorf("%d anchors have issues", len(issues))
			}
			return nil
		}

		// Human-readable output
		fmt.Fprintf(cmd.OutOrStdout(), "Validating %d anchors...\n\n", len(results))

		for _, r := range results {
			if r.Status == codeanchor.ValidationValid {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ %s (%s) - matched %d symbols\n",
					r.Anchor.Label, r.Anchor.Lang, r.MatchedCount)
				continue
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✗ %s (%s)", r.Anchor.Label, r.Anchor.Lang)
			if r.Anchor.BaseSym != nil {
				fqn := r.Anchor.BaseSym.Pkg + "." + r.Anchor.BaseSym.Name
				if r.Anchor.BaseSym.Pkg == "" {
					fqn = r.Anchor.BaseSym.Name
				}
				fmt.Fprintf(cmd.OutOrStdout(), " - ref: %s", fqn)
			}
			fmt.Fprintln(cmd.OutOrStdout())

			if len(r.SuffixMatches) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  No exact match. Did you mean:")
				for _, fqn := range r.SuffixMatches {
					fmt.Fprintf(cmd.OutOrStdout(), "    - %s (suffix match)\n", fqn)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\n  Suggested fix:\n    ref: %s\n", r.SuffixMatches[0])
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", r.Message)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Summary: %d/%d anchors valid", len(valid), len(results))
		if len(issues) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), ", %d issues found\n", len(issues))
			return fmt.Errorf("%d anchors have issues", len(issues))
		}
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
	},
}
