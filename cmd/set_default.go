package cmd

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultInvocationOptions) makeSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set-default",
		Aliases: []string{"sd"},
		Short:   "Sets default vault",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			v := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: name}
			err := v.SetDefaultName(name)
			if err != nil {
				return err
			}
			path, err := v.Path()
			if err != nil {
				return err
			}
			fmt.Println("Default vault set to: ", name)
			fmt.Println("Default vault path set to: ", path)

			return nil
		},
	}
}

func (commandOptions *vaultInvocationOptions) registerSetDefaultCommands(vaultCmd, setDefaultCmd *cobra.Command) {
	vaultCmd.AddCommand(setDefaultCmd)
}
