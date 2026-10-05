package cmd

import (
	"fmt"
	"log"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var graphIgnoreCmd = &cobra.Command{
	Use:   "ignore [patterns...]",
	Short: "Set graph ignore patterns for this vault (.rhizome/config.yml)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		selectedVault := vaultName
		if selectedVault == "" {
			v := &obsidian.Vault{}
			name, err := v.DefaultName()
			if err != nil {
				return err
			}
			selectedVault = name
		}

		vault := &obsidian.Vault{Name: selectedVault}
		vaultPath, err := vault.Path()
		if err != nil {
			return err
		}

		cfg, err := obsidian.LoadGraphConfig(vaultPath)
		if err != nil {
			return err
		}
		cfg.Ignore = args

		if err := obsidian.SaveGraphConfig(vaultPath, cfg); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Saved graph ignore patterns for vault %q (%s): %v\n", selectedVault, vaultPath, args)
		return nil
	},
}

func init() {
	graphIgnoreCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	graphCmd.AddCommand(graphIgnoreCmd)
	if err := graphIgnoreCmd.MarkFlagRequired("vault"); err != nil {
		log.Printf("MarkFlagRequired failed: %v", err)
	}
}
