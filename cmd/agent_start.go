package cmd

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentstart"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/spf13/cobra"
)

type agentStartResponse = agentstart.Response

func newAgentStartCmd() *cobra.Command {
	var profile, intent string
	var contextFiles, files []string
	var budgetChars, submoduleDepth int
	var includeTags, recencyCascade, graphSummary bool
	var includeOntology bool
	var timings bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Initialize an agent session with vault context",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fire-and-forget: this read-only command never waits on the
			// runtime, it only makes sure the vault has one warming up.
			startVaultRuntimeInBackground(cmd.Context(), vaultDefOrDefaultContext)
			if !cmd.Flags().Changed("context-file") {
				contextFiles = nil
			}
			if !cmd.Flags().Changed("file") {
				files = nil
			}
			payload, err := executeAgentStart(cmd.Context(), agentstart.Request{
				Profile: profile, Intent: intent, ContextFiles: contextFiles, Files: files,
				BudgetChars: budgetChars, SubmoduleDepth: submoduleDepth,
				SkipAnchors: skipAnchors, SkipEmbeds: skipEmbeds, IncludeTags: includeTags,
				RecencyCascade: recencyCascade, GraphSummary: graphSummary,
				IncludeOntology: includeOntology, Timings: timings, SessionID: agentSessionID,
			})
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			if err := writeAgentPayload(cmd, payload); err != nil {
				return err
			}
			return nil
		},
	}

	addBudgetFlag(cmd, &budgetChars)
	cmd.Flags().StringVar(&profile, "profile", "", "context profile")
	cmd.Flags().StringVar(&intent, "intent", "", "task intent")
	cmd.Flags().StringArrayVar(&contextFiles, "context-file", nil, "context file path")
	cmd.Flags().StringArrayVar(&files, "file", nil, "file or directory to include targeted context for")
	cmd.Flags().IntVar(&submoduleDepth, "submodule-depth", 0, "submodule doc depth for directory targets")
	cmd.Flags().BoolVar(&skipAnchors, "skip-anchors", false, "skip wikilinks with anchors")
	cmd.Flags().BoolVar(&skipEmbeds, "skip-embeds", false, "skip embedded wikilinks")
	cmd.Flags().BoolVar(&includeTags, "include-tags", true, "include tags in graph context")
	cmd.Flags().BoolVar(&recencyCascade, "recency-cascade", true, "include recency cascade")
	cmd.Flags().BoolVar(&graphSummary, "graph-summary", false, "include graph communities/orphans even when ontology is present")
	cmd.Flags().BoolVar(&includeOntology, "ontology", false, "include ontology type-count summary")
	cmd.Flags().BoolVar(&timings, "timings", false, "include stable startup timing and operation diagnostics")

	return cmd
}

func executeAgentStart(ctx context.Context, request agentstart.Request) (agentstart.Response, error) {
	return agentstart.Execute(ctx, agentstart.Dependencies{
		ResolveVault: vaultDefOrDefaultContext,
		Formats:      newNoteFormatRuntime,
		BuildConfig: func(ctx context.Context, budget int, requirements bootstrap.RuntimeRequirements) (agentapi.Config, func(), error) {
			cfg, runtime, err := buildAgentConfigWithRequirements(ctx, budget, "start", requirements)
			if err != nil {
				return agentapi.Config{}, nil, err
			}
			cleanup := func() {}
			if runtime != nil {
				cleanup = func() { runtime.Close() }
			}
			return cfg, cleanup, nil
		},
	}, request)
}
