package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultInvocationOptions) makeRemoveVaultCmd() *cobra.Command {
	return &cobra.Command{
		Use: "remove [name]",
		Aliases: []string{
			"remove-vault",
		},
		Short: "Remove a vault path from rhizome preferences",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: name}
			if err := vault.RemoveFromPreferences(); err != nil {
				return err
			}

			fmt.Printf("Vault '%s' removed from preferences\n", name)
			return nil
		},
	}
}

func (commandOptions *vaultInvocationOptions) registerRemoveVaultCommands(vaultCmd, removeVaultCmd *cobra.Command) {
	vaultCmd.AddCommand(removeVaultCmd)
}
