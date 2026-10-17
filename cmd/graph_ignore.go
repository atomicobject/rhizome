package cmd

import (
	"fmt"
	"log"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphIgnoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ignore [patterns...]",
		Short: "Set graph ignore patterns for this vault (.rhizome/config.yml)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selectedVault := commandOptions.vaultName
			if selectedVault == "" {
				v := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
				name, err := v.DefaultName()
				if err != nil {
					return err
				}
				selectedVault = name
			}

			vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: selectedVault}
			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}
			resolvedConfig1, configErr1 := repoconfig.Load(vaultPath)
			cfg, err := resolvedConfig1.Graph, configErr1
			if err != nil {
				return err
			}
			cfg.Ignore = args

			if err := repoconfig.SaveGraphConfig(vaultPath, cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Saved graph ignore patterns for vault %q (%s): %v\n", selectedVault, vaultPath, args)
			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerGraphIgnoreCommands(graphIgnoreCmd, graphCmd *cobra.Command) {
	graphIgnoreCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	graphCmd.AddCommand(graphIgnoreCmd)
	if err := graphIgnoreCmd.MarkFlagRequired("vault"); err != nil {
		log.Printf("MarkFlagRequired failed: %v", err)
	}
}
