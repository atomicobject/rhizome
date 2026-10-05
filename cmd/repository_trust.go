package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func newRepositoryTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust",
		Short: "Trust this repository to select its Rhizome executable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			checkout, err := repositoryCheckout(cwd)
			if err != nil {
				return err
			}
			store, err := repositorytrust.DefaultStore()
			if err != nil {
				return err
			}
			trusted, err := store.Trusted(checkout)
			if err != nil {
				return err
			}
			if trusted {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Repository is already trusted:\n%s\n", checkout)
				return err
			}
			if !isInteractive(cmd.InOrStdin()) {
				return errors.New("trust requires an interactive terminal; run `rzm trust` directly from the repository checkout")
			}
			confirmed, err := repositoryTrustPrompt(cmd.InOrStdin(), cmd.OutOrStdout())(checkout)
			if err != nil {
				return err
			}
			if !confirmed {
				return errors.New("repository trust was not confirmed")
			}
			canonical, err := store.Trust(checkout)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Trusted repository:\n%s\n", canonical)
			return err
		},
	}
}

func newRepositoryUntrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "untrust",
		Short: "Revoke this repository's Rhizome executable trust",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			checkout, err := repositoryCheckout(cwd)
			if err != nil {
				return err
			}
			store, err := repositorytrust.DefaultStore()
			if err != nil {
				return err
			}
			canonical, removed, err := store.Untrust(checkout)
			if err != nil {
				return err
			}
			if !removed {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Repository was not trusted:\n%s\n", canonical)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Revoked repository trust:\n%s\n", canonical)
			return err
		},
	}
}

func repositoryCheckout(cwd string) (string, error) {
	cfgDir, _, err := obsidian.FindLocalConfigForDelegation(cwd)
	if err == nil {
		return repositorytrust.CanonicalCheckout(cfgDir)
	}
	if !errors.Is(err, obsidian.ErrNoLocalConfig) {
		return "", err
	}
	if root, ok := findRhizomeSourceRoot(cwd); ok {
		return repositorytrust.CanonicalCheckout(root)
	}
	return "", errors.New("no Rhizome repository checkout found from the current directory")
}

func init() {
	rootCmd.AddCommand(newRepositoryTrustCmd(), newRepositoryUntrustCmd())
}
