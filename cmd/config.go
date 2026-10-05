package cmd

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage rzm CLI configuration",
}

var configSetEnvCmd = &cobra.Command{
	Use:   "set-env KEY VALUE",
	Short: "Persist an environment value into ~/.config/rhizome/config.yml",
	Long: `Persist an environment value into the user-level rzm CLI config.

Used by setup tooling. Empty values clear the entry.`,
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := strings.TrimSpace(args[0])
		if key == "" {
			return fmt.Errorf("key must be non-empty")
		}
		value := strings.TrimSpace(args[1])

		cfg, err := obsidian.LoadCliConfig(true)
		if err != nil {
			return fmt.Errorf("load CLI config: %w", err)
		}
		if cfg.Env == nil {
			cfg.Env = map[string]string{}
		}
		if value == "" {
			delete(cfg.Env, key)
		} else {
			cfg.Env[key] = value
		}
		if err := obsidian.SaveCliConfig(cfg); err != nil {
			return fmt.Errorf("save CLI config: %w", err)
		}
		if value == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Cleared %s in ~/.config/rhizome/config.yml\n", key)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Saved %s in ~/.config/rhizome/config.yml\n", key)
		}
		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetEnvCmd)
	rootCmd.AddCommand(configCmd)
}
