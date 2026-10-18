package cmd

import (
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeOpenVaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "open",
		Aliases: []string{"o"},
		Short:   "Opens note in vault by note name",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			uri := obsidian.Uri{}
			noteName := args[0]
			params := actions.OpenParams{NoteName: noteName}
			err := actions.OpenNote(&vault, &uri, params)
			if err != nil {
				return err
			}
			return nil
		},
	}
}

func (commandOptions *noteInvocationOptions) registerOpenCommands(OpenVaultCmd, noteCmd *cobra.Command) {
	OpenVaultCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (not required if default is set)")
	noteCmd.AddCommand(OpenVaultCmd)
}
