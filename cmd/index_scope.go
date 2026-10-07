package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	indexScopeJSON    bool
	indexScopeSkip    []string
	indexScopeRemove  []string
	indexScopeInclude []string
)

var errScopeNotConfigured = errors.New("this folder is not set up for Rhizome; run rzm init first")

var indexScopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "Show and change what gets indexed",
	Long: `Show the rules that decide which paths Rhizome indexes, layer by layer in
evaluation order: the built-in list while .rhizome/ignore has no rules, the
.gitignore files Rhizome reads, .rhizome/ignore, and notes excludes in
.rhizome/config.yml. Hidden files and folders are never indexed, and the last
matching rule wins.

--skip, --remove-rule, and --include-ignored change .rhizome/ignore together;
when one fails, nothing changes. Removing a suggested skip also marks the path
to stay indexed, so rzm init does not suggest it again. A running Rhizome
picks up the change.

Examples:
  rzm index scope
  rzm index scope --skip web/src/api/generated
  rzm index scope --remove-rule /testdata/ --json`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		edits := initactions.ScopeEdits{Skip: indexScopeSkip, RemoveRules: indexScopeRemove, IncludeIgnored: indexScopeInclude}
		def, err := indexScopeVault(cmd)
		var scope initactions.Scope
		if err == nil {
			scope, err = initactions.ChangeScope(def.BasePath(), def.Excludes, edits)
		}
		out := cmd.OutOrStdout()
		if !indexScopeJSON {
			if err != nil {
				return err
			}
			writeScope(out, def.BasePath(), scope)
			return nil
		}
		if err != nil {
			failure := initJSONFailure{Schema: initactions.SetupSchema}
			failure.Error.Code, failure.Error.Message = "scope_failed", err.Error()
			if errors.Is(err, errScopeNotConfigured) {
				failure.Error.Code = "not_configured"
			}
			if encodeErr := json.NewEncoder(out).Encode(failure); encodeErr != nil {
				return encodeErr
			}
			return silentExitError{code: 1}
		}
		return json.NewEncoder(out).Encode(struct {
			Schema int    `json:"schema"`
			Root   string `json:"root"`
			initactions.Scope
		}{initactions.SetupSchema, def.BasePath(), scope})
	},
}

// indexScopeVault is the configured folder containing the working
// directory, or the vault --vault names. Unlike other commands, it never
// falls back to the default vault, so an unconfigured folder is an error.
func indexScopeVault(cmd *cobra.Command) (obsidian.VaultDefinition, error) {
	if vaultName != "" {
		return vaultDefOrDefaultContext(cmd.Context())
	}
	cwd, err := commandEnvFromContext(cmd.Context()).Getwd()
	if err != nil {
		return obsidian.VaultDefinition{}, err
	}
	def, err := localVaultDefFromCWD(cwd)
	if errors.Is(err, obsidian.ErrNoLocalConfig) {
		return def, errScopeNotConfigured
	}
	return def, err
}

func writeScope(out io.Writer, root string, scope initactions.Scope) {
	fmt.Fprintf(out, "Rhizome indexes %s except hidden files and folders and what these rules skip. The last matching rule wins.\n", root)
	headings := map[string]string{
		ignore.LayerDefault:   "Built-in list (applies until .rhizome/ignore has rules)",
		ignore.LayerGitignore: "Inherited from .gitignore",
		ignore.LayerRhizome:   "Rhizome rules",
		ignore.LayerConfig:    "Notes excludes in .rhizome/config.yml",
	}
	layer := ""
	for _, rule := range scope.Rules {
		if rule.Layer != layer {
			layer = rule.Layer
			fmt.Fprintf(out, "\n%s\n", headings[layer])
		}
		var notes []string
		if rule.Line > 0 {
			notes = append(notes, fmt.Sprintf("%s:%d", rule.Source, rule.Line))
		}
		if rule.Reason != "" {
			notes = append(notes, rule.Reason)
		}
		if rule.Included {
			notes = append(notes, "included")
		}
		fmt.Fprintf(out, "  %-32s %s\n", rule.Pattern, strings.Join(notes, " · "))
	}
	if len(scope.KeepIndexed) > 0 {
		fmt.Fprintf(out, "\nKept indexed: %s\n", strings.Join(scope.KeepIndexed, ", "))
	}
}

func init() {
	indexScopeCmd.Flags().BoolVar(&indexScopeJSON, "json", false, "write one JSON document")
	indexScopeCmd.Flags().StringArrayVar(&indexScopeSkip, "skip", nil, "leave a folder or file out of the index (repeatable)")
	indexScopeCmd.Flags().StringArrayVar(&indexScopeRemove, "remove-rule", nil, "remove a rule from .rhizome/ignore by its exact text (repeatable)")
	indexScopeCmd.Flags().StringArrayVar(&indexScopeInclude, "include-ignored", nil, "index a folder .gitignore excludes (repeatable)")
	indexCmd.AddCommand(indexScopeCmd)
}
