package cmd

import (
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeNoteFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "find",
		Aliases: []string{"f"},
		Short:   "Fuzzy-find and open a note in the vault",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			note := obsidian.Note{}
			uri := obsidian.Uri{}
			fuzzyFinder := obsidian.FuzzyFinder{}
			err := actions.SearchNotes(&vault, &note, &uri, &fuzzyFinder)
			if err != nil {
				return err
			}
			return nil
		},
	}
}

func (commandOptions *noteInvocationOptions) registerSearchCommands(noteFindCmd, noteCmd *cobra.Command) {
	noteFindCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(noteFindCmd)
}
