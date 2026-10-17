package cmd

import "github.com/spf13/cobra"

import "github.com/atomicobject/rhizome/pkg/app/bootstrap"

func (commandOptions *vaultCommandOptions) makeClaudeCmd() *cobra.Command {
	return newBootstrapCommand("Claude", bootstrap.RunClaude)
}

func (commandOptions *vaultCommandOptions) registerClaudeCommands(rootCmd, claudeCmd *cobra.Command) {
	rootCmd.AddCommand(claudeCmd)
}
