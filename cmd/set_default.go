package cmd

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var setDefaultCmd = &cobra.Command{
	Use:     "set-default",
	Aliases: []string{"sd"},
	Short:   "Sets default vault",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		v := obsidian.Vault{Name: name}
		err := v.SetDefaultName(name)
		if err != nil {
			return err
		}
		path, err := v.Path()
		if err != nil {
			return err
		}
		fmt.Println("Default vault set to: ", name)
		fmt.Println("Default vault path set to: ", path)

		return nil
	},
}

func init() {
	vaultCmd.AddCommand(setDefaultCmd)
}
