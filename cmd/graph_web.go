package cmd

// Graph CLI command equivalent to web UI /api/graph/* endpoints.
// Used for testing and debugging graph performance from the command line.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	graphWebPath   string
	graphWebModule string
	graphWebDepth  int
	graphWebJSON   bool
)

var graphWebCmd = &cobra.Command{
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

		// Resolve vault
		selectedVault := vaultName
		if selectedVault == "" {
			v := &obsidian.Vault{}
			name, err := v.DefaultName()
			if err != nil {
				return err
			}
			selectedVault = name
		}
		vault := obsidian.Vault{Name: selectedVault}
		vaultPath, err := vault.Path()
		if err != nil {
			return err
		}

		// Load code config
		codeCfg, err := obsidian.LoadCodeConfig(vaultPath)
		if err != nil {
			codeCfg = codeanchor.DefaultConfig(vaultPath)
		}
		codeCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)

		// Open intel store
		tStore := time.Now()
		store, cleanup, err := requireIntelStore(vaultPath, codeCfg)
		if err != nil {
			return err
		}
		storeOpenTime := time.Since(tStore)
		defer cleanup()

		// Load vault definition and ignore matcher
		vaultDef, err := vault.Definition()
		if err != nil {
			return err
		}
		ignoreMatcher := ignore.LoadUnifiedMatcher(vaultPath, vaultDef.Excludes)
		schema, _ := ontology.LoadSchema(vaultPath)

		// Create a minimal graph builder
		gb := &graphBuilder{
			store:         store,
			vaultDef:      vaultDef,
			schema:        schema,
			ignoreMatcher: ignoreMatcher,
		}

		start := time.Now()
		var resp web.GraphResponse
		var buildErr error

		switch mode {
		case "global":
			resp, buildErr = gb.buildGlobalGraph(ctx, graphLimit, graphWebDepth)
		case "local":
			if graphWebPath == "" {
				return fmt.Errorf("--path is required for local mode")
			}
			resp, buildErr = gb.buildLocalGraph(ctx, graphWebPath, graphLimit, graphWebDepth)
		case "expand":
			if graphWebModule == "" {
				return fmt.Errorf("--module is required for expand mode")
			}
			resp, buildErr = gb.buildModuleGraph(ctx, graphWebModule, graphLimit)
		default:
			return fmt.Errorf("unknown mode: %s (use global, local, or expand)", mode)
		}

		elapsed := time.Since(start)

		if buildErr != nil {
			return buildErr
		}

		if graphWebJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		}

		// Summary output
		fmt.Fprintf(cmd.OutOrStdout(), "Mode: %s\n", mode)
		fmt.Fprintf(cmd.OutOrStdout(), "Nodes: %d\n", len(resp.Nodes))
		fmt.Fprintf(cmd.OutOrStdout(), "Edges: %d\n", len(resp.Edges))
		fmt.Fprintf(cmd.OutOrStdout(), "Truncated: %v\n", resp.Truncated)
		if resp.CenterID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Center: %s\n", resp.CenterID)
		}

		// Edge breakdown by kind
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

		// Node breakdown by kind
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

		if graphTimings {
			fmt.Fprintf(cmd.OutOrStdout(), "\nTiming: %s\n", elapsed)
			fmt.Fprintf(cmd.OutOrStdout(), "  store open:     %6s\n", storeOpenTime.Round(time.Millisecond))
		}

		return nil
	},
}

func init() {
	graphCmd.AddCommand(graphWebCmd)
	graphWebCmd.Flags().StringVar(&graphWebPath, "path", "", "file path for local graph mode")
	graphWebCmd.Flags().StringVar(&graphWebModule, "module", "", "module/directory path for expand mode")
	graphWebCmd.Flags().IntVar(&graphWebDepth, "depth", 2, "module depth for grouping (default 2)")
	graphWebCmd.Flags().BoolVar(&graphWebJSON, "json", false, "output full graph as JSON")
}

// graphBuilder is a minimal implementation of the web.Server graph methods
// without requiring the full web server infrastructure.
type graphBuilder struct {
	store         *semdb.Store
	vaultDef      obsidian.VaultDefinition
	schema        *ontology.Schema
	ignoreMatcher *ignore.Matcher
}

func (gb *graphBuilder) buildGlobalGraph(ctx context.Context, limit, depth int) (web.GraphResponse, error) {
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

func (gb *graphBuilder) buildLocalGraph(ctx context.Context, target string, limit, depth int) (web.GraphResponse, error) {
	if limit <= 0 {
		limit = 200
	}
	if depth <= 0 {
		depth = 1
	}

	centerKind, centerPath, err := gb.resolveLocalGraphTarget(ctx, target)
	if err != nil {
		return web.GraphResponse{}, err
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

func (gb *graphBuilder) buildModuleGraph(ctx context.Context, module string, limit int) (web.GraphResponse, error) {
	if limit <= 0 {
		limit = 600
	}
	return gb.buildNodeReadGraph(ctx, noderead.GraphRequest{
		PathPrefixes: []string{module},
		Profile:      noderead.GraphProfileCodeAware,
		Limit:        limit,
	}, 2, "")
}

func (gb *graphBuilder) buildNodeReadGraph(ctx context.Context, req noderead.GraphRequest, depth int, centerID string) (web.GraphResponse, error) {
	graph, err := noderead.NewService(gb.vaultDef, &obsidian.Note{}, gb.store, gb.schema).NewScope(ctx, noderead.ScopeOptions{}).Graph(ctx, req)
	if err != nil {
		return web.GraphResponse{}, err
	}
	nodes := make(map[string]web.GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if gb.ignoreMatcher != nil && node.NotePath != "" && gb.ignoreMatcher.IsIgnored(node.NotePath, false) {
			continue
		}
		kind := string(node.Kind)
		path := node.Path
		if path == "" {
			path = node.NotePath
		}
		var nodeRef *ontology.NodeRef
		if !node.Ref.IsZero() {
			ref := node.Ref
			nodeRef = &ref
		}
		nodes[node.ID] = web.GraphNode{
			ID:            node.ID,
			Path:          path,
			NotePath:      node.NotePath,
			NodeID:        node.NodeID,
			NodeRef:       nodeRef,
			Label:         firstGraphCLIString(node.Label, graphWebTitleFromPath(path)),
			Kind:          kind,
			Lang:          nodeLang(kind),
			Module:        moduleKey(firstGraphCLIString(node.NotePath, path), depth),
			SourceLocator: node.SourceLocator,
			ResolvedType:  node.TypeName,
		}
	}
	edges := make([]web.GraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if _, ok := nodes[edge.Source]; !ok {
			continue
		}
		if _, ok := nodes[edge.Target]; !ok {
			continue
		}
		edges = append(edges, web.GraphEdge{
			Source:        edge.Source,
			Target:        edge.Target,
			Kind:          edge.Kind,
			Weight:        edge.Weight,
			RelationName:  edge.RelationName,
			RelationLabel: edge.RelationLabel,
			Provenance:    edge.Provenance,
			Structural:    edge.Structural,
		})
	}
	nodesOut, edgesOut, truncated := collapseGraphCLI(nodes, edges, req.Limit)
	return web.GraphResponse{Nodes: nodesOut, Edges: edgesOut, Truncated: truncated, CenterID: centerID, Diagnostics: graph.Diagnostics}, nil
}

func firstGraphCLIString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Helper functions (duplicated from web/graph.go for CLI independence)

func nodeID(kind, path string) string {
	return kind + ":" + path
}

func nodeLang(kind string) string {
	if kind == "note" || kind == "embedded" || kind == "section" {
		return "markdown"
	}
	return ""
}

func graphWebTitleFromPath(p string) string {
	base := filepath.Base(p)
	if extension := filepath.Ext(base); extension != "" {
		base = strings.TrimSuffix(base, extension)
	}
	return base
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

const moduleCollapseThreshold = 250

func collapseGraphCLI(nodes map[string]web.GraphNode, edges []web.GraphEdge, limit int) ([]web.GraphNode, []web.GraphEdge, bool) {
	if len(nodes) <= limit {
		nodeList := make([]web.GraphNode, 0, len(nodes))
		for _, n := range nodes {
			nodeList = append(nodeList, n)
		}
		return nodeList, edges, false
	}

	byModule := map[string][]web.GraphNode{}
	for _, n := range nodes {
		byModule[n.Module] = append(byModule[n.Module], n)
	}
	collapsed := map[string]web.GraphNode{}
	for module, members := range byModule {
		if len(members) < moduleCollapseThreshold {
			for _, m := range members {
				collapsed[m.ID] = m
			}
			continue
		}
		collapsed[nodeID("module", module)] = web.GraphNode{
			ID:         nodeID("module", module),
			Path:       module,
			Label:      module,
			Kind:       "module",
			Collapsed:  true,
			ChildCount: len(members),
		}
	}

	edgeMap := map[string]*web.GraphEdge{}
	for _, e := range edges {
		src := collapseNodeIDCLI(collapsed, e.Source)
		dst := collapseNodeIDCLI(collapsed, e.Target)
		key := src + "|" + dst + "|" + e.Kind
		if agg, ok := edgeMap[key]; ok {
			agg.Weight += e.Weight
		} else {
			edgeMap[key] = &web.GraphEdge{Source: src, Target: dst, Kind: e.Kind, Weight: e.Weight}
		}
	}

	nodeList := make([]web.GraphNode, 0, len(collapsed))
	for _, n := range collapsed {
		nodeList = append(nodeList, n)
	}
	edgeList := make([]web.GraphEdge, 0, len(edgeMap))
	for _, e := range edgeMap {
		edgeList = append(edgeList, *e)
	}
	return nodeList, edgeList, true
}

func collapseNodeIDCLI(nodes map[string]web.GraphNode, id string) string {
	if _, ok := nodes[id]; ok {
		return id
	}
	if len(id) > 7 && id[:7] == "module:" {
		return id
	}
	var path string
	if len(id) > 5 && id[:5] == "note:" {
		path = id[5:]
	} else if len(id) > 5 && id[:5] == "code:" {
		path = id[5:]
	} else {
		return id
	}
	module := moduleKey(path, 2)
	modID := nodeID("module", module)
	if _, ok := nodes[modID]; ok {
		return modID
	}
	return id
}
