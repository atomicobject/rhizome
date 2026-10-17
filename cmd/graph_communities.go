package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphCommunitiesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "communities",
		Short: "Show loosely connected communities (label propagation)",
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
			vaultDef, err := vault.Definition()
			if err != nil {
				return err
			}
			note, err := newProjectedNoteReader(cmd.Context(), vaultDef)
			if err != nil {
				return err
			}
			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}
			intelStore, cleanup, err := openOptionalIntelStoreForAnalysis(vaultPath)
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}

			noteMetadata, err := newNoteMetadataIndexer()
			if err != nil {
				return err
			}
			analysis, err := actions.DocGraphAnalysis(cmd.Context(), &vault, note, actions.DocGraphAnalysisParams{
				NoteMetadata:    noteMetadata,
				UseConfig:       true,
				IncludeTags:     true,
				ExcludePatterns: commandOptions.graphExcludePatterns,
				IncludePatterns: commandOptions.graphIncludePatterns,
				IntelStore:      intelStore,
				WikilinkOptions: markdownsyntax.DefaultWikilinkOptions,
			})
			if err != nil {
				return err
			}

			communities := analysis.Communities
			fmt.Fprintf(cmd.OutOrStdout(), "Communities for vault %q (%s)\n", selectedVault, vaultPath)
			if len(communities) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
				return nil
			}

			limit := commandOptions.graphLimit
			if commandOptions.graphShowAll || limit <= 0 || limit > len(communities) {
				limit = len(communities)
			}
			if !commandOptions.graphShowAll && limit < len(communities) {
				fmt.Fprintf(cmd.OutOrStdout(), "Showing top %d of %d communities:\n", limit, len(communities))
			}

			for i := 0; i < limit; i++ {
				c := communities[i]
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "----------------------------------------")
				}
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintf(cmd.OutOrStdout(), "  community %s (size %d)\n", commandOptions.colorCommunity(c.ID), len(c.Nodes))
				if c.Anchor != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    anchor: %s\n", c.Anchor)
				}
				if c.Density > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "    density: %.3f\n", c.Density)
				}
				if c.Recency != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "    recency: %.1f days ago (%d in last %dd)\n", c.Recency.LatestAgeDays, c.Recency.RecentCount, c.Recency.WindowDays)
				}
				if len(c.TopTags) > 0 {
					var tags []string
					tagLimit := commandOptions.graphLimit
					if commandOptions.graphShowAll || tagLimit <= 0 || tagLimit > len(c.TopTags) {
						tagLimit = len(c.TopTags)
					}
					for j := 0; j < tagLimit; j++ {
						tt := c.TopTags[j]
						tags = append(tags, fmt.Sprintf("%s(%d)", tt.Tag, tt.Count))
					}
					if !commandOptions.graphShowAll && tagLimit < len(c.TopTags) {
						tags = append(tags, "...")
					}
					fmt.Fprintf(cmd.OutOrStdout(), "    tags: %s\n", strings.Join(tags, ", "))
				}

				noteLimit := commandOptions.graphLimit
				if commandOptions.graphShowAll || noteLimit <= 0 || noteLimit > len(c.TopAuthority) {
					noteLimit = len(c.TopAuthority)
				}
				if noteLimit > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "    top files (by authority):\n")
					for j := 0; j < noteLimit; j++ {
						p := c.TopAuthority[j]
						n := analysis.Nodes[p.Path]
						tagStr := ""
						if len(n.Tags) > 0 {
							tagStr = fmt.Sprintf(" tags:%s", strings.Join(n.Tags, ","))
						}
						kind := n.Kind
						if kind == "" {
							kind = "file"
						}
						fmt.Fprintf(cmd.OutOrStdout(), "      %d) %s [%s] auth=%.4f hub=%.4f in=%d out=%d%s\n", j+1, p.Path, kind, p.Authority, p.Hub, n.Inbound, n.Outbound, tagStr)
					}
					if !commandOptions.graphShowAll && noteLimit < len(c.TopAuthority) {
						fmt.Fprintf(cmd.OutOrStdout(), "      ... (%d more)\n", len(c.TopAuthority)-noteLimit)
					}
				}
				if len(c.Bridges) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "    bridges: %s\n", strings.Join(c.Bridges, ", "))
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if commandOptions.graphTimings {
				printTimings(cmd, analysis.Timings.ToMillis())
			}
			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) colorCommunity(id string) string {
	if commandOptions.graphNoColor {
		return id
	}
	return "\033[36m" + id + "\033[0m"
}

func (commandOptions *graphInvocationOptions) registerGraphCommunitiesCommands(graphCmd, graphCommunitiesCmd *cobra.Command) {
	graphCmd.AddCommand(graphCommunitiesCmd)
}
