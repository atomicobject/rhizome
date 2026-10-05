package cmd

import (
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var noteDailyCmd = &cobra.Command{
	Use:   "daily",
	Short: "Create or open the daily note in the vault",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		vault := obsidian.Vault{Name: vaultName}
		uri := obsidian.Uri{}
		err := actions.DailyNote(&vault, &uri)
		if err != nil {
			return err
		}
		return nil
	},
}

func init() {
	noteDailyCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name (not required if default is set)")
	noteCmd.AddCommand(noteDailyCmd)
}
