package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info [file]",
		Short: "Show file information including frontmatter and tags",
		Long: `Show detailed information about a file including its frontmatter and all tags.
Tags can be defined either in frontmatter or as hashtags in the file content.

Example:
  rhizome info "Notes/Project.md"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no vault name is provided, get the default vault name
			if commandOptions.vaultName == "" {
				vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
				defaultName, err := vault.DefaultName()
				if err != nil {
					return err
				}
				commandOptions.vaultName = defaultName
			}

			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			vaultDef, err := vault.Definition()
			if err != nil {
				return err
			}
			note, err := newProjectedNoteReader(cmd.Context(), vaultDef)
			if err != nil {
				return err
			}

			info, err := actions.GetFileInfo(&vault, note, args[0])
			if err != nil {
				return err
			}

			// Print the file information
			fmt.Fprintln(cmd.OutOrStdout(), "File:", args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "\nFrontmatter:")
			if info.Frontmatter != nil {
				for k, v := range info.Frontmatter {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v\n", k, v)
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "  No frontmatter found")
			}

			fmt.Fprintln(cmd.OutOrStdout(), "\nTags:")
			if len(info.Tags) > 0 {
				for _, tag := range info.Tags {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", tag)
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "  No tags found")
			}
			return nil
		},
	}
}

func (commandOptions *noteInvocationOptions) registerInfoCommands(infoCmd, noteCmd *cobra.Command) {
	infoCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(infoCmd)
}
