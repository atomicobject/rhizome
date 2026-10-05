package cmd

import "github.com/spf13/cobra"

var noteCmd = &cobra.Command{
	Use:   "note",
	Short: "Work with notes (open/print/create/find/daily/tags/properties/move/rename)",
}

func init() {
	rootCmd.AddCommand(noteCmd)
}
