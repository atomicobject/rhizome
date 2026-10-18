package cmd

import "github.com/spf13/cobra"

func (commandOptions *vaultInvocationOptions) makeVaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vault",
		Short: "Manage vault selection and paths",
	}
}

func (commandOptions *vaultInvocationOptions) registerVaultCommands(rootCmd, vaultCmd *cobra.Command) {
	rootCmd.AddCommand(vaultCmd)
}
