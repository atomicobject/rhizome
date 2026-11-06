package cmd

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

// Graph CLI command equivalent to web UI /api/graph/* endpoints.
// Used for testing and debugging graph performance from the command line.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"os"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *graphInvocationOptions) makeGraphWebCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "web [global|local|expand]",
		Short: "Query the doc+code graph (equivalent to web UI endpoints)",
		Long: `Query the unified doc+code graph, mirroring the web UI endpoints.

Modes:
  global  - Full vault graph (all nodes and edges)
  local   - Local graph centered on a specific file (--path)
  expand  - Expand a module/directory (--module)

Examples:
  rzm graph web global                    # Full vault graph
  rzm graph web global --timings          # With timing breakdown
  rzm graph web local --path docs/foo.md  # Graph around a file
  rzm graph web expand --module pkg/app   # Expand a directory
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			mode := "global"
			if len(args) > 0 {
				mode = args[0]
			}

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
			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}
			resolvedConfig1, configErr1 := repoconfig.Load(vaultPath)
			codeCfg, err := resolvedConfig1.Code, configErr1
			if err != nil {
				codeCfg = codeanchor.DefaultConfig(vaultPath)
			}
			codeCfg.IndexPath = repoconfig.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)

			tStore := time.Now()
			store, cleanup, err := requireIntelStore(vaultPath, codeCfg, indexstore.ReadOnly)
			if err != nil {
				return err
			}
			storeOpenTime := time.Since(tStore)
			defer cleanup()

			vaultDef, err := vault.Definition()
			if err != nil {
				return err
			}
			ignoreMatcher := ignore.LoadUnifiedMatcher(vaultPath, vaultDef.Excludes)
			schema, _ := ontoschema.LoadSchema(vaultPath)

			gb := &graphBuilder{
				store:         store,
				vaultDef:      vaultDef,
				schema:        schema,
				ignoreMatcher: ignoreMatcher,
			}

			start := time.Now()
			var resp graphpresentation.GraphResponse
			var buildErr error

			switch mode {
			case "global":
				resp, buildErr = gb.buildGlobalGraph(ctx, commandOptions.graphLimit, commandOptions.graphWebDepth)
			case "local":
				if commandOptions.graphWebPath == "" {
					return fmt.Errorf("--path is required for local mode")
				}
				resp, buildErr = gb.buildLocalGraph(ctx, commandOptions.graphWebPath, commandOptions.graphLimit, commandOptions.graphWebDepth)
			case "expand":
				if commandOptions.graphWebModule == "" {
					return fmt.Errorf("--module is required for expand mode")
				}
				resp, buildErr = gb.buildModuleGraph(ctx, commandOptions.graphWebModule, commandOptions.graphLimit)
			default:
				return fmt.Errorf("unknown mode: %s (use global, local, or expand)", mode)
			}

			elapsed := time.Since(start)

			if buildErr != nil {
				return buildErr
			}

			if commandOptions.graphWebJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Mode: %s\n", mode)
			fmt.Fprintf(cmd.OutOrStdout(), "Nodes: %d\n", len(resp.Nodes))
			fmt.Fprintf(cmd.OutOrStdout(), "Edges: %d\n", len(resp.Edges))
			fmt.Fprintf(cmd.OutOrStdout(), "Truncated: %v\n", resp.Truncated)
			if resp.CenterID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Center: %s\n", resp.CenterID)
			}

			kindCounts := map[string]int{}
			for _, e := range resp.Edges {
				kindCounts[e.Kind]++
			}
			if len(kindCounts) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\nEdge kinds:\n")
				for kind, count := range kindCounts {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d\n", kind, count)
				}
			}

			nodeKindCounts := map[string]int{}
			for _, n := range resp.Nodes {
				nodeKindCounts[n.Kind]++
			}
			if len(nodeKindCounts) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\nNode kinds:\n")
				for kind, count := range nodeKindCounts {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d\n", kind, count)
				}
			}

			if commandOptions.graphTimings {
				fmt.Fprintf(cmd.OutOrStdout(), "\nTiming: %s\n", elapsed)
				fmt.Fprintf(cmd.OutOrStdout(), "  store open:     %6s\n", storeOpenTime.Round(time.Millisecond))
			}

			return nil
		},
	}
}

func (commandOptions *graphInvocationOptions) registerGraphWebCommands(graphCmd, graphWebCmd *cobra.Command) {
	graphCmd.AddCommand(graphWebCmd)
	graphWebCmd.Flags().StringVar(&commandOptions.graphWebPath, "path", "", "file path for local graph mode")
	graphWebCmd.Flags().StringVar(&commandOptions.graphWebModule, "module", "", "module/directory path for expand mode")
	graphWebCmd.Flags().IntVar(&commandOptions.graphWebDepth, "depth", 2, "module depth for grouping (default 2)")
	graphWebCmd.Flags().BoolVar(&commandOptions.graphWebJSON, "json", false, "output full graph as JSON")
}

// graphBuilder is a minimal implementation of the web.Server graph methods
// without requiring the full web server infrastructure.
type graphBuilder struct {
	store         *semdb.Store
	vaultDef      obsidian.VaultDefinition
	schema        *ontoschema.Schema
	ignoreMatcher *ignore.Matcher
}

func (gb *graphBuilder) buildGlobalGraph(ctx context.Context, limit, depth int) (graphpresentation.GraphResponse, error) {
	if limit <= 0 {
		limit = 2000
	}
	if depth <= 0 {
		depth = 2
	}
	return gb.buildNodeReadGraph(ctx, noderead.GraphRequest{
		Profile: noderead.GraphProfileCodeAware,
		Limit:   limit,
	}, depth, "")
}

func (gb *graphBuilder) buildLocalGraph(ctx context.Context, target string, limit, depth int) (graphpresentation.GraphResponse, error) {
	if limit <= 0 {
		limit = 200
	}
	if depth <= 0 {
		depth = 1
	}

	centerKind, centerPath, err := gb.resolveLocalGraphTarget(ctx, target)
	if err != nil {
		return graphpresentation.GraphResponse{}, err
	}
	return gb.buildNodeReadGraph(ctx, noderead.GraphRequest{
		Sources: []ontology.NodeRef{{NotePath: centerPath, Kind: ontology.NodeKind(centerKind)}},
		Profile: noderead.GraphProfileCodeAware,
		Limit:   limit,
	}, depth, nodeID(centerKind, centerPath))
}

// resolveLocalGraphTarget reads the indexed path kind. A graph command cannot
// infer note ownership from a filename extension because the stored graph is
// the source of truth for its projected identities.
func (gb *graphBuilder) resolveLocalGraphTarget(ctx context.Context, target string) (string, string, error) {
	if gb == nil || gb.store == nil {
		return "", "", fmt.Errorf("graph index is unavailable")
	}
	vaultPaths, err := paths.NewVaultPaths(gb.vaultDef.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return "", "", fmt.Errorf("invalid vault path %q", gb.vaultDef.BasePath())
	}
	path, err := vaultPaths.RelStrict(target)
	if err != nil || path == "" {
		return "", "", fmt.Errorf("invalid path %q", target)
	}
	scores, err := gb.store.GraphDocScoresByPaths(ctx, []string{path.String()})
	if err != nil {
		return "", "", fmt.Errorf("read persisted graph catalog: %w", err)
	}
	score, ok := scores[path.String()]
	if !ok {
		return "", "", fmt.Errorf("path %q is not in the persisted graph catalog", path)
	}
	switch score.DocType {
	case "note", "code":
		return score.DocType, score.DocPath, nil
	default:
		return "", "", fmt.Errorf("path %q has unsupported persisted graph kind %q", path, score.DocType)
	}
}

func (gb *graphBuilder) buildModuleGraph(ctx context.Context, module string, limit int) (graphpresentation.GraphResponse, error) {
	if limit <= 0 {
		limit = 600
	}
	return gb.buildNodeReadGraph(ctx, noderead.GraphRequest{
		PathPrefixes: []string{module},
		Profile:      noderead.GraphProfileCodeAware,
		Limit:        limit,
	}, 2, "")
}

func (gb *graphBuilder) buildNodeReadGraph(ctx context.Context, req noderead.GraphRequest, depth int, centerID string) (graphpresentation.GraphResponse, error) {
	graph, err := noderead.NewService(gb.vaultDef, &obsidian.Note{}, gb.store, gb.schema).NewScope(noderead.ScopeOptions{}).Graph(ctx, req)
	if err != nil {
		return graphpresentation.GraphResponse{}, err
	}
	return gb.presentNodeReadGraph(graph, req.Limit, depth, centerID), nil
}

func (gb *graphBuilder) presentNodeReadGraph(graph noderead.GraphResult, limit, depth int, centerID string) graphpresentation.GraphResponse {
	nodes := make(map[string]graphpresentation.GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if gb.ignoreMatcher != nil && node.NotePath != "" && gb.ignoreMatcher.IsIgnored(node.NotePath, false) {
			continue
		}
		projected := graphpresentation.ProjectNode(node)
		projected.Path = firstGraphCLIString(node.Path, node.NotePath)
		projected.Label = firstGraphCLIString(node.Label, paths.NoteTitle(projected.Path))
		projected.Lang = nodeLang(projected.Kind)
		projected.Module = moduleKey(firstGraphCLIString(node.NotePath, projected.Path), depth)
		nodes[node.ID] = projected
	}
	edges := graphpresentation.ProjectEdges(nodes, graph.Edges)
	nodesOut, edgesOut, truncated := graphpresentation.Collapse(nodes, edges, limit, func(path string) string { return moduleKey(path, 2) })
	return graphpresentation.GraphResponse{Nodes: nodesOut, Edges: edgesOut, Truncated: truncated, CenterID: centerID, Diagnostics: graph.Diagnostics}
}

func firstGraphCLIString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nodeID(kind, path string) string {
	return kind + ":" + path
}

func nodeLang(kind string) string {
	if kind == "note" || kind == "embedded" || kind == "section" {
		return "markdown"
	}
	return ""
}

func moduleKey(p string, depth int) string {
	if depth <= 0 {
		depth = 2
	}
	parts := []string{}
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			parts = append(parts, p[start:i])
			start = i + 1
			if len(parts) >= depth {
				break
			}
		}
	}
	if len(parts) == 0 {
		return "root"
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "/" + parts[i]
	}
	return result
}
