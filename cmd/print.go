package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var printCmd = &cobra.Command{
	Use:     "print",
	Aliases: []string{"p"},
	Short:   "Print contents of note",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		noteName := args[0]
		vault := obsidian.Vault{Name: vaultName}
		note := obsidian.Note{}
		params := actions.PrintParams{
			NoteName: noteName,
		}
		contents, err := actions.PrintNote(&vault, &note, params)
		if err != nil {
			return err
		}
		fmt.Println(contents)
		return nil
	},
}

func init() {
	printCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(printCmd)
}
