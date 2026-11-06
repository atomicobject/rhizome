package web

import (
	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
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

func collapseGraph(nodes map[string]graphpresentation.GraphNode, edges []graphpresentation.GraphEdge, limit int) ([]graphpresentation.GraphNode, []graphpresentation.GraphEdge, bool) {
	collapsed, resultEdges, truncated := graphpresentation.Collapse(nodes, edges, limit, func(path string) string { return moduleKey(path, defaultModuleDepth) })
	if truncated {
		byModule := map[string][]graphpresentation.GraphNode{}
		for _, node := range nodes {
			byModule[node.Module] = append(byModule[node.Module], node)
		}
		for i := range collapsed {
			node := &collapsed[i]
			if !node.Collapsed {
				continue
			}
			counts := map[string]int{}
			for _, member := range byModule[node.Path] {
				if member.Community != "" {
					counts[member.Community]++
				}
				if member.Authority > node.Authority {
					node.Authority = member.Authority
				}
				if member.Hub > node.Hub {
					node.Hub = member.Hub
				}
			}
			for community, count := range counts {
				if node.Community == "" || count > counts[node.Community] {
					node.Community = community
				}
			}
		}
	}
	sort.SliceStable(collapsed, func(i, j int) bool { return collapsed[i].ID < collapsed[j].ID })
	if truncated {
		sortGraphEdges(resultEdges)
	} else {
		resultEdges = sortedEdgeSlice(resultEdges)
	}
	return collapsed, resultEdges, truncated
}

func finalizeGraph(nodes map[string]graphpresentation.GraphNode, edges []graphpresentation.GraphEdge, limit int, notesOnly bool) ([]graphpresentation.GraphNode, []graphpresentation.GraphEdge, bool) {
	if notesOnly {
		// Ontology mode asks the backend for a notes-only graph up front. If we
		// collapse first, large note sets can disappear into module stand-ins and
		// the frontend has nothing meaningful left to render.
		return sortedNodes(nodes), sortedEdgeSlice(edges), false
	}
	return collapseGraph(nodes, edges, limit)
}

func sortedNodes(nodes map[string]graphpresentation.GraphNode) []graphpresentation.GraphNode {
	out := make([]graphpresentation.GraphNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedEdgeSlice(edges []graphpresentation.GraphEdge) []graphpresentation.GraphEdge {
	out := append([]graphpresentation.GraphEdge(nil), edges...)
	sortGraphEdges(out)
	return out
}

func sortGraphEdges(edges []graphpresentation.GraphEdge) {
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
