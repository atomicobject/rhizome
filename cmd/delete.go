package cmd

import (
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete",
		Aliases: []string{"d"},
		Short:   "Delete note in vault",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			notePath := args[0]
			params := actions.DeleteParams{Context: cmd.Context(), NotePath: notePath}
			err := actions.DeleteNote(&vault, params)
			return err
		},
	}
}

func (commandOptions *noteInvocationOptions) registerDeleteCommands(deleteCmd, noteCmd *cobra.Command) {
	deleteCmd.Flags().BoolVarP(&commandOptions.shouldOpen, "open", "o", false, "open new note")
	deleteCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(deleteCmd)
}
