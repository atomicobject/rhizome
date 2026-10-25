package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

type credentialImport func(context.Context, string, []string) (int, error)

func (commandOptions *vaultCommandOptions) makeCredentialsCmd() *cobra.Command {
	return newCredentialsCommand(credentials.ImportFromOnePassword, credentials.ImportEnvironment)
}

func newCredentialsCommand(importCredentials credentialImport, importEnvironment func(context.Context, repoconfig.CredentialSource) (int, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "credentials",
		Short: "Import provider credentials into the user-level configuration",
	}
	importCommand := &cobra.Command{
		Use:   "import",
		Short: "Import provider credentials from 1Password",
		Long: `Read a 1Password Environment with stable CLI 2.41+ using --environment ID and bare --key names, or read stable 1Password item references and save their values in ~/.config/rhizome/config.yml.

For example:
  rzm credentials import --from 1password --account my.1password.com \
    --key VOYAGE_API_KEY=op://Private/rhizome-voyage/credential \
    --key TYPESAFE_API_KEY=op://Private/rhizome-typesafe/credential`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, err := cmd.Flags().GetString("from")
			if err != nil {
				return err
			}
			if !strings.EqualFold(strings.TrimSpace(from), "1password") {
				return fmt.Errorf("unsupported credential source %q", from)
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			keys, err := cmd.Flags().GetStringArray("key")
			if err != nil {
				return err
			}
			environment, _ := cmd.Flags().GetString("environment")
			var count int
			if environment != "" {
				count, err = importEnvironment(cmd.Context(), repoconfig.CredentialSource{Account: account, Environment: environment, Keys: keys})
			} else {
				count, err = importCredentials(cmd.Context(), account, keys)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported %d credential(s) into ~/.config/rhizome/config.yml\n", count)
			return nil
		},
	}
	importCommand.Flags().String("from", "", "credential source (1password)")
	importCommand.Flags().String("account", "", "1Password account")
	importCommand.Flags().String("environment", "", "1Password Environment ID (requires stable CLI 2.41+)")
	importCommand.Flags().StringArray("key", nil, "supported KEY for an Environment, or KEY=op://vault/item/field")
	_ = importCommand.MarkFlagRequired("from")
	_ = importCommand.MarkFlagRequired("account")
	_ = importCommand.MarkFlagRequired("key")
	command.AddCommand(importCommand)
	return command
}

func (commandOptions *vaultCommandOptions) registerCredentialsCommands(rootCmd, credentialsCmd *cobra.Command) {
	rootCmd.AddCommand(credentialsCmd)
}
