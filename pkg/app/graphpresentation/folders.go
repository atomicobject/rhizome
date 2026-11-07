package graphpresentation

import (
	"sort"
	"strings"
)

// FolderOverview contains the file-level facts used by Explorer's folder layout.
type FolderOverview struct {
	Files       []GraphNode       `json:"files"`
	Links       []FolderLink      `json:"links"`
	NestedTypes []NestedTypeCount `json:"nestedTypes"`
	Truncated   bool              `json:"truncated"`
	NeedsIndex  bool              `json:"needsIndex,omitempty"`
}

type FolderLink struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	NoteCode int    `json:"noteCode"`
	Other    int    `json:"other"`
}

type NestedTypeCount struct {
	ResolvedType string `json:"resolvedType"`
	Count        int    `json:"count"`
}

func ProjectFolderOverview(graph GraphResponse) FolderOverview {
	out := FolderOverview{Files: []GraphNode{}, Links: []FolderLink{}, NestedTypes: []NestedTypeCount{}, Truncated: graph.Truncated, NeedsIndex: graph.NeedsIndex}
	paths := map[string]string{}
	owners := map[string]string{}
	types := map[string]int{}
	path := func(n GraphNode) string {
		p := n.NotePath
		if p == "" {
			p = n.Path
		}
		return strings.TrimLeft(p, "/")
	}
	for _, n := range graph.Nodes {
		if n.Kind != "note" && n.Kind != "code" && n.Kind != "module" {
			types[n.ResolvedType]++
			continue
		}
		owners[n.ID] = n.ID
		if n.Kind == "note" {
			paths[path(n)] = n.ID
		}
		out.Files = append(out.Files, GraphNode{ID: n.ID, Kind: n.Kind, Label: n.Label, Path: n.Path, NotePath: n.NotePath,
			NodeID: n.NodeID, ResolvedType: n.ResolvedType, Score: n.Score, Authority: n.Authority, Community: n.Community,
			ChildCount: n.ChildCount, Collapsed: n.Collapsed})
	}
	for _, n := range graph.Nodes {
		if n.Kind == "embedded" {
			owners[n.ID] = paths[path(n)]
		}
	}
	type pair struct{ a, b string }
	pairs := map[pair]int{}
	for _, edge := range graph.Edges {
		if edge.Kind == "embeds" {
			continue
		}
		a, b := owners[edge.Source], owners[edge.Target]
		if a == "" || b == "" || a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		key := pair{a, b}
		i, found := pairs[key]
		if !found {
			i = len(out.Links)
			pairs[key] = i
			out.Links = append(out.Links, FolderLink{Source: a, Target: b})
		}
		weight := edge.Weight
		if weight == 0 {
			weight = 1
		}
		if edge.Kind == "mentions" || edge.Kind == "coderef" {
			out.Links[i].NoteCode = max(out.Links[i].NoteCode, weight)
		} else {
			out.Links[i].Other = max(out.Links[i].Other, weight)
		}
	}
	for name, count := range types {
		out.NestedTypes = append(out.NestedTypes, NestedTypeCount{ResolvedType: name, Count: count})
	}
	sort.Slice(out.NestedTypes, func(i, j int) bool { return out.NestedTypes[i].ResolvedType < out.NestedTypes[j].ResolvedType })
	return out
}
