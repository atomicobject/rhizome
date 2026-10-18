package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultInvocationOptions) makePrintDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "print-default",
		Aliases: []string{"pd"},
		Short:   "prints default vault name and path",
		Args:    cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
			name, err := vault.DefaultName()
			if err != nil {
				return err
			}
			path, err := vault.Path()
			if err != nil {
				return err
			}
			fmt.Println("Default vault name: ", name)
			fmt.Println("Default vault path: ", path)
			return nil
		},
	}
}

func (commandOptions *vaultInvocationOptions) registerPrintDefaultCommands(vaultCmd, printDefaultCmd *cobra.Command) {
	vaultCmd.AddCommand(printDefaultCmd)
}
