package cmd

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/paths"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphCommunityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "community <id>",
		Short: "Show details for a specific community",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			communityArg := args[0]

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
				IncludeTags:     commandOptions.graphCommunityIncludeTags,
				ExcludePatterns: commandOptions.graphExcludePatterns,
				IncludePatterns: commandOptions.graphIncludePatterns,
				IntelStore:      intelStore,
				WikilinkOptions: markdownsyntax.DefaultWikilinkOptions,
			})
			if err != nil {
				return err
			}

			var target *vaultgraph.CommunitySummary
			for i := range analysis.Communities {
				if analysis.Communities[i].ID == communityArg {
					target = &analysis.Communities[i]
					break
				}
			}

			if target == nil {
				normalized := communityArg
				if filepath.IsAbs(communityArg) {
					vaultPaths, _ := paths.NewVaultPaths(vaultPath)
					if rel, err := vaultPaths.RelNotePathStrict(communityArg); err == nil && rel.String() != "" {
						normalized = rel.String()
					}
				}
				notePath := normalizeDocInput(vaultPath, normalized, true)
				codePath := normalizeDocInput(vaultPath, normalized, false)
				var resolved string
				if _, ok := analysis.Nodes[notePath]; ok {
					resolved = notePath
				} else if _, ok := analysis.Nodes[codePath]; ok {
					resolved = codePath
				}
				if resolved == "" {
					return fmt.Errorf("community %s not found and file %s not in graph (use vault-relative paths, ensure it exists, and check include/exclude filters)", communityArg, communityArg)
				}

				lookup := vaultgraph.CommunityMembershipLookup(analysis.Communities)
				target = lookup[resolved]
				if target == nil {
					return fmt.Errorf("file %s is not assigned to a community under current filters", communityArg)
				}
			}

			commandOptions.printCommunityDetail(cmd, target, analysis, selectedVault)
			if commandOptions.graphTimings {
				printTimings(cmd, analysis.Timings.ToMillis())
			}
			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) printCommunityDetail(cmd *cobra.Command, target *vaultgraph.CommunitySummary, analysis *vaultgraph.GraphAnalysis, selectedVault string) {
	edgeCount := vaultgraph.CommunityInternalEdges(target, analysis.Nodes)

	fmt.Fprintf(cmd.OutOrStdout(), "Community %s (size %d) in vault %q\n", commandOptions.colorCommunity(target.ID), len(target.Nodes), selectedVault)
	if target.Anchor != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  anchor: %s\n", target.Anchor)
	}
	if target.Density > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  density: %.3f\n", target.Density)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  edges (internal): %d\n", edgeCount)
	if len(target.TopTags) > 0 {
		var tags []string
		for _, tt := range target.TopTags {
			tags = append(tags, fmt.Sprintf("%s(%d)", tt.Tag, tt.Count))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  tags: %s\n", strings.Join(tags, ", "))
	}
	if len(target.Bridges) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  bridges: %s\n", strings.Join(target.Bridges, ", "))
	}

	limit := commandOptions.graphLimit
	if commandOptions.graphShowAll || limit <= 0 || limit > len(target.Nodes) {
		limit = len(target.Nodes)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nMembers (sorted by authority):\n")
	members := rankCommunityMembers(target.Nodes, analysis)
	for i := 0; i < limit; i++ {
		m := members[i]
		tagStr := ""
		if len(m.tags) > 0 && commandOptions.graphCommunityIncludeTags {
			tagStr = fmt.Sprintf(" tags:%s", strings.Join(m.tags, ","))
		}
		kind := m.kind
		if kind == "" {
			kind = "file"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %d) %s [%s] auth=%.4f hub=%.4f in=%d out=%d%s\n", i+1, m.path, kind, m.authority, m.hub, m.in, m.out, tagStr)
		if commandOptions.graphCommunityIncludeNeighbors {
			fmt.Fprintf(cmd.OutOrStdout(), "      neighbors: %s\n", strings.Join(m.neighbors, ", "))
		}
	}
	if !commandOptions.graphShowAll && len(members) > limit {
		fmt.Fprintf(cmd.OutOrStdout(), "  ... (%d more)\n", len(members)-limit)
	}
}

func rankCommunityMembers(members []string, analysis *vaultgraph.GraphAnalysis) []struct {
	path      string
	kind      string
	hub       float64
	authority float64
	in        int
	out       int
	tags      []string
	neighbors []string
} {
	var ranked []struct {
		path      string
		kind      string
		hub       float64
		authority float64
		in        int
		out       int
		tags      []string
		neighbors []string
	}
	for _, p := range members {
		n := analysis.Nodes[p]
		ranked = append(ranked, struct {
			path      string
			kind      string
			hub       float64
			authority float64
			in        int
			out       int
			tags      []string
			neighbors []string
		}{
			path:      p,
			kind:      n.Kind,
			hub:       n.Hub,
			authority: n.Authority,
			in:        n.Inbound,
			out:       n.Outbound,
			tags:      n.Tags,
			neighbors: n.Neighbors,
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].authority == ranked[j].authority {
			return ranked[i].path < ranked[j].path
		}
		return ranked[i].authority > ranked[j].authority
	})
	return ranked
}

func (commandOptions *graphInvocationOptions) registerGraphCommunityCommands(graphCommunityCmd, graphCmd *cobra.Command) {
	graphCommunityCmd.Flags().BoolVar(&commandOptions.graphCommunityIncludeNeighbors, "neighbors", false, "show neighbor lists for members")
	graphCommunityCmd.Flags().BoolVar(&commandOptions.graphCommunityIncludeTags, "tags", false, "show tags for members")

	graphCmd.AddCommand(graphCommunityCmd)
}

func normalizeDocInput(vaultPath, raw string, isNote bool) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	if isNote {
		rel, _, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, clean)
		if err != nil || rel == "" {
			return ""
		}
		return rel.String()
	}
	rel, _, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, clean)
	if err != nil || rel == "" {
		return ""
	}
	return rel.String()
}
