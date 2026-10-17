package retrieval

import (
	"context"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

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
		confidenceByEdge[key.String()] = algorithms.ConfidenceValue(edge.Confidence, edge.ConfidenceScore)
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
		ref := meta.Endpoint.Ref
		if ref.NotePath == "" {
			ref.NotePath = notePath
		}
		if ref.NodeID == "" {
			ref.NodeID = nodeID
		}
		if ref.Kind == "" {
			ref.Kind = ontology.NodeKind(strings.ToUpper(string(meta.Endpoint.Kind)))
		}
		if ref.TypeName == "" {
			ref.TypeName = meta.Endpoint.TypeName
		}
		h := knowledge.NodeChunkHandle(nodeID, notePath, "graph", 0)
		owner := knowledge.NoteHandle(notePath)
		return search.Candidate{
			SourceRef: search.SourceRef{
				NoteID:     notePath,
				Path:       notePath,
				Title:      strings.TrimSuffix(pathpkg.Base(notePath), pathpkg.Ext(notePath)),
				ChunkIndex: -1,
				NodeID:     nodeID,
				NodeRef:    &ref,
			},
			Handle: h,
			Owner:  owner,
			Evidence: []search.Evidence{{
				Type:     "graph_ppr",
				RawScore: score,
				Source:   "graph_ppr",
				Details:  details,
			}},
			Type: "note",
		}, true
	}

	switch meta.Endpoint.Kind {
	case noderead.GraphEndpointNote:
		h := knowledge.NoteHandle(node)
		return search.Candidate{
			SourceRef: search.SourceRef{
				NoteID:     node,
				Path:       node,
				Title:      strings.TrimSuffix(pathpkg.Base(node), pathpkg.Ext(node)),
				ChunkIndex: -1,
			},
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     "graph_ppr",
				RawScore: score,
				Source:   "graph_ppr",
				Details:  details,
			}},
			Type: "note",
		}, true
	case noderead.GraphEndpointCode:
		h := knowledge.FileHandle(node)
		return search.Candidate{
			SourceRef: search.SourceRef{
				Path:       node,
				Title:      pathpkg.Base(node),
				ChunkIndex: -1,
			},
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     "graph_ppr",
				RawScore: score,
				Source:   "graph_ppr",
				Details:  details,
			}},
			Type: "code",
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
