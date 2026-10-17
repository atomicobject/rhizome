package cmd

import (
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"

	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphVaultContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vault-context",
		Short: "Print an LLM-optimized vault context (budgeted text)",
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

			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: selectedVault}

			// Get vault path to load compression config
			vaultDef, err := vault.Definition()
			if err != nil {
				return err
			}
			vaultPath := vaultDef.BasePath()
			noteMetadata, err := newNoteMetadataIndexer()
			if err != nil {
				return err
			}
			note, err := newProjectedNoteReader(cmd.Context(), vaultDef)
			if err != nil {
				return err
			}

			compressor := commandOptions.loadContextCompressor(cmd.Context(), vaultPath)
			plan := oneshotruntime.GraphContextPlan(compressor != nil)
			if err := plan.ValidateForAuthority(oneshotruntime.AuthorityReadOnly); err != nil {
				return err
			}

			text, err := actions.BuildVaultContextText(&vault, note, actions.VaultContextTextParams{
				NoteMetadata: noteMetadata,
				BudgetChars:  commandOptions.vaultContextBudgetChars,
				Profile:      actions.ContextProfile(commandOptions.vaultContextProfile),
				SkipAnchors:  commandOptions.graphSkipAnchors,
				SkipEmbeds:   commandOptions.graphSkipEmbeds,
				GraphSummary: commandOptions.vaultContextGraphSummary,
				Compressor:   compressor,
				// Intent is set automatically for code profile in BuildVaultContextText
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), text)
			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerGraphVaultContextCommands(graphVaultContextCmd, graphCmd *cobra.Command) {
	graphVaultContextCmd.Flags().IntVar(&commandOptions.vaultContextBudgetChars, "budget-chars", repoconfig.DefaultBudgetChars, "max output size in characters")
	graphVaultContextCmd.Flags().StringVar(&commandOptions.vaultContextProfile, "profile", "auto", "context profile: auto|vault|code")
	graphVaultContextCmd.Flags().BoolVar(&commandOptions.vaultContextGraphSummary, "graph-summary", false, "include graph communities/orphans even when ontology is present")
	graphCmd.AddCommand(graphVaultContextCmd)
}
