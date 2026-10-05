package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/vault/changelog"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Show the changelog",
	Long:  `Display the changelog showing recent changes and release history.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprint(cmd.OutOrStdout(), changelog.Content)
	},
}

func init() {
	rootCmd.AddCommand(changelogCmd)
}
