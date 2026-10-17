package cmd

import "github.com/spf13/cobra"

import "github.com/atomicobject/rhizome/pkg/app/bootstrap"

func (commandOptions *vaultCommandOptions) makeCodexCmd() *cobra.Command {
	return newBootstrapCommand("Codex", bootstrap.RunCodex)
}

func (commandOptions *vaultCommandOptions) registerCodexCommands(rootCmd, codexCmd *cobra.Command) {
	rootCmd.AddCommand(codexCmd)
}
