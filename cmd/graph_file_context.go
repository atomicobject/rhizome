package cmd

import (
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"

	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

// fileContextIntent is the default intent for file_context compression.
const fileContextIntent = "Produce a token-dense context for the specified files. " +
	"Prioritize: (1) key constraints and invariants, (2) module boundaries and responsibilities, " +
	"(3) important patterns and extension points. " +
	"Omit boilerplate and content that doesn't help understand or safely modify the code."

func (commandOptions *graphInvocationOptions) makeGraphFileContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "file-context [files...]",
		Short: "Print LLM-optimized context for notes or code files (budgeted text)",
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

			ensureMode, err := actions.ParseEnsureLinkTargetMode(commandOptions.fcEnsureLinks)
			if err != nil {
				return err
			}
			plan := oneshotruntime.GraphContextPlan(compressor != nil)
			if err := plan.ValidateForAuthority(oneshotruntime.AuthorityWriteCapable); err != nil {
				return err
			}

			var codeRefsByFile map[string][]coderefs.CodeRef
			if ensureMode != ontology.EnsureLinkTargetNever {
				codeRefsByFile, err = actions.ScanConfiguredCodeRefsForInputs(cmd.Context(), vaultDef, note, args)
				if err != nil {
					return err
				}
			}
			text, err := actions.BuildFileContextText(&vault, note, actions.FileContextTextParams{
				NoteMetadata:     noteMetadata,
				ApplyLinkTargets: liveNodeLinkApplier(vaultDef, noteMetadata),
				BudgetChars:      commandOptions.fcBudgetChars,
				Profile:          actions.ContextProfile(commandOptions.fcProfile),
				SkipAnchors:      commandOptions.graphSkipAnchors,
				SkipEmbeds:       commandOptions.graphSkipEmbeds,
				Files:            args,
				Compressor:       compressor,
				Intent:           fileContextIntent,
				EnsureLinkTarget: ensureMode,
				CodeRefsByFile:   codeRefsByFile,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), text)
			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerGraphFileContextCommands(graphFileContextCmd, graphCmd *cobra.Command) {
	graphFileContextCmd.Flags().IntVar(&commandOptions.fcBudgetChars, "budget-chars", repoconfig.DefaultBudgetChars, "max output size in characters")
	graphFileContextCmd.Flags().StringVar(&commandOptions.fcProfile, "profile", "auto", "context profile: auto|vault|code")
	graphFileContextCmd.Flags().StringVar(&commandOptions.fcEnsureLinks, "ensure-link-targets", "", "embedded node link repair mode: never|plan|apply")
	graphCmd.AddCommand(graphFileContextCmd)
}
