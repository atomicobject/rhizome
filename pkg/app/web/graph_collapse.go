package web

import (
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"path/filepath"
	"sort"
	"strings"
)

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
