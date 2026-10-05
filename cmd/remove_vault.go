package cmd

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/spf13/cobra"
)

var removeVaultCmd = &cobra.Command{
	Use: "remove [name]",
	Aliases: []string{
		"remove-vault",
	},
	Short: "Remove a vault path from rhizome preferences",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		vault := obsidian.Vault{Name: name}
		if err := vault.RemoveFromPreferences(); err != nil {
			return err
		}

		fmt.Printf("Vault '%s' removed from preferences\n", name)
		return nil
	},
}

func init() {
	vaultCmd.AddCommand(removeVaultCmd)
}
