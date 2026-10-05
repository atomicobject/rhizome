package cmd

import (
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	vaultContextBudgetChars  int
	vaultContextProfile      string
	vaultContextGraphSummary bool
)

var graphVaultContextCmd = &cobra.Command{
	Use:   "vault-context",
	Short: "Print an LLM-optimized vault context (budgeted text)",
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

		vault := obsidian.Vault{Name: selectedVault}

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

		compressor := loadContextCompressor(vaultPath)
		plan := oneshotruntime.GraphContextPlan(compressor != nil)
		if err := plan.ValidateForAuthority(oneshotruntime.AuthorityReadOnly); err != nil {
			return err
		}

		text, err := actions.BuildVaultContextText(&vault, note, actions.VaultContextTextParams{
			NoteMetadata: noteMetadata,
			BudgetChars:  vaultContextBudgetChars,
			Profile:      actions.ContextProfile(vaultContextProfile),
			SkipAnchors:  graphSkipAnchors,
			SkipEmbeds:   graphSkipEmbeds,
			GraphSummary: vaultContextGraphSummary,
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

func init() {
	graphVaultContextCmd.Flags().IntVar(&vaultContextBudgetChars, "budget-chars", contextpack.DefaultBudgetChars, "max output size in characters")
	graphVaultContextCmd.Flags().StringVar(&vaultContextProfile, "profile", "auto", "context profile: auto|vault|code")
	graphVaultContextCmd.Flags().BoolVar(&vaultContextGraphSummary, "graph-summary", false, "include graph communities/orphans even when ontology is present")
	graphCmd.AddCommand(graphVaultContextCmd)
}
