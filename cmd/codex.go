package cmd

import "github.com/atomicobject/rhizome/pkg/app/bootstrap"

var codexCmd = newBootstrapCommand("Codex", bootstrap.RunCodex)

func init() {
	rootCmd.AddCommand(codexCmd)
}
