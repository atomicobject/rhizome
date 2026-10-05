package cmd

import (
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var shouldAppend bool
var shouldOverwrite bool
var content string
var createNoteCmd = &cobra.Command{
	Use:     "create",
	Aliases: []string{"c"},
	Short:   "Creates note in vault",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vault := obsidian.Vault{Name: vaultName}
		uri := obsidian.Uri{}
		noteName := args[0]
		params := actions.CreateParams{
			NoteName:        noteName,
			Content:         content,
			ShouldAppend:    shouldAppend,
			ShouldOverwrite: shouldOverwrite,
			ShouldOpen:      shouldOpen,
		}
		err := actions.CreateNote(&vault, &uri, params)
		if err != nil {
			return err
		}
		return nil
	},
}

func init() {
	createNoteCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	createNoteCmd.Flags().BoolVarP(&shouldOpen, "open", "", false, "open created note")
	createNoteCmd.Flags().StringVarP(&content, "content", "c", "", "text to add to note")
	createNoteCmd.Flags().BoolVarP(&shouldAppend, "append", "a", false, "append to note")
	createNoteCmd.Flags().BoolVarP(&shouldOverwrite, "overwrite", "o", false, "overwrite note")
	createNoteCmd.MarkFlagsMutuallyExclusive("append", "overwrite")
	noteCmd.AddCommand(createNoteCmd)
}
