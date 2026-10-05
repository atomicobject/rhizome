package cmd

import (
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/spf13/cobra"
)

var codeRationaleKinds []string

var codeRationaleCmd = &cobra.Command{
	Use:   "rationale [path]",
	Short: "Show rationale-style comments (NOTE, HACK, TODO, etc.) extracted from code",
	Long: `List rationale comments extracted from indexed code files.

If path is a file, shows rationale for that file.
If path is a directory, shows rationale for all indexed files under it.
If omitted, shows all rationale in the vault.

Use --kind to filter by comment kind (note, hack, todo, fixme, important, why, rationale).`,
	Args: cobra.MaximumNArgs(1),
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

		// Normalize requested kinds to lowercase.
		var kindsFilter []string
		for _, k := range codeRationaleKinds {
			for _, part := range strings.Split(k, ",") {
				part = strings.ToLower(strings.TrimSpace(part))
				if part != "" {
					kindsFilter = append(kindsFilter, part)
				}
			}
		}

		var rationale []codeanchor.Rationale

		if len(args) == 0 {
			// All rationale in the vault.
			rationale, err = store.RationaleByKind(ctx, kindsFilter)
			if err != nil {
				return fmt.Errorf("rationale query: %w", err)
			}
		} else {
			input := args[0]
			vaultPaths, vpErr := paths.NewVaultPaths(vaultPath)
			if vpErr != nil || vaultPaths.Root() == "" {
				return fmt.Errorf("invalid vault path %q", vaultPath)
			}

			_, abs, resolveErr := paths.ResolveCodeInputWithVaultPaths(vaultPaths, input)
			if resolveErr != nil || abs == "" {
				return fmt.Errorf("invalid path %q", input)
			}
			absStr := abs.String()

			// Get vault-relative path for querying.
			relPath := ""
			if rel, relErr := vaultPaths.RelCodeStrict(absStr); relErr == nil {
				relPath = string(paths.NormalizeCode(rel.String()))
			}
			if relPath == "" {
				relPath = absStr
			}

			// Determine file vs directory.
			info, statErr := os.Stat(absStr)
			isDir := statErr == nil && info.IsDir()

			if isDir {
				prefix := relPath
				if !strings.HasSuffix(prefix, "/") {
					prefix += "/"
				}
				rationale, err = store.RationaleForPathPrefix(ctx, prefix, kindsFilter)
				if err != nil {
					return fmt.Errorf("rationale query: %w", err)
				}
			} else {
				rationale, err = store.RationaleForPath(ctx, relPath)
				if err != nil {
					return fmt.Errorf("rationale query: %w", err)
				}
				// Apply kind filter (RationaleForPath returns all kinds).
				if len(kindsFilter) > 0 {
					kindSet := make(map[string]bool, len(kindsFilter))
					for _, k := range kindsFilter {
						kindSet[k] = true
					}
					filtered := rationale[:0]
					for _, r := range rationale {
						if kindSet[string(r.Kind)] {
							filtered = append(filtered, r)
						}
					}
					rationale = filtered
				}
			}
		}

		if len(rationale) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no rationale found)")
			return nil
		}

		printRationale(cmd, rationale)
		return nil
	},
}

func printRationale(cmd *cobra.Command, rationale []codeanchor.Rationale) {
	out := cmd.OutOrStdout()
	for _, r := range rationale {
		symbolInfo := ""
		if r.SymbolFQN != "" {
			symbolInfo = " " + r.SymbolFQN
		}
		fmt.Fprintf(out, "%s:%d [%s]%s\n  %s\n\n",
			r.Path, r.StartLine, strings.ToUpper(string(r.Kind)), symbolInfo, r.Content)
	}
}

func init() {
	codeRationaleCmd.Flags().StringSliceVar(&codeRationaleKinds, "kind", nil, "Filter by kind (note,hack,todo,fixme,important,why,rationale)")
	codeCmd.AddCommand(codeRationaleCmd)
}
