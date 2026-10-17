package cmd

import (
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *stopInvocationOptions) makeStopCmd() *cobra.Command {
	return &cobra.Command{
		Use: "stop",
		// A runtime that will not stop is an operational result, not a usage error.
		SilenceUsage: true,
		Short:        "Stop the Rhizome runtime serving this vault",
		Long: "Stop the vault's runtime gracefully and wait for it to exit.\n" +
			"A headless runtime that ignores shutdown is terminated after a grace period;\n" +
			"an attached one is reported instead, so the terminal that started it stays in control.",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := appruntime.StopOptions{All: commandOptions.stopAll, Out: cmd.OutOrStdout()}
			if !commandOptions.stopAll {
				resolvedName := commandOptions.vaultName
				if resolvedName == "" {
					defaultName, err := (&repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}).DefaultName()
					if err != nil {
						return err
					}
					resolvedName = defaultName
				}
				vaultDef, err := (&repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: resolvedName}).Definition()
				if err != nil {
					return err
				}
				opts.VaultPath = vaultDef.BasePath()
			}
			return appruntime.Stop(cmd.Context(), opts)
		},
	}
}

func (commandOptions *stopInvocationOptions) registerStopCommands(stopCmd, rootCmd *cobra.Command) {
	stopCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (uses default if unset)")
	stopCmd.Flags().BoolVar(&commandOptions.stopAll, "all", false, "stop every running Rhizome runtime")
	rootCmd.AddCommand(stopCmd)
}
