package cmd

import (
	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var stopAll bool

var stopCmd = &cobra.Command{
	Use: "stop",
	// A runtime that will not stop is an operational result, not a usage error.
	SilenceUsage: true,
	Short:        "Stop the Rhizome runtime serving this vault",
	Long: "Stop the vault's runtime gracefully and wait for it to exit.\n" +
		"A headless runtime that ignores shutdown is terminated after a grace period;\n" +
		"an attached one is reported instead, so the terminal that started it stays in control.",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := appserve.StopOptions{All: stopAll, Out: cmd.OutOrStdout()}
		if !stopAll {
			resolvedName := vaultName
			if resolvedName == "" {
				defaultName, err := (&obsidian.Vault{}).DefaultName()
				if err != nil {
					return err
				}
				resolvedName = defaultName
			}
			vaultDef, err := (&obsidian.Vault{Name: resolvedName}).Definition()
			if err != nil {
				return err
			}
			opts.VaultPath = vaultDef.BasePath()
		}
		return appserve.Stop(cmd.Context(), opts)
	},
}

func init() {
	stopCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	stopCmd.Flags().BoolVar(&stopAll, "all", false, "stop every running Rhizome runtime")
	rootCmd.AddCommand(stopCmd)
}
