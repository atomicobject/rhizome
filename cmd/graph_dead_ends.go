package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"sort"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeDeadEndsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dead-ends",
		Short: "List notes with incoming links but no outgoing links",
		Long: `Find notes that are linked to but don't link to other notes.

Dead-end notes are potential stubs or incomplete notes that may need expansion.
They have at least one inbound link but zero outbound wikilinks.

Orphan notes (no inbound or outbound links) are not included here;
use 'graph orphans' to find those.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			selectedVault := commandOptions.vaultName
			if selectedVault == "" {
				vault := &repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config}
				defaultName, err := vault.DefaultName()
				if err != nil {
					return err
				}
				selectedVault = defaultName
			}

			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: selectedVault}
			note := obsidian.Note{}
			noteMetadata, err := newNoteMetadataIndexer()
			if err != nil {
				return err
			}

			params := actions.GraphAnalysisParams{
				UseConfig:    true,
				NoteMetadata: noteMetadata,
				Options: vaultgraph.GraphAnalysisOptions{
					WikilinkOptions: markdownsyntax.WikilinkOptions{
						SkipAnchors: commandOptions.deadEndsSkipAnchors,
						SkipEmbeds:  commandOptions.deadEndsSkipEmbeds,
					},
					MinDegree:  commandOptions.graphMinDegree,
					MutualOnly: commandOptions.graphMutualOnly,
				},
				ExcludePatterns: commandOptions.graphExcludePatterns,
				IncludePatterns: commandOptions.graphIncludePatterns,
			}

			deadEnds, err := actions.DeadEnds(&vault, &note, params)
			if err != nil {
				return err
			}

			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Dead-end notes in vault %q (%s):\n", selectedVault, vaultPath)

			if len(deadEnds) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  No dead-end notes found.")
				return nil
			}

			// Sort by path
			sort.Slice(deadEnds, func(i, j int) bool {
				return deadEnds[i].Path < deadEnds[j].Path
			})

			// Determine limit
			limit := commandOptions.deadEndsLimit
			if commandOptions.deadEndsAll {
				limit = 0 // 0 means no limit
			}

			shown := 0
			for _, de := range deadEnds {
				if limit > 0 && shown >= limit {
					break
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s (%d inbound link(s))\n", de.Path, de.InboundLinks)
				shown++
			}

			// Show total count
			fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d dead-end note(s)", len(deadEnds))
			if limit > 0 && shown < len(deadEnds) {
				fmt.Fprintf(cmd.OutOrStdout(), "  (showing %d of %d)", shown, len(deadEnds))
			}
			fmt.Fprintln(cmd.OutOrStdout())

			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerGraphDeadEndsCommands(deadEndsCmd, graphCmd *cobra.Command) {
	deadEndsCmd.Flags().BoolVar(&commandOptions.deadEndsSkipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	deadEndsCmd.Flags().BoolVar(&commandOptions.deadEndsSkipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")
	deadEndsCmd.Flags().IntVar(&commandOptions.deadEndsLimit, "limit", 25, "maximum number of dead-end notes to display (0 for unlimited)")
	deadEndsCmd.Flags().BoolVar(&commandOptions.deadEndsAll, "all", false, "show all dead-end notes (overrides --limit)")

	graphCmd.AddCommand(deadEndsCmd)
}
