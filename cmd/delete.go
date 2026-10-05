package cmd

import (
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"d"},
	Short:   "Delete note in vault",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vault := obsidian.Vault{Name: vaultName}
		notePath := args[0]
		params := actions.DeleteParams{Context: cmd.Context(), NotePath: notePath}
		err := actions.DeleteNote(&vault, params)
		return err
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&shouldOpen, "open", "o", false, "open new note")
	deleteCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(deleteCmd)
}
