package cmd

import (
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/spf13/cobra"
)

var noteFindCmd = &cobra.Command{
	Use:     "find",
	Aliases: []string{"f"},
	Short:   "Fuzzy-find and open a note in the vault",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		vault := obsidian.Vault{Name: vaultName}
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

func init() {
	noteFindCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(noteFindCmd)
}
