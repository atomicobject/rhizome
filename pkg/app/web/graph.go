package web

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/graphdb"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	defaultModuleDepth       = 2
	defaultLocalNodeLimit    = 200
	defaultGlobalGraphLimit  = 2000
	defaultModuleExpandLimit = 600
	moduleCollapseThreshold  = 250
)

func (s *Server) graphNeedsIndexResponse() GraphResponse {
	return GraphResponse{
		Nodes:      []GraphNode{},
		Edges:      []GraphEdge{},
		NeedsIndex: true,
	}
}

func (s *Server) cachedGraphResponse(ctx context.Context, scope string, global bool, build func(ctx context.Context) (GraphResponse, error)) (GraphResponse, error) {
	if s == nil || s.graphCache == nil || s.runtime == nil {
		return build(ctx)
	}
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return build(ctx)
	}
	fingerprint, err := intelStore.GraphWebFingerprint(ctx)
	if err != nil {
		// A cancelled caller must not fall back to its own unshared build.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return GraphResponse{}, ctxErr
		}
		return build(ctx)
	}
	if fingerprint == "" {
		return build(ctx)
	}
	key := fmt.Sprintf(
		"%s|store=%p|ignore=%p|fingerprint=%s",
		scope,
		intelStore,
		s.runtime.IgnoreMatcher(),
		fingerprint,
	)
	return s.graphCache.getOrBuild(ctx, key, global, build)
}

func (s *Server) ensureGraphScoresAsync() {
	if s == nil || s.graphCache == nil {
		return
	}
	s.graphScoresBackfill.Do(func() {
		s.graphCache.spawn(func(ctx context.Context) {
			if s.runtime == nil {
				return
			}
			intelStore := s.runtime.Intel()
			if intelStore == nil {
				return
			}
			_ = actions.EnsureDocScores(ctx, s.cfg.VaultDef, &obsidian.Note{}, intelStore, graphdb.DocScoresOptions{
				WikilinkOptions: obsidian.DefaultWikilinkOptions,
			})
			state, err := intelStore.AnchorScoresSummary(ctx)
			if err != nil || state.Count > 0 {
				return
			}
			anchorScores, err := graphdb.ComputeAnchorPageRank(ctx, intelStore)
			if err != nil {
				return
			}
			_ = intelStore.ReplaceAnchorScores(ctx, anchorScores)
		})
	})
}

// attachGraphTypes populates GraphNode.ResolvedType for note nodes when the
// vault has an ontology schema. This is a best-effort enrichment — if the
// schema is missing or the lookup fails, nodes are returned unchanged.
func (s *Server) attachGraphTypes(ctx context.Context, nodes map[string]GraphNode) {
	if s == nil || s.runtime == nil || len(nodes) == 0 {
		return
	}
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return
	}
	notePaths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == "note" && node.Path != "" {
			notePaths = append(notePaths, node.Path)
		}
	}
	if len(notePaths) == 0 {
		return
	}
	rows, err := intelStore.OntologyTypesByPaths(ctx, notePaths)
	if err != nil || len(rows) == 0 {
		return
	}
	for id, node := range nodes {
		if node.Kind != "note" || node.Path == "" {
			continue
		}
		if row, ok := rows[node.Path]; ok && row.TypeName != "" {
			node.ResolvedType = row.TypeName
			nodes[id] = node
		}
	}
}

func (s *Server) attachGraphScores(ctx context.Context, nodes map[string]GraphNode) {
	if s == nil || s.runtime == nil || len(nodes) == 0 {
		return
	}
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return
	}
	docScoresState, err := intelStore.GraphDocScoresSummary(ctx)
	if err != nil {
		return
	}
	anchorScoresState, err := intelStore.AnchorScoresSummary(ctx)
	if err != nil {
		return
	}
	if docScoresState.Count == 0 || anchorScoresState.Count == 0 {
		s.ensureGraphScoresAsync()
	}
	docPaths := make([]string, 0, len(nodes))
	seen := map[string]struct{}{}
	codePaths := make([]string, 0, len(nodes))
	nodeCandidates := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if node.Kind != "note" && node.Kind != "code" {
			continue
		}
		if node.Path == "" {
			continue
		}
		// Graph nodes carry their persisted typed path. Do not infer a path kind
		// from its extension while attaching optional score data.
		candidates := []string{node.Path}
		if node.Kind == "code" {
			abs := filepath.Join(s.cfg.VaultPath, filepath.FromSlash(node.Path))
			resolved := string(paths.ResolveSymlinks(abs))
			if filepath.IsAbs(abs) {
				candidates = append(candidates, filepath.ToSlash(abs))
			}
			if resolved != "" {
				candidates = append(candidates, filepath.ToSlash(resolved))
			}
		}
		for _, p := range candidates {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			docPaths = append(docPaths, p)
		}
		nodeCandidates[node.ID] = candidates
		if node.Kind == "code" {
			codePaths = append(codePaths, node.Path)
		}
	}
	if len(docPaths) == 0 {
		return
	}
	scores := map[string]semdb.GraphDocScore{}
	if docScoresState.Count > 0 {
		scores, err = intelStore.GraphDocScoresByPaths(ctx, docPaths)
		if err != nil {
			return
		}
	}
	var maxNoteAuthority float64
	for id, node := range nodes {
		if node.Kind != "note" && node.Kind != "code" {
			continue
		}
		candidates := nodeCandidates[id]
		var score semdb.GraphDocScore
		found := false
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if sc, ok := scores[candidate]; ok && sc.DocType == node.Kind {
				score = sc
				found = true
				break
			}
		}
		if !found {
			continue
		}
		node.Authority = score.Authority
		node.Hub = score.Hub
		node.Community = score.Community
		if node.Kind == "note" {
			node.Score = score.Authority
			if score.Authority > maxNoteAuthority {
				maxNoteAuthority = score.Authority
			}
		}
		nodes[id] = node
	}

	if len(codePaths) == 0 {
		return
	}
	pageRanks := map[string]float64{}
	if anchorScoresState.Count > 0 {
		pageRanks, err = intelStore.CodePageRankByPaths(ctx, codePaths)
		if err != nil {
			return
		}
	}
	var maxPageRank float64
	for _, pr := range pageRanks {
		if pr > maxPageRank {
			maxPageRank = pr
		}
	}
	scale := 1.0
	if maxPageRank > 0 && maxNoteAuthority > 0 {
		scale = maxNoteAuthority / maxPageRank
	}
	for id, node := range nodes {
		if node.Kind != "code" {
			continue
		}
		pr := pageRanks[node.Path]
		node.PageRank = pr
		node.Score = pr * scale
		nodes[id] = node
	}
}

func (s *Server) buildLocalGraphMode(ctx context.Context, target string, limit, depth int, notesOnly bool, diagnostics bool) (GraphResponse, error) {
	centerKind, centerPath, err := s.resolveLocalGraphPath(target)
	if err != nil {
		return GraphResponse{}, err
	}
	ref := ontology.NodeRef{NotePath: centerPath, Kind: ontology.NodeKind(centerKind)}
	return s.buildLocalGraphRefMode(ctx, ref, limit, depth, notesOnly, diagnostics)
}

func (s *Server) resolveLocalGraphPath(input string) (string, string, error) {
	path := filepath.ToSlash(strings.TrimSpace(input))
	path = strings.TrimPrefix(path, "/")
	canonical, err := paths.CleanRelPath(path)
	if err != nil || canonical == "" {
		return "", "", errors.New("invalid path")
	}
	path = canonical.String()
	if s != nil && s.catalog != nil {
		if provider, unsupported := s.catalog.unsupportedNoteProjection(path); unsupported {
			return "", "", fmt.Errorf("%s", unsupportedProjectionMessage(path, provider, "graph"))
		}
		kind, _ := s.classifyFile(path)
		if kind == "note" || kind == "code" {
			return kind, path, nil
		}
	}
	return "", "", fmt.Errorf("path %q is not a configured note or code file", input)
}

func (s *Server) buildLocalGraphRefMode(ctx context.Context, ref ontology.NodeRef, limit, depth int, notesOnly bool, diagnostics bool) (GraphResponse, error) {
	if limit <= 0 {
		limit = defaultLocalNodeLimit
	}
	if depth <= 0 {
		depth = 1
	}
	if ref.NotePath != "" && ref.Kind != ontology.NodeKindEmbedded && ref.Kind != ontology.NodeKindSection {
		ref.NotePath = s.normalizeUserGraphInput(ref.NotePath)
	}
	scope := fmt.Sprintf("local|ref=%s|node=%s|kind=%s|structural=%s|limit=%d|depth=%d|notesOnly=%t|diagnostics=%t", ref.String(), ref.NodeID, ref.Kind, ref.Structural, limit, depth, notesOnly, diagnostics)
	return s.cachedGraphResponse(ctx, scope, false, func(ctx context.Context) (GraphResponse, error) {
		return s.buildLocalGraphRefUncached(ctx, ref, limit, depth, notesOnly, diagnostics)
	})
}

func (s *Server) buildLocalGraphRefUncached(ctx context.Context, ref ontology.NodeRef, limit, depth int, notesOnly bool, diagnostics bool) (GraphResponse, error) {
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return s.graphNeedsIndexResponse(), nil
	}
	centerKind := string(ref.Kind)
	centerPath := ref.NotePath
	if centerKind == "" || centerKind == string(ontology.NodeKindSection) || centerKind == string(ontology.NodeKindEmbedded) {
		centerKind = "note"
	}
	if centerPath == "" {
		return GraphResponse{}, errors.New("invalid path")
	}
	fallbackCenterID := nodeID(centerKind, centerPath)
	profile := noderead.GraphProfileCodeAware
	if notesOnly {
		profile = noderead.GraphProfileNotesOnly
	}
	nodes, edgeList, graphDiagnostics, err := s.buildGraphWithNodeRead(ctx, noderead.GraphRequest{
		Sources:          []ontology.NodeRef{ref},
		Profile:          profile,
		Limit:            limit,
		IncludeCodeEdges: !notesOnly,
		Diagnostics:      diagnostics,
	}, depth)
	if err != nil {
		return GraphResponse{}, err
	}
	centerID := localGraphCenterID(nodes, ref, fallbackCenterID)
	if _, ok := nodes[centerID]; !ok && (!notesOnly || centerKind == "note") {
		nodes[centerID] = GraphNode{
			ID:     centerID,
			Path:   centerPath,
			Label:  titleFromPath(centerPath),
			Kind:   centerKind,
			Lang:   nodeLang(centerKind, centerPath),
			Module: moduleKey(centerPath, depth),
		}
	}

	resp := GraphResponse{CenterID: centerID, Diagnostics: graphDiagnostics}
	s.attachGraphScores(ctx, nodes)
	s.attachGraphTypes(ctx, nodes)
	resp.Nodes, resp.Edges, resp.Truncated = finalizeGraph(nodes, edgeList, limit, notesOnly)
	return resp, nil
}

func localGraphCenterID(nodes map[string]GraphNode, ref ontology.NodeRef, fallback string) string {
	if ref.Kind != ontology.NodeKindEmbedded {
		return fallback
	}
	for id, node := range nodes {
		if node.Kind != "embedded" {
			continue
		}
		if ref.NodeID != "" && (node.NodeID == ref.NodeID || id == nodeID("embedded", ref.NodeID)) {
			return id
		}
		if ref.Fragment != "" && node.SourceLocator == ref.NotePath+"#"+strings.TrimPrefix(ref.Fragment, "#") {
			return id
		}
		if node.NodeRef != nil {
			if ref.NodeID != "" && node.NodeRef.NodeID == ref.NodeID {
				return id
			}
			if ref.Fragment != "" && node.NodeRef.NotePath == ref.NotePath && strings.TrimPrefix(node.NodeRef.Fragment, "#") == strings.TrimPrefix(ref.Fragment, "#") {
				return id
			}
		}
	}
	if ref.NodeID != "" {
		return nodeID("embedded", ref.NodeID)
	}
	return fallback
}

func (s *Server) buildGlobalGraph(ctx context.Context, limit, depth int) (GraphResponse, error) {
	return s.buildGlobalGraphMode(ctx, limit, depth, false, false)
}

func (s *Server) buildGlobalGraphMode(ctx context.Context, limit, depth int, notesOnly bool, diagnostics bool) (GraphResponse, error) {
	if limit <= 0 {
		limit = defaultGlobalGraphLimit
	}
	if depth <= 0 {
		depth = defaultModuleDepth
	}
	scope := fmt.Sprintf("global|limit=%d|depth=%d|notesOnly=%t|diagnostics=%t", limit, depth, notesOnly, diagnostics)
	return s.cachedGraphResponse(ctx, scope, true, func(ctx context.Context) (GraphResponse, error) {
		return s.buildGlobalGraphUncached(ctx, limit, depth, notesOnly, diagnostics)
	})
}

func (s *Server) buildGlobalGraphUncached(ctx context.Context, limit, depth int, notesOnly bool, diagnostics bool) (GraphResponse, error) {
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return s.graphNeedsIndexResponse(), nil
	}
	profile := noderead.GraphProfileCodeAware
	if notesOnly {
		profile = noderead.GraphProfileNotesOnly
	}
	nodes, edgeList, graphDiagnostics, err := s.buildGraphWithNodeRead(ctx, noderead.GraphRequest{
		Profile:          profile,
		Limit:            limit,
		NodeLimit:        -1,
		EdgeLimit:        -1,
		IncludeCodeEdges: !notesOnly,
		Diagnostics:      diagnostics,
	}, depth)
	if err != nil {
		return GraphResponse{}, err
	}

	s.attachGraphScores(ctx, nodes)
	s.attachGraphTypes(ctx, nodes)
	nodesOut, edgesOut, truncated := finalizeGraph(nodes, edgeList, limit, notesOnly)
	// No edge limit needed since we already collapsed to ~2k file pairs

	resp := GraphResponse{Nodes: nodesOut, Edges: edgesOut, Truncated: truncated, Diagnostics: graphDiagnostics}
	return resp, nil
}

func (s *Server) buildModuleGraphMode(ctx context.Context, module string, limit int, diagnostics bool) (GraphResponse, error) {
	if limit <= 0 {
		limit = defaultModuleExpandLimit
	}
	module = strings.TrimPrefix(filepath.ToSlash(module), "/")
	scope := fmt.Sprintf("expand|module=%s|limit=%d|diagnostics=%t", module, limit, diagnostics)
	return s.cachedGraphResponse(ctx, scope, false, func(ctx context.Context) (GraphResponse, error) {
		return s.buildModuleGraphUncached(ctx, module, limit, diagnostics)
	})
}

func (s *Server) buildModuleGraphUncached(ctx context.Context, module string, limit int, diagnostics bool) (GraphResponse, error) {
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return s.graphNeedsIndexResponse(), nil
	}
	if module == "" {
		return GraphResponse{}, errors.New("invalid module")
	}
	nodes, edgeList, graphDiagnostics, err := s.buildGraphWithNodeRead(ctx, noderead.GraphRequest{
		PathPrefixes:     []string{module},
		Profile:          noderead.GraphProfileCodeAware,
		Limit:            limit,
		IncludeCodeEdges: true,
		Diagnostics:      diagnostics,
	}, defaultModuleDepth)
	if err != nil {
		return GraphResponse{}, err
	}

	resp := GraphResponse{Diagnostics: graphDiagnostics}
	s.attachGraphScores(ctx, nodes)
	s.attachGraphTypes(ctx, nodes)
	resp.Nodes, resp.Edges, resp.Truncated = collapseGraph(nodes, edgeList, limit)
	return resp, nil
}

func (s *Server) normalizeEdgePath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return ""
	}
	vaultPaths, _ := paths.NewVaultPaths(s.cfg.VaultPath)
	if vaultPaths.Root() == "" {
		return p
	}
	if rel, err := vaultPaths.RelStrict(p); err == nil && rel.String() != "" {
		return rel.String()
	}
	if filepath.IsAbs(p) {
		if resolved := paths.ResolveSymlinks(p); resolved != "" {
			return resolved.String()
		}
	}
	return p
}

func (s *Server) normalizeUserGraphInput(p string) string {
	clean := strings.TrimSpace(p)
	if clean == "" {
		return ""
	}
	return s.normalizeEdgePath(clean)
}

func collapseGraph(nodes map[string]GraphNode, edges []GraphEdge, limit int) ([]GraphNode, []GraphEdge, bool) {
	if len(nodes) <= limit {
		return sortedNodes(nodes), sortedEdgeSlice(edges), false
	}
	byModule := map[string][]GraphNode{}
	for _, n := range nodes {
		byModule[n.Module] = append(byModule[n.Module], n)
	}
	collapsed := map[string]GraphNode{}
	for module, members := range byModule {
		if len(members) < moduleCollapseThreshold {
			for _, m := range members {
				collapsed[m.ID] = m
			}
			continue
		}
		community := ""
		communityCounts := map[string]int{}
		maxAuthority := 0.0
		maxHub := 0.0
		for _, m := range members {
			if m.Community != "" {
				communityCounts[m.Community]++
			}
			if m.Authority > maxAuthority {
				maxAuthority = m.Authority
			}
			if m.Hub > maxHub {
				maxHub = m.Hub
			}
		}
		for id, count := range communityCounts {
			if community == "" || count > communityCounts[community] {
				community = id
			}
		}
		collapsed[nodeID("module", module)] = GraphNode{
			ID:         nodeID("module", module),
			Path:       module,
			Label:      module,
			Kind:       "module",
			Community:  community,
			Authority:  maxAuthority,
			Hub:        maxHub,
			Collapsed:  true,
			ChildCount: len(members),
		}
	}

	edgeMap := map[string]*GraphEdge{}
	for _, e := range edges {
		src := collapseNodeID(collapsed, e.Source)
		dst := collapseNodeID(collapsed, e.Target)
		key := src + "|" + dst + "|" + e.Kind
		if agg, ok := edgeMap[key]; ok {
			agg.Weight += e.Weight
		} else {
			edgeMap[key] = &GraphEdge{Source: src, Target: dst, Kind: e.Kind, Weight: e.Weight}
		}
	}

	return sortedNodes(collapsed), sortedEdgeList(edgeMap), true
}

func finalizeGraph(nodes map[string]GraphNode, edges []GraphEdge, limit int, notesOnly bool) ([]GraphNode, []GraphEdge, bool) {
	if notesOnly {
		// Ontology mode asks the backend for a notes-only graph up front. If we
		// collapse first, large note sets can disappear into module stand-ins and
		// the frontend has nothing meaningful left to render.
		return sortedNodes(nodes), sortedEdgeSlice(edges), false
	}
	return collapseGraph(nodes, edges, limit)
}

func collapseNodeID(nodes map[string]GraphNode, id string) string {
	if _, ok := nodes[id]; ok {
		return id
	}
	if strings.HasPrefix(id, "module:") {
		return id
	}
	var path string
	switch {
	case strings.HasPrefix(id, "note:"):
		path = strings.TrimPrefix(id, "note:")
	case strings.HasPrefix(id, "code:"):
		path = strings.TrimPrefix(id, "code:")
	default:
		return id
	}
	module := moduleKey(path, defaultModuleDepth)
	modID := nodeID("module", module)
	if _, ok := nodes[modID]; ok {
		return modID
	}
	return id
}

func sortedNodes(nodes map[string]GraphNode) []GraphNode {
	out := make([]GraphNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedEdgeList(edges map[string]*GraphEdge) []GraphEdge {
	out := make([]GraphEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, *e)
	}
	sortGraphEdges(out)
	return out
}

func sortedEdgeSlice(edges []GraphEdge) []GraphEdge {
	out := append([]GraphEdge(nil), edges...)
	sortGraphEdges(out)
	return out
}

func sortGraphEdges(edges []GraphEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Weight != edges[j].Weight {
			return edges[i].Weight > edges[j].Weight
		}
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Kind < edges[j].Kind
	})
}

func nodeID(kind, path string) string {
	return kind + ":" + path
}

func nodeLang(kind, path string) string {
	if kind == "note" {
		return "markdown"
	}
	return coderefs.DetectLanguage(path)
}

func moduleKey(p string, depth int) string {
	if depth <= 0 {
		depth = defaultModuleDepth
	}
	p = filepath.ToSlash(strings.TrimPrefix(p, "/"))
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return "root"
	}
	if len(parts) < depth {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[:depth], "/")
}
