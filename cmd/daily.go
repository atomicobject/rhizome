package cmd

import (
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeNoteDailyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daily",
		Short: "Create or open the daily note in the vault",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			uri := obsidian.Uri{}
			err := actions.DailyNote(&vault, &uri)
			if err != nil {
				return err
			}
			return nil
		},
	}
}

func (commandOptions *noteInvocationOptions) registerDailyCommands(noteDailyCmd, noteCmd *cobra.Command) {
	noteDailyCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (not required if default is set)")
	noteCmd.AddCommand(noteDailyCmd)
}
