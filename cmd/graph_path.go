package cmd

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <from> <to>",
		Short: "Find the shortest path between two notes or code files",
		Args:  cobra.ExactArgs(2),
		RunE:  commandOptions.runGraphPath,
	}
}

func (commandOptions *graphInvocationOptions) registerGraphPathCommands(graphPathCmd, graphCmd *cobra.Command) {
	graphPathCmd.Flags().IntVar(&commandOptions.graphPathMaxHops, "max-hops", 8, "maximum path length")
	graphCmd.AddCommand(graphPathCmd)
}

func (commandOptions *graphInvocationOptions) runGraphPath(cmd *cobra.Command, args []string) error {
	fromInput := args[0]
	toInput := args[1]

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
	vaultPath, err := vault.Path()
	if err != nil {
		return err
	}

	store, cleanup, err := openOptionalIntelStoreForAnalysis(vaultPath)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	if store == nil {
		return fmt.Errorf("no code index found at %s; run `rzm index` first", vaultPath)
	}

	vaultDef, err := vault.Definition()
	if err != nil {
		return err
	}

	facts, err := noderead.NewService(vaultDef, &obsidian.Note{}, store, nil).
		NewScope(noderead.ScopeOptions{}).
		GraphFacts(cmd.Context(), noderead.GraphFactsRequest{
			NodeLimit:       1_000_000,
			EdgeLimit:       1_000_000,
			IncludeOntology: true,
			IncludeDocLinks: true,
			IncludeCode:     true,
			IncludeEmbedded: true,
		})
	if err != nil {
		return fmt.Errorf("loading graph facts: %w", err)
	}
	minEdges, allPaths, canonicalPath := graphFactsPathInputs(facts)

	// Resolve fuzzy inputs.
	fromPath, err := algorithms.ResolvePathFuzzy(allPaths, fromInput)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", err)
		return nil
	}
	toPath, err := algorithms.ResolvePathFuzzy(allPaths, toInput)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", err)
		return nil
	}

	fromPath = canonicalGraphPath(canonicalPath, fromPath)
	toPath = canonicalGraphPath(canonicalPath, toPath)

	result, err := algorithms.ShortestPath(minEdges, fromPath, toPath, commandOptions.graphPathMaxHops)
	if err != nil {
		// No path found — friendly output.
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No path found between %q and %q within %d hops.\n", fromInput, toInput, commandOptions.graphPathMaxHops)
		return nil
	}

	printGraphPath(cmd, result)
	return nil
}

func graphFactsPathInputs(facts noderead.GraphFactsResult) ([]algorithms.GraphDocEdgeMinimal, []string, map[string]string) {
	pathSet := make(map[string]struct{}, len(facts.Nodes)*2+len(facts.Edges)*2)
	canonical := make(map[string]string, len(facts.Nodes)*4)
	for _, node := range facts.Nodes {
		if node.Kind != noderead.GraphEndpointNote {
			addGraphFactAlias(pathSet, canonical, node.ID, node.ID)
		}
		addGraphFactAlias(pathSet, canonical, node.NotePath, node.ID)
		addGraphFactAlias(pathSet, canonical, node.Path, node.ID)
		addGraphFactAlias(pathSet, canonical, node.SourceLocator, node.ID)
	}
	minEdges := make([]algorithms.GraphDocEdgeMinimal, 0, len(facts.Edges))
	for _, edge := range facts.Edges {
		src := edge.Source
		dst := edge.Target
		if src == "" || dst == "" || src == dst {
			continue
		}
		addGraphFactEndpoint(pathSet, canonical, src)
		addGraphFactEndpoint(pathSet, canonical, dst)
		minEdges = append(minEdges, algorithms.GraphDocEdgeMinimal{
			SrcPath:         src,
			DstPath:         dst,
			Kind:            graphFactEdgeKind(edge),
			Confidence:      graphFactConfidenceLabel(edge),
			ConfidenceScore: edge.Confidence,
		})
	}
	allPaths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		allPaths = append(allPaths, p)
	}
	return minEdges, allPaths, canonical
}

func addGraphFactAlias(pathSet map[string]struct{}, canonical map[string]string, path, endpoint string) {
	path = strings.TrimSpace(path)
	if path != "" {
		pathSet[path] = struct{}{}
		canonical[path] = strings.TrimSpace(endpoint)
	}
}

func addGraphFactEndpoint(pathSet map[string]struct{}, canonical map[string]string, endpoint string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return
	}
	canonical[endpoint] = endpoint
	if !strings.HasPrefix(endpoint, string(noderead.GraphEndpointNote)+":") {
		pathSet[endpoint] = struct{}{}
	}
}

func canonicalGraphPath(canonical map[string]string, path string) string {
	if endpoint := canonical[strings.TrimSpace(path)]; endpoint != "" {
		return endpoint
	}
	return path
}

func graphFactEdgeKind(edge noderead.GraphFactEdge) string {
	if edge.RelationName != "" && edge.Kind == "ontology" {
		return edge.RelationName
	}
	return edge.Kind
}

func graphFactConfidenceLabel(edge noderead.GraphFactEdge) string {
	if edge.Confidence > 0 && edge.Confidence < 1 {
		return semdb.EdgeConfidenceInferred
	}
	return semdb.EdgeConfidenceExtracted
}

func printGraphPath(cmd *cobra.Command, result *algorithms.PathResult) {
	w := cmd.OutOrStdout()
	if result.Hops == 0 {
		fmt.Fprintf(w, "Same node: %s\n", result.From)
		return
	}
	fmt.Fprintf(w, "Shortest path (%d %s):\n\n", result.Hops, hopWord(result.Hops))
	fmt.Fprintf(w, "  %s\n", result.From)
	for _, hop := range result.Path {
		edgeLabel := formatEdgeLabel(hop)
		fmt.Fprintf(w, "    ──[%s]──▶\n", edgeLabel)
		fmt.Fprintf(w, "  %s\n", hop.ToPath)
	}
	fmt.Fprintln(w)
}

func hopWord(n int) string {
	if n == 1 {
		return "hop"
	}
	return "hops"
}

func formatEdgeLabel(hop algorithms.PathHop) string {
	var parts []string
	parts = append(parts, hop.EdgeKind)
	if hop.Confidence != "" {
		if hop.Confidence == semdb.EdgeConfidenceInferred && hop.ConfidenceScore > 0 {
			parts = append(parts, fmt.Sprintf("inferred %.2f", hop.ConfidenceScore))
		} else {
			parts = append(parts, hop.Confidence)
		}
	}
	return strings.Join(parts, ", ")
}
