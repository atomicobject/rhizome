package retrieval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

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
			search.RecordTiming(ctx, search.TimingEvent{
				Name:     "graph_ppr.edges:batch",
				Kind:     "sql",
				Started:  startedEdges,
				Duration: elapsed,
				Status:   search.TimingStatus(err),
				Err:      search.TimingError(err),
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
