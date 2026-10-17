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

func (commandOptions *graphInvocationOptions) makeOrphansCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "orphans",
		Short: "List notes with no inbound or outbound wikilinks",
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

			analysis, err := actions.GraphAnalysis(&vault, &note, actions.GraphAnalysisParams{
				UseConfig:    true,
				NoteMetadata: noteMetadata,
				Options: vaultgraph.GraphAnalysisOptions{
					WikilinkOptions: markdownsyntax.WikilinkOptions{
						SkipAnchors: commandOptions.orphansSkipAnchors,
						SkipEmbeds:  commandOptions.orphansSkipEmbeds,
					},
					MinDegree:         commandOptions.graphMinDegree,
					MutualOnly:        commandOptions.graphMutualOnly,
					RecencyCascade:    commandOptions.graphRecencyCascade,
					RecencyCascadeSet: true,
				},
				ExcludePatterns: commandOptions.graphExcludePatterns,
				IncludePatterns: commandOptions.graphIncludePatterns,
			})
			if err != nil {
				return err
			}

			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Orphans (no inbound or outbound wikilinks) in %q (%s):\n", selectedVault, vaultPath)
			if len(analysis.Orphans) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
				return nil
			}

			sorted := make([]string, len(analysis.Orphans))
			copy(sorted, analysis.Orphans)
			sort.Strings(sorted)

			for _, path := range sorted {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", path)
			}
			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerOrphansCommands(orphansCmd, graphCmd *cobra.Command) {
	orphansCmd.Flags().BoolVar(&commandOptions.orphansSkipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	orphansCmd.Flags().BoolVar(&commandOptions.orphansSkipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")

	graphCmd.AddCommand(orphansCmd)
}
