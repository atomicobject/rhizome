package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultInvocationOptions) makeInstallIgnoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-ignore",
		Short: "Write the default .rhizome/ignore into the vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")

			if commandOptions.vaultName == "" {
				vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
				defaultName, err := vault.DefaultName()
				if err != nil {
					return fmt.Errorf("failed to get default vault name: %w", err)
				}
				commandOptions.vaultName = defaultName
			}

			vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			path, err := actions.InstallDefaultIgnore(vault, actions.InstallIgnoreOptions{Force: force})
			if err != nil {
				return err
			}

			fmt.Printf("Wrote default ignore file to %s\n", path)
			return nil
		},
	}
}

func (commandOptions *vaultInvocationOptions) registerIgnoreCommands(installIgnoreCmd, vaultCmd *cobra.Command) {
	installIgnoreCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name")
	installIgnoreCmd.Flags().Bool("force", false, "Overwrite an existing .rhizome/ignore")
	vaultCmd.AddCommand(installIgnoreCmd)
}
