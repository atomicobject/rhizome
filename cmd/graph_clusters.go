package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"strings"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphClustersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clusters",
		Short: "Show mutual-link clusters (strongly connected components)",
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
						SkipAnchors: commandOptions.clustersSkipAnchors,
						SkipEmbeds:  commandOptions.clustersSkipEmbeds,
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

			fmt.Fprintf(cmd.OutOrStdout(), "Mutual-link clusters for vault %q (%s)\n", selectedVault, vaultPath)

			var clusters [][]string
			for _, component := range analysis.StrongComponents {
				if len(component) <= 1 {
					continue
				}
				clusters = append(clusters, component)
			}

			if len(clusters) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
				return nil
			}

			limit := commandOptions.graphLimit
			if commandOptions.graphShowAll || limit <= 0 || limit > len(clusters) {
				limit = len(clusters)
			}
			if !commandOptions.graphShowAll && limit < len(clusters) {
				fmt.Fprintf(cmd.OutOrStdout(), "Showing top %d of %d clusters:\n", limit, len(clusters))
			}

			for i := 0; i < limit; i++ {
				component := clusters[i]
				fmt.Fprintf(cmd.OutOrStdout(), "  size %d: %s\n", len(component), strings.Join(component, ", "))
			}
			if !commandOptions.graphShowAll && limit < len(clusters) {
				fmt.Fprintf(cmd.OutOrStdout(), "  ... (%d more)\n", len(clusters)-limit)
			}

			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerGraphClustersCommands(graphClustersCmd, graphCmd *cobra.Command) {
	graphClustersCmd.Flags().BoolVar(&commandOptions.clustersSkipAnchors, "skip-anchors", false, "skip wikilinks that contain anchors (e.g. [[Note#Section]])")
	graphClustersCmd.Flags().BoolVar(&commandOptions.clustersSkipEmbeds, "skip-embeds", false, "skip embedded wikilinks (e.g. ![[Embedded Note]])")

	graphCmd.AddCommand(graphClustersCmd)
}
