package cmd

import (
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var vaultName string
var OpenVaultCmd = &cobra.Command{
	Use:     "open",
	Aliases: []string{"o"},
	Short:   "Opens note in vault by note name",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vault := obsidian.Vault{Name: vaultName}
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

func init() {
	OpenVaultCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name (not required if default is set)")
	noteCmd.AddCommand(OpenVaultCmd)
}
