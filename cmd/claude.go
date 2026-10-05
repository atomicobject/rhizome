package cmd

import "github.com/atomicobject/rhizome/pkg/app/bootstrap"

var claudeCmd = newBootstrapCommand("Claude", bootstrap.RunClaude)

func init() {
	rootCmd.AddCommand(claudeCmd)
}
