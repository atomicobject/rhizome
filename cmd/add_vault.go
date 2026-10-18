package cmd

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultInvocationOptions) makeAddVaultCmd() *cobra.Command {
	return &cobra.Command{
		Use: "add [name] [path]",
		Aliases: []string{
			"add-vault",
		},
		Short: "Add or override a vault path in rhizome preferences",
		Long: `Add or override a vault path in rhizome preferences.

For traditional Obsidian vaults, provide a name and path:
  rhizome vault add myvault /path/to/vault

For markdown collections (e.g., documentation in a code repo), use --root and --includes:
  rhizome vault add docs /path/to/repo --root /path/to/repo --includes "docs/**/*.md" --includes "src/**/*.md"

Options:
  --root       Base directory for glob patterns (defaults to path)
  --includes   Glob patterns to include (can be specified multiple times)
  --excludes   Glob patterns to exclude (can be specified multiple times)
  --links      Link format: "wikilinks", "markdown", or "both" (default: both)`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			var def obsidian.VaultDefinition
			def.Name = name

			// If includes are specified, this is a collection-style vault
			if len(commandOptions.addVaultIncludes) > 0 {
				if len(args) < 2 && commandOptions.addVaultRoot == "" {
					return fmt.Errorf("either a path argument or --root is required for collection vaults")
				}
				if commandOptions.addVaultRoot != "" {
					def.Root = commandOptions.addVaultRoot
				} else {
					def.Root = args[1]
				}
				def.Includes = commandOptions.addVaultIncludes
				def.Excludes = commandOptions.addVaultExcludes
			} else {
				// Traditional vault
				if len(args) < 2 {
					return fmt.Errorf("path argument is required")
				}
				def.Path = args[1]
			}

			// Set link type if specified
			if commandOptions.addVaultLinks != "" {
				switch strings.ToLower(commandOptions.addVaultLinks) {
				case "wikilinks", "wiki":
					def.Links = obsidian.LinkTypeWikilinks
				case "markdown", "md":
					def.Links = obsidian.LinkTypeMarkdown
				case "both", "all":
					def.Links = obsidian.LinkTypeBoth
				default:
					return fmt.Errorf("invalid --links value: %s (use wikilinks, markdown, or both)", commandOptions.addVaultLinks)
				}
			}

			if err := repoconfig.SaveDefinitionToPreferences(commandEnvFromContext(cmd.Context()).Config, def, commandOptions.addVaultForce); err != nil {
				return err
			}

			if def.IsCollection() {
				fmt.Fprintf(cmd.OutOrStdout(), "Collection vault '%s' saved with root %s\n", name, def.Root)
				fmt.Fprintf(cmd.OutOrStdout(), "  Includes: %v\n", def.Includes)
				if len(def.Excludes) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "  Excludes: %v\n", def.Excludes)
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Vault '%s' saved at %s\n", name, def.Path)
			}
			return nil
		},
	}
}

func (commandOptions *vaultInvocationOptions) registerAddVaultCommands(addVaultCmd, vaultCmd *cobra.Command) {
	addVaultCmd.Flags().BoolVar(&commandOptions.addVaultForce, "force", false, "overwrite existing vault with this name")
	addVaultCmd.Flags().StringVar(&commandOptions.addVaultRoot, "root", "", "base directory for include/exclude globs")
	addVaultCmd.Flags().StringArrayVar(&commandOptions.addVaultIncludes, "includes", nil, "glob patterns to include (repeatable)")
	addVaultCmd.Flags().StringArrayVar(&commandOptions.addVaultExcludes, "excludes", nil, "glob patterns to exclude (repeatable)")
	addVaultCmd.Flags().StringVar(&commandOptions.addVaultLinks, "links", "", "link format: wikilinks, markdown, or both")
	vaultCmd.AddCommand(addVaultCmd)
}
