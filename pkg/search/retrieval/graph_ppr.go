package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type graphDocEdgeStore interface {
	GraphDocEdgesForPaths(ctx context.Context, paths []string, limit int, includeCalls bool) ([]semdb.GraphDocEdge, error)
	GraphDocEdgesWithConfidenceForPathsLimit(ctx context.Context, paths []string, limit int) ([]semdb.GraphDocEdge, error)
	IntelAnchorByID(ctx context.Context, anchorID string) (codeanchor.IntelAnchor, bool, error)
}

type GraphFactsProvider interface {
	GraphFacts(ctx context.Context, req noderead.GraphFactsRequest) (noderead.GraphFactsResult, error)
}

type graphNodeMeta struct {
	Endpoint noderead.GraphEndpoint
}

type graphSeedNodes map[string]graphNodeMeta

type edgeKey struct {
	src  string
	dst  string
	kind string
}

// GraphDiffuser is a pluggable diffusion algorithm over a doc graph.
type GraphDiffuser interface {
	Diffuse(adjacency map[string]map[string]float64, seeds map[string]float64) map[string]float64
}

// PPRDiffuser implements GraphDiffuser via graphalg.PersonalizedPageRank.
type PPRDiffuser struct {
	Options graphalg.PPROptions
}

func (d PPRDiffuser) Diffuse(adjacency map[string]map[string]float64, seeds map[string]float64) map[string]float64 {
	return graphalg.PersonalizedPageRank(adjacency, seeds, d.Options)
}

// GraphPPROptions controls GraphPPRRetriever behavior.
type GraphPPROptions struct {
	// Depth controls how many BFS hops to include in the induced subgraph.
	Depth int
	// PerNodeEdgeLimit caps edges fetched per expanded node.
	PerNodeEdgeLimit int
	// MaxNodes caps the number of nodes included in the induced subgraph.
	MaxNodes int
	// MaxEdges caps the number of edges included in the induced subgraph.
	MaxEdges int
	// ReturnLimit caps the number of candidates returned.
	ReturnLimit int
	// MinScore drops candidates below this normalized score (0..1).
	MinScore float64
	// IncludeCalls includes code->code call edges in the subgraph (can be noisy).
	IncludeCalls bool
	// IncludeSeedNodes emits seed doc nodes (notes/files) as candidates. This is useful
	// when seeds come from anchors/chunks and the owning file/note itself did not hit
	// in the first-stage retrieval.
	IncludeSeedNodes bool
	// MaxNotes and MaxFiles enforce a simple type budget to keep diffusion results
	// balanced for context packing. Zero means "no cap" for that type.
	MaxNotes int
	MaxFiles int
}

func defaultGraphPPROptions() GraphPPROptions {
	return GraphPPROptions{
		Depth:            2,
		PerNodeEdgeLimit: 250,
		MaxNodes:         800,
		MaxEdges:         4000,
		ReturnLimit:      25,
		MinScore:         0.05,
		IncludeCalls:     true,
		IncludeSeedNodes: true,
		// Leave MaxNotes/MaxFiles uncapped by default. The caller-facing packer
		// should decide how much breadth to include based on budget.
		MaxNotes: 0,
		MaxFiles: 0,
	}
}

// GraphPPRRetriever expands doc candidates by running query-time diffusion (personalized PageRank)
// on a bounded induced doc graph constructed from the intel store.
//
// It is designed to run in the AutoExpand stage: spec.Seeds are treated as restart seeds,
// and additional note/file candidates are surfaced even if they did not match text/vector retrieval.
type GraphPPRRetriever struct {
	Store      graphDocEdgeStore
	GraphFacts GraphFactsProvider
	// VaultPath is used only for stable title formatting; it can be empty.
	VaultPath string

	Options  GraphPPROptions
	Diffuser GraphDiffuser
}

func (r *GraphPPRRetriever) Name() string { return "graph_ppr" }

func (r *GraphPPRRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil && r.GraphFacts == nil || len(spec.Seeds) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := r.Options
	if opts == (GraphPPROptions{}) {
		opts = defaultGraphPPROptions()
	}
	if opts.Depth <= 0 {
		return nil, nil
	}
	if opts.PerNodeEdgeLimit <= 0 {
		opts.PerNodeEdgeLimit = 250
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 800
	}
	if opts.MaxEdges <= 0 {
		opts.MaxEdges = 4000
	}
	if opts.ReturnLimit <= 0 {
		opts.ReturnLimit = 25
	}
	if opts.MinScore < 0 {
		opts.MinScore = 0
	}

	diffuser := r.Diffuser
	if diffuser == nil {
		diffuser = PPRDiffuser{Options: graphalg.DefaultPPROptions()}
	}

	seedNodes, err := seedDocNodes(ctx, r.Store, spec.Seeds)
	if err != nil {
		return nil, err
	}
	if len(seedNodes) == 0 {
		return nil, nil
	}

	seedCode := make(map[string]struct{})
	for n, meta := range seedNodes {
		if meta.Endpoint.Kind == noderead.GraphEndpointCode {
			seedCode[n] = struct{}{}
		}
	}

	startedGraph := time.Now()
	graphFacts := r.GraphFacts
	if graphFacts == nil && r.Store != nil {
		graphFacts = docGraphFactsAdapter{store: r.Store}
	}
	adj, nodes, edges, noteLinkedToSeedCode, nodeMeta, err := buildInducedDocGraph(ctx, graphFacts, seedNodes, seedCode, opts)
	addTiming(ctx, search.TimingEvent{
		Name:     "graph_ppr.induced_graph",
		Kind:     "retriever",
		Started:  startedGraph,
		Duration: time.Since(startedGraph),
		Status:   timingStatus(err),
		Err:      timingErr(err),
	})
	if err != nil {
		return nil, err
	}
	if len(adj) == 0 {
		return nil, nil
	}

	seeds := make(map[string]float64, len(seedNodes))
	for n := range seedNodes {
		seeds[n] = 1
	}

	startedDiffuse := time.Now()
	ranks := diffuser.Diffuse(adj, seeds)
	addTiming(ctx, search.TimingEvent{
		Name:     "graph_ppr.diffuse",
		Kind:     "retriever",
		Started:  startedDiffuse,
		Duration: time.Since(startedDiffuse),
		Status:   "ok",
	})
	if len(ranks) == 0 {
		return nil, nil
	}

	type scored struct {
		node  string
		score float64
		adj   float64
		seed  bool
	}
	items := make([]scored, 0, len(ranks))
	var max float64
	var maxAdj float64
	for node, v := range ranks {
		_, isSeed := seedNodes[node]
		if isSeed && !opts.IncludeSeedNodes {
			continue
		}
		if isSeed && opts.IncludeSeedNodes {
			// Seed nodes are normalized against the non-seed max so they don't squash
			// the rest of the neighborhood. We'll treat them as norm=1.0 later.
			items = append(items, scored{node: node, score: v, adj: v, seed: true})
			continue
		}
		if v > max {
			max = v
		}
		items = append(items, scored{node: node, score: v, adj: v, seed: false})
	}
	if max <= 0 || len(items) == 0 {
		return nil, nil
	}

	// Prefer notes that are directly linked to seeded code via code anchors (mentions edges)
	// or coderefs. This helps surface "docs for code" even when the text query doesn't hit them.
	//
	// We scale the bonus relative to the max base score so it behaves consistently across graphs.
	const linkedNoteBonusFrac = 0.10
	for i := range items {
		if items[i].seed {
			continue
		}
		if !isGraphNoteNode(nodeMeta[items[i].node]) {
			continue
		}
		if _, ok := noteLinkedToSeedCode[items[i].node]; ok {
			items[i].adj = items[i].score + linkedNoteBonusFrac*max
		}
		if items[i].adj > maxAdj {
			maxAdj = items[i].adj
		}
	}
	if maxAdj <= 0 {
		maxAdj = max
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].adj != items[j].adj {
			return items[i].adj > items[j].adj
		}
		return items[i].node < items[j].node
	})
	if len(items) > opts.ReturnLimit {
		items = items[:opts.ReturnLimit]
	}

	out := make([]search.Candidate, 0, len(items))
	notesAdded := 0
	filesAdded := 0
	for _, it := range items {
		norm := it.adj / maxAdj
		if it.seed {
			norm = 1.0
		}
		if norm < opts.MinScore {
			continue
		}

		// Enforce simple type quotas (if configured).
		if isGraphNoteNode(nodeMeta[it.node]) {
			if opts.MaxNotes > 0 && notesAdded >= opts.MaxNotes {
				continue
			}
		} else {
			if opts.MaxFiles > 0 && filesAdded >= opts.MaxFiles {
				continue
			}
		}

		linked := false
		if _, ok := noteLinkedToSeedCode[it.node]; ok {
			linked = true
		}
		c, ok := candidateForGraphNode(it.node, nodeMeta[it.node], norm, nodes, edges, it.seed, linked)
		if !ok {
			continue
		}
		out = append(out, c)
		if c.Type == "note" {
			notesAdded++
		} else if c.Type == "code" {
			filesAdded++
		}
	}
	return out, nil
}

func seedDocNodes(ctx context.Context, store graphDocEdgeStore, seeds []knowledge.Handle) (graphSeedNodes, error) {
	out := make(graphSeedNodes)
	for _, s := range seeds {
		switch s.Kind {
		case knowledge.KindNote, knowledge.KindNoteChunk:
			p := search.NormalizeLocalityPath(s.ID)
			if p != "" {
				out[p] = graphNodeMeta{Endpoint: noderead.GraphEndpoint{
					ID:            string(noderead.GraphEndpointNote) + ":" + p,
					Kind:          noderead.GraphEndpointNote,
					Path:          p,
					NotePath:      p,
					SourceLocator: p,
				}}
			}
		case knowledge.KindNodeChunk:
			ref, ok := nodeChunkSeedRef(s)
			if !ok {
				continue
			}
			endpoint := graphPPRSeedEndpoint(ref)
			if endpoint.ID != "" {
				out[endpoint.ID] = graphNodeMeta{Endpoint: endpoint}
			}
		case knowledge.KindFile:
			p := search.NormalizeLocalityPath(s.ID)
			if p != "" {
				out[p] = graphNodeMeta{Endpoint: noderead.GraphEndpoint{
					ID:            string(noderead.GraphEndpointCode) + ":" + p,
					Kind:          noderead.GraphEndpointCode,
					Path:          p,
					SourceLocator: p,
				}}
			}
		case knowledge.KindAnchor:
			if store == nil {
				continue
			}
			a, ok, err := store.IntelAnchorByID(ctx, strings.TrimSpace(s.ID))
			if err != nil {
				return nil, err
			}
			if ok && strings.TrimSpace(a.Path) != "" {
				p := search.NormalizeLocalityPath(a.Path)
				if p != "" {
					out[p] = graphNodeMeta{Endpoint: noderead.GraphEndpoint{
						ID:            string(noderead.GraphEndpointCode) + ":" + p,
						Kind:          noderead.GraphEndpointCode,
						Path:          p,
						SourceLocator: p,
					}}
				}
			}
		}
	}
	return out, nil
}

func buildInducedDocGraph(ctx context.Context, graphFacts GraphFactsProvider, seeds graphSeedNodes, seedCode map[string]struct{}, opts GraphPPROptions) (adj map[string]map[string]float64, nodeCount int, edgeCount int, noteLinkedToSeedCode map[string]struct{}, nodeMeta map[string]graphNodeMeta, err error) {
	type void = struct{}
	seen := make(map[string]void, len(seeds))
	frontier := make([]string, 0, len(seeds))
	nodeMeta = make(map[string]graphNodeMeta)
	for n, meta := range seeds {
		seen[n] = void{}
		frontier = append(frontier, n)
		if meta.Endpoint.ID != "" {
			nodeMeta[n] = meta
		}
	}
	sort.Strings(frontier)

	adj = make(map[string]map[string]float64)
	noteLinkedToSeedCode = make(map[string]struct{})
	ensure := func(n string) {
		if n == "" {
			return
		}
		if _, ok := adj[n]; !ok {
			adj[n] = make(map[string]float64)
		}
	}
	add := func(src, dst, kind string, weight float64, confidence float64) {
		if src == "" || dst == "" || src == dst {
			return
		}
		w := edgeWeight(kind, weight)
		if confidence > 0 {
			w *= confidence
		}
		if w <= 0 {
			return
		}
		ensure(src)
		ensure(dst)
		adj[src][dst] += w
		adj[dst][src] += w // treat as undirected relevance graph
		edgeCount++
	}

	// BFS expansion with caps.
	for depth := 0; depth < opts.Depth; depth++ {
		if len(frontier) == 0 {
			break
		}
		if len(seen) >= opts.MaxNodes || edgeCount >= opts.MaxEdges {
			break
		}
		next := make([]string, 0, len(frontier)*2)
		frontierSet := make(map[string]struct{}, len(frontier))
		for _, node := range frontier {
			frontierSet[node] = void{}
			ensure(node)
		}

		edgesLimit := opts.PerNodeEdgeLimit * len(frontier)
		if edgesLimit <= 0 {
			edgesLimit = 500
		}
		if remaining := opts.MaxEdges - edgeCount; remaining > 0 && edgesLimit > remaining {
			edgesLimit = remaining
		}

		startedEdges := time.Now()
		paths, endpointRefs := graphPPRFrontierRequest(frontier, nodeMeta)
		facts, err := graphFacts.GraphFacts(ctx, noderead.GraphFactsRequest{
			Sources:         endpointRefs,
			Paths:           paths,
			EdgeLimit:       edgesLimit,
			NodeLimit:       opts.MaxNodes,
			IncludeOntology: true,
			IncludeDocLinks: true,
			IncludeCode:     true,
			IncludeCalls:    opts.IncludeCalls,
			IncludeEmbedded: true,
		})
		elapsed := time.Since(startedEdges)
		if elapsed > 25*time.Millisecond {
			addTiming(ctx, search.TimingEvent{
				Name:     "graph_ppr.edges:batch",
				Kind:     "sql",
				Started:  startedEdges,
				Duration: elapsed,
				Status:   timingStatus(err),
				Err:      timingErr(err),
			})
		}
		if err != nil {
			return nil, 0, 0, nil, nil, fmt.Errorf("graph edges for frontier: %w", err)
		}
		perNodeCounts := make(map[string]int, len(frontier))

		recordEdge := func(src, dst, kind string, weight float64, confidence float64) {
			add(src, dst, kind, weight, confidence)
			// Track note<->seedCode connectivity for prioritization.
			if codeanchor.IsDocDomainEdge(kind) {
				if isGraphNoteNode(nodeMeta[src]) {
					if _, ok := seedCode[dst]; ok {
						noteLinkedToSeedCode[src] = struct{}{}
					}
				}
				if isGraphNoteNode(nodeMeta[dst]) {
					if _, ok := seedCode[src]; ok {
						noteLinkedToSeedCode[dst] = struct{}{}
					}
				}
			}
		}

		for _, e := range facts.Edges {
			if len(seen) >= opts.MaxNodes || edgeCount >= opts.MaxEdges {
				break
			}
			kind := strings.ToLower(strings.TrimSpace(e.Kind))
			src := graphPPRFactNodeKey(e.Source, e.SourcePath, e.SourceKind)
			dst := graphPPRFactNodeKey(e.Target, e.TargetPath, e.TargetKind)
			if src == "" || dst == "" {
				continue
			}
			nodeMeta[src] = graphNodeMeta{Endpoint: noderead.GraphEndpoint{
				ID:            e.Source,
				Ref:           e.SourceRef,
				Kind:          e.SourceKind,
				Path:          e.SourcePath,
				NotePath:      e.SourceRef.NotePath,
				NodeID:        e.SourceNodeID,
				TypeName:      e.SourceTypeName,
				SourceLocator: e.SourceLocator,
			}}
			nodeMeta[dst] = graphNodeMeta{Endpoint: noderead.GraphEndpoint{
				ID:            e.Target,
				Ref:           e.TargetRef,
				Kind:          e.TargetKind,
				Path:          e.TargetPath,
				NotePath:      e.TargetRef.NotePath,
				NodeID:        e.TargetNodeID,
				TypeName:      e.TargetTypeName,
				SourceLocator: e.TargetLocator,
			}}

			added := false
			if _, ok := frontierSet[src]; ok {
				if perNodeCounts[src] < opts.PerNodeEdgeLimit {
					perNodeCounts[src]++
					recordEdge(src, dst, kind, e.Weight, e.Confidence)
					added = true
				}
			}
			if _, ok := frontierSet[dst]; ok {
				if perNodeCounts[dst] < opts.PerNodeEdgeLimit {
					perNodeCounts[dst]++
					recordEdge(src, dst, kind, e.Weight, e.Confidence)
					added = true
				}
			}
			if !added {
				continue
			}

			// Expand on both endpoints (since we treat graph as undirected for reachability).
			for _, n := range []string{src, dst} {
				if n == "" {
					continue
				}
				if _, ok := seen[n]; ok {
					continue
				}
				if len(seen) >= opts.MaxNodes {
					break
				}
				seen[n] = void{}
				next = append(next, n)
			}
		}
		sort.Strings(next)
		frontier = dedupeSorted(next)
	}

	nodeCount = len(adj)
	return adj, nodeCount, edgeCount, noteLinkedToSeedCode, nodeMeta, nil
}

type docGraphFactsAdapter struct {
	store graphDocEdgeStore
}

func (a docGraphFactsAdapter) GraphFacts(ctx context.Context, req noderead.GraphFactsRequest) (noderead.GraphFactsResult, error) {
	if a.store == nil {
		return noderead.GraphFactsResult{}, nil
	}
	// Legacy fallback: this adapter can answer only persisted document/file graph
	// requests. Embedded and section endpoint sources require noderead.Scope.
	docPaths := docOnlyGraphFactPaths(req)
	if len(docPaths) == 0 {
		return noderead.GraphFactsResult{}, nil
	}
	limit := req.EdgeLimit
	if limit <= 0 {
		limit = 500
	}
	edges, err := a.store.GraphDocEdgesForPaths(ctx, docPaths, limit, req.IncludeCalls)
	if err != nil {
		return noderead.GraphFactsResult{}, err
	}
	confEdges, err := a.store.GraphDocEdgesWithConfidenceForPathsLimit(ctx, docPaths, limit)
	if err != nil {
		return noderead.GraphFactsResult{}, err
	}
	confidenceByEdge := make(map[string]float64, len(confEdges))
	for _, edge := range confEdges {
		key := edgeKey{
			src:  normalizeGraphDocNode(edge.SrcPath),
			dst:  normalizeGraphDocNode(edge.DstPath),
			kind: strings.ToLower(strings.TrimSpace(edge.Kind)),
		}
		confidenceByEdge[key.String()] = graphalg.ConfidenceValue(edge.Confidence, edge.ConfidenceScore)
	}
	out := noderead.GraphFactsResult{}
	nodes := map[string]noderead.GraphEndpoint{}
	addNode := func(kind noderead.GraphEndpointKind, path string) noderead.GraphEndpoint {
		id := string(kind) + ":" + path
		node := noderead.GraphEndpoint{ID: id, Kind: kind, Path: path, SourceLocator: path}
		if kind == noderead.GraphEndpointNote {
			node.NotePath = path
		}
		nodes[id] = node
		return node
	}
	for _, edge := range edges {
		srcKind, srcPath, dstKind, dstPath := graphPPRDocEndpoints(edge)
		if srcPath == "" || dstPath == "" {
			continue
		}
		if !req.IncludeCode && (srcKind == noderead.GraphEndpointCode || dstKind == noderead.GraphEndpointCode) {
			continue
		}
		if !req.IncludeCalls && strings.EqualFold(edge.Kind, "calls") {
			continue
		}
		src := addNode(srcKind, srcPath)
		dst := addNode(dstKind, dstPath)
		confidence := confidenceByEdge[edgeKey{src: srcPath, dst: dstPath, kind: strings.ToLower(strings.TrimSpace(edge.Kind))}.String()]
		if confidence <= 0 {
			confidence = 1
		}
		weight := float64(edge.Weight)
		if weight <= 0 {
			weight = 1
		}
		out.Edges = append(out.Edges, noderead.GraphFactEdge{
			Source:        src.ID,
			Target:        dst.ID,
			SourceRef:     src.Ref,
			TargetRef:     dst.Ref,
			SourcePath:    srcPath,
			TargetPath:    dstPath,
			SourceKind:    srcKind,
			TargetKind:    dstKind,
			Kind:          edge.Kind,
			Weight:        weight,
			Confidence:    confidence,
			SourceLocator: srcPath,
			TargetLocator: dstPath,
		})
	}
	for _, node := range nodes {
		out.Nodes = append(out.Nodes, node)
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	return out, nil
}

func docOnlyGraphFactPaths(req noderead.GraphFactsRequest) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(req.Paths)+len(req.Sources))
	add := func(raw string) {
		path := normalizeGraphDocNode(raw)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	for _, path := range req.Paths {
		add(path)
	}
	for _, ref := range req.Sources {
		if ref.Kind == ontology.NodeKindEmbedded || ref.Kind == ontology.NodeKindSection || strings.TrimSpace(ref.NodeID) != "" || strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(ref.Structural) != "" {
			continue
		}
		add(ref.NotePath)
	}
	return out
}

func nodeChunkSeedRef(seed knowledge.Handle) (ontology.NodeRef, bool) {
	nodeID := strings.TrimSpace(seed.ID)
	if nodeID == "" {
		return ontology.NodeRef{}, false
	}
	notePath := ""
	if len(seed.Fragments) > 0 {
		notePath = search.NormalizeLocalityPath(seed.Fragments[0])
	}
	if notePath == "" {
		return ontology.NodeRef{}, false
	}
	return ontology.NodeRef{
		NotePath: notePath,
		NodeID:   nodeID,
		Kind:     ontology.NodeKindEmbedded,
	}, true
}

func graphPPRSeedEndpoint(ref ontology.NodeRef) noderead.GraphEndpoint {
	kind := graphPPRNodeRefEndpointKind(ref.Kind)
	key := strings.TrimSpace(ref.NodeID)
	if kind == noderead.GraphEndpointNote {
		key = strings.TrimSpace(ref.NotePath)
	}
	if key == "" && strings.TrimSpace(ref.Fragment) != "" {
		key = strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	}
	if key == "" {
		return noderead.GraphEndpoint{}
	}
	locator := strings.TrimSpace(ref.NotePath)
	if ref.Fragment != "" && ref.NotePath != "" {
		locator = ref.NotePath + "#" + strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	}
	return noderead.GraphEndpoint{
		ID:            string(kind) + ":" + key,
		Ref:           ref,
		Kind:          kind,
		Path:          ref.NotePath,
		NotePath:      ref.NotePath,
		NodeID:        ref.NodeID,
		TypeName:      ref.TypeName,
		SourceLocator: locator,
	}
}

func graphPPRNodeRefEndpointKind(kind ontology.NodeKind) noderead.GraphEndpointKind {
	switch kind {
	case ontology.NodeKindSection:
		return noderead.GraphEndpointSection
	case ontology.NodeKindNote:
		return noderead.GraphEndpointNote
	default:
		return noderead.GraphEndpointEmbedded
	}
}

func graphPPRFrontierRequest(frontier []string, nodeMeta map[string]graphNodeMeta) ([]string, []ontology.NodeRef) {
	paths := make([]string, 0, len(frontier))
	refs := make([]ontology.NodeRef, 0)
	for _, node := range frontier {
		if meta, ok := nodeMeta[node]; ok {
			switch meta.Endpoint.Kind {
			case noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
				ref := meta.Endpoint.Ref
				if ref.Kind == "" {
					ref.Kind = ontology.NodeKind(strings.ToUpper(string(meta.Endpoint.Kind)))
				}
				if ref.NodeID == "" {
					ref.NodeID = meta.Endpoint.NodeID
				}
				if ref.NotePath == "" {
					ref.NotePath = meta.Endpoint.NotePath
				}
				if !ref.IsZero() {
					refs = append(refs, ref)
					continue
				}
			case noderead.GraphEndpointNote, noderead.GraphEndpointCode:
				if meta.Endpoint.Path != "" {
					paths = append(paths, meta.Endpoint.Path)
					continue
				}
			}
		}
		if kind, id, ok := strings.Cut(node, ":"); ok && (kind == string(noderead.GraphEndpointEmbedded) || kind == string(noderead.GraphEndpointSection)) {
			refs = append(refs, ontology.NodeRef{NodeID: id, Kind: ontology.NodeKind(strings.ToUpper(kind))})
			continue
		}
		paths = append(paths, node)
	}
	return paths, refs
}

func graphPPRFactNodeKey(endpointID, path string, kind noderead.GraphEndpointKind) string {
	switch kind {
	case noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
		return endpointID
	case noderead.GraphEndpointNote, noderead.GraphEndpointCode:
		return normalizeGraphDocNode(path)
	default:
		return ""
	}
}

func graphPPRDocEndpoints(edge semdb.GraphDocEdge) (noderead.GraphEndpointKind, string, noderead.GraphEndpointKind, string) {
	src := normalizeGraphDocNode(edge.SrcPath)
	dst := normalizeGraphDocNode(edge.DstPath)
	if src == "" || dst == "" {
		return "", "", "", ""
	}
	switch strings.ToLower(strings.TrimSpace(edge.Kind)) {
	case "wikilink", "mdlink":
		return noderead.GraphEndpointNote, src, noderead.GraphEndpointNote, dst
	case "mentions":
		return noderead.GraphEndpointNote, src, noderead.GraphEndpointCode, dst
	case "coderef":
		return noderead.GraphEndpointCode, src, noderead.GraphEndpointNote, dst
	default:
		return noderead.GraphEndpointCode, src, noderead.GraphEndpointCode, dst
	}
}

func edgeWeight(kind string, weight float64) float64 {
	if weight <= 0 {
		weight = 1
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case string(noderead.GraphEdgeKindOntology):
		return 1.4 * weight
	case string(noderead.GraphEdgeKindEmbeds):
		return 0.6 * weight
	}
	return codeanchor.EdgeWeight(kind, int(weight))
}

func (k edgeKey) String() string {
	return k.src + "\x00" + k.dst + "\x00" + k.kind
}

func normalizeGraphDocNode(path string) string {
	return search.NormalizeLocalityPath(path)
}

func dedupeSorted(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := items[:0]
	var last string
	for _, it := range items {
		if it == "" || it == last {
			continue
		}
		out = append(out, it)
		last = it
	}
	return out
}

func candidateForGraphNode(node string, meta graphNodeMeta, score float64, nodeCount int, edgeCount int, isSeed bool, linkedToSeedCode bool) (search.Candidate, bool) {
	node = strings.TrimSpace(node)
	if node == "" {
		return search.Candidate{}, false
	}
	details := map[string]string{
		"nodes": fmt.Sprintf("%d", nodeCount),
		"edges": fmt.Sprintf("%d", edgeCount),
	}
	if isSeed {
		details["seed"] = "true"
	}
	if linkedToSeedCode {
		details["linked_to_seed_code"] = "true"
	}
	if meta.Endpoint.Kind == noderead.GraphEndpointEmbedded || meta.Endpoint.Kind == noderead.GraphEndpointSection {
		notePath := firstNonEmpty(meta.Endpoint.NotePath, meta.Endpoint.Ref.NotePath, meta.Endpoint.Path)
		nodeID := firstNonEmpty(meta.Endpoint.NodeID, meta.Endpoint.Ref.NodeID, strings.TrimPrefix(node, string(meta.Endpoint.Kind)+":"))
		if nodeID == "" || notePath == "" {
			return search.Candidate{}, false
		}
		refJSON := ""
		if !meta.Endpoint.Ref.IsZero() {
			if data, err := json.Marshal(meta.Endpoint.Ref); err == nil {
				refJSON = string(data)
			}
		}
		h := knowledge.NodeChunkHandle(nodeID, notePath, "graph", 0)
		owner := knowledge.NoteHandle(notePath)
		return search.Candidate{
			Handle: h,
			Owner:  owner,
			Evidence: []search.Evidence{{
				Type:     "graph_ppr",
				RawScore: score,
				Source:   "graph_ppr",
				Details:  details,
			}},
			Type:        "note",
			NoteID:      notePath,
			Path:        notePath,
			Title:       strings.TrimSuffix(pathpkg.Base(notePath), pathpkg.Ext(notePath)),
			ChunkIndex:  -1,
			NodeID:      nodeID,
			NodeRefJSON: refJSON,
			NodeKind:    string(meta.Endpoint.Kind),
			NodeType:    meta.Endpoint.TypeName,
		}, true
	}

	switch meta.Endpoint.Kind {
	case noderead.GraphEndpointNote:
		h := knowledge.NoteHandle(node)
		return search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     "graph_ppr",
				RawScore: score,
				Source:   "graph_ppr",
				Details:  details,
			}},
			Type:       "note",
			NoteID:     node,
			Path:       node,
			Title:      strings.TrimSuffix(pathpkg.Base(node), pathpkg.Ext(node)),
			ChunkIndex: -1,
		}, true
	case noderead.GraphEndpointCode:
		h := knowledge.FileHandle(node)
		return search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     "graph_ppr",
				RawScore: score,
				Source:   "graph_ppr",
				Details:  details,
			}},
			Type:       "code",
			Path:       node,
			Title:      pathpkg.Base(node),
			ChunkIndex: -1,
		}, true
	default:
		return search.Candidate{}, false
	}
}

func isGraphNoteNode(meta graphNodeMeta) bool {
	switch meta.Endpoint.Kind {
	case noderead.GraphEndpointNote, noderead.GraphEndpointEmbedded, noderead.GraphEndpointSection:
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
