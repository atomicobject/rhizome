package cmd

import "github.com/spf13/cobra"

func (commandOptions *noteInvocationOptions) makeNoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "note",
		Short: "Work with notes (open/print/create/find/daily/tags/properties/move/rename)",
	}
}

func (commandOptions *noteInvocationOptions) registerNoteCommands(rootCmd, noteCmd *cobra.Command) {
	rootCmd.AddCommand(noteCmd)
}
