package cmd

import (
	"context"
	"fmt"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	graphSurprisesLimit    int
	graphSurprisesMinScore float64
)

var graphSurprisesCmd = &cobra.Command{
	Use:   "surprises",
	Short: "Surface unexpected connections in the knowledge graph",
	Long: `Analyze the graph to surface non-obvious, cross-community, or cross-type edges
that are most likely to represent surprising or valuable hidden connections.

Edges are scored by unexpectedness: inferred/ambiguous confidence, community
boundaries, code↔note type crossings, and peripheral nodes reaching hubs.`,
	RunE: runGraphSurprises,
}

func init() {
	graphSurprisesCmd.Flags().IntVar(&graphSurprisesLimit, "limit", 10, "maximum number of surprising connections to show")
	graphSurprisesCmd.Flags().Float64Var(&graphSurprisesMinScore, "min-score", 2.0, "minimum surprise score to include")
	graphCmd.AddCommand(graphSurprisesCmd)
}

func runGraphSurprises(cmd *cobra.Command, args []string) error {
	selectedVault := vaultName
	if selectedVault == "" {
		vault := &obsidian.Vault{}
		defaultName, err := vault.DefaultName()
		if err != nil {
			return err
		}
		selectedVault = defaultName
	}

	vault := obsidian.Vault{Name: selectedVault}
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

	if intelStore == nil {
		return fmt.Errorf("no code index found for vault %q; run `rzm index` first", selectedVault)
	}

	conns, err := loadGraphSurprises(cmd.Context(), intelStore, graphSurprisesLimit, graphSurprisesMinScore)
	if err != nil {
		return fmt.Errorf("loading graph surprises: %w", err)
	}

	if len(conns) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No surprising connections found (try --min-score 0 to see all).\n")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Surprising connections (scored by unexpectedness):")
	for _, c := range conns {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "  %.1f  %s ←[%s]→ %s\n", c.Score, c.SrcPath, c.EdgeKind, c.DstPath)
		if len(c.Reasons) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "       %s\n", strings.Join(c.Reasons, "; "))
		}
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// loadGraphSurprises loads edges and scores from the store and runs surprise scoring.
func loadGraphSurprises(ctx context.Context, store *semdb.Store, topN int, minScore float64) ([]graphalg.SurprisingConnection, error) {
	facts, err := noderead.NewService(obsidian.VaultDefinition{}, &obsidian.Note{}, store, nil).
		NewScope(ctx, noderead.ScopeOptions{}).
		GraphFacts(ctx, noderead.GraphFactsRequest{
			NodeLimit:       1_000_000,
			EdgeLimit:       1_000_000,
			IncludeOntology: true,
			IncludeDocLinks: true,
			IncludeCode:     true,
			IncludeEmbedded: true,
		})
	if err != nil {
		return nil, err
	}

	docScores, err := store.GraphDocScores(ctx)
	if err != nil {
		return nil, err
	}

	edges := make([]graphalg.GraphDocEdgeForSurprise, 0, len(facts.Edges))
	for _, e := range facts.Edges {
		src := e.SourcePath
		dst := e.TargetPath
		if src == "" {
			src = e.SourceRef.NotePath
		}
		if dst == "" {
			dst = e.TargetRef.NotePath
		}
		if src == "" || dst == "" || src == dst {
			continue
		}
		edges = append(edges, graphalg.GraphDocEdgeForSurprise{
			SrcPath:         src,
			DstPath:         dst,
			Kind:            graphFactEdgeKind(e),
			Confidence:      graphFactConfidenceLabel(e),
			ConfidenceScore: e.Confidence,
		})
	}

	scores := make(map[string]graphalg.GraphDocScoreMinimal, len(docScores))
	for _, sc := range docScores {
		scores[sc.DocPath] = graphalg.GraphDocScoreMinimal{
			DocPath:   sc.DocPath,
			DocType:   sc.DocType,
			Community: sc.Community,
			Inbound:   sc.Inbound,
			Outbound:  sc.Outbound,
			Authority: sc.Authority,
		}
	}

	return graphalg.FindSurprisingConnections(edges, scores, topN, minScore), nil
}
