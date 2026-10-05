package cmd

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var printDefaultCmd = &cobra.Command{
	Use:     "print-default",
	Aliases: []string{"pd"},
	Short:   "prints default vault name and path",
	Args:    cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		vault := obsidian.Vault{}
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

func init() {
	vaultCmd.AddCommand(printDefaultCmd)
}
