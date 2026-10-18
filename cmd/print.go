package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makePrintCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "print",
		Aliases: []string{"p"},
		Short:   "Print contents of note",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			noteName := args[0]
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
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
}

func (commandOptions *noteInvocationOptions) registerPrintCommands(printCmd, noteCmd *cobra.Command) {
	printCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	noteCmd.AddCommand(printCmd)
}
