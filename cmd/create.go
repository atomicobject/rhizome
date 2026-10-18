package cmd

import (
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeCreateNoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "create",
		Aliases: []string{"c"},
		Short:   "Creates note in vault",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			uri := obsidian.Uri{}
			noteName := args[0]
			params := actions.CreateParams{
				NoteName:        noteName,
				Content:         commandOptions.content,
				ShouldAppend:    commandOptions.shouldAppend,
				ShouldOverwrite: commandOptions.shouldOverwrite,
				ShouldOpen:      commandOptions.shouldOpen,
			}
			err := actions.CreateNote(&vault, &uri, params)
			if err != nil {
				return err
			}
			return nil
		},
	}
}

func (commandOptions *noteInvocationOptions) registerCreateCommands(createNoteCmd, noteCmd *cobra.Command) {
	createNoteCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	createNoteCmd.Flags().BoolVarP(&commandOptions.shouldOpen, "open", "", false, "open created note")
	createNoteCmd.Flags().StringVarP(&commandOptions.content, "content", "c", "", "text to add to note")
	createNoteCmd.Flags().BoolVarP(&commandOptions.shouldAppend, "append", "a", false, "append to note")
	createNoteCmd.Flags().BoolVarP(&commandOptions.shouldOverwrite, "overwrite", "o", false, "overwrite note")
	createNoteCmd.MarkFlagsMutuallyExclusive("append", "overwrite")
	noteCmd.AddCommand(createNoteCmd)
}
