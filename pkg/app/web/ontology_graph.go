package web

import (
	"context"
	"errors"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func (s *Server) buildGraphWithNodeRead(ctx context.Context, req noderead.GraphRequest, depth int) (map[string]GraphNode, []GraphEdge, *noderead.GraphDiagnostics, error) {
	if s == nil || s.runtime == nil {
		return map[string]GraphNode{}, nil, nil, nil
	}
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return nil, nil, nil, errors.New("graph index unavailable")
	}
	var schema *ontology.Schema
	if defs, err := s.ontologyDefinitions(); err == nil && defs != nil {
		schema = defs.schema
	}
	graph, err := noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, intelStore, schema).NewScope(ctx, noderead.ScopeOptions{}).Graph(ctx, req)
	if err != nil {
		return nil, nil, nil, err
	}
	nodes := make(map[string]GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		s.mergeReadGraphNode(nodes, node, depth)
	}
	filterIgnoredGraphNodes(s, nodes)
	edges := make([]GraphEdge, 0, len(graph.Edges))
	for _, row := range graph.Edges {
		if _, ok := nodes[row.Source]; !ok {
			continue
		}
		if _, ok := nodes[row.Target]; !ok {
			continue
		}
		edges = append(edges, GraphEdge{
			Source:        row.Source,
			Target:        row.Target,
			Kind:          row.Kind,
			Weight:        row.Weight,
			RelationName:  row.RelationName,
			RelationLabel: row.RelationLabel,
			Provenance:    row.Provenance,
			Structural:    row.Structural,
		})
	}
	return nodes, edges, graph.Diagnostics, nil
}

func filterIgnoredGraphNodes(s *Server, nodes map[string]GraphNode) {
	if s == nil || s.runtime == nil {
		return
	}
	ignoreMatcher := s.runtime.IgnoreMatcher()
	if ignoreMatcher == nil {
		return
	}
	for id, node := range nodes {
		path := firstGraphString(node.NotePath, node.Path)
		if path != "" && ignoreMatcher.IsIgnored(path, false) {
			delete(nodes, id)
		}
	}
}

func (s *Server) mergeReadGraphNode(nodes map[string]GraphNode, node noderead.GraphEndpoint, depth int) {
	if node.ID == "" {
		return
	}
	kind := string(node.Kind)
	if kind == "" {
		kind = "note"
	}
	graphNode, ok := nodes[node.ID]
	if !ok {
		graphNode = GraphNode{
			ID:     node.ID,
			Path:   firstGraphString(node.Path, node.NotePath),
			Label:  firstGraphString(node.Label, titleFromPath(firstGraphString(node.NotePath, node.Path))),
			Kind:   kind,
			Lang:   nodeLang(kind, firstGraphString(node.Path, node.NotePath)),
			Module: moduleKey(firstGraphString(node.NotePath, node.Path), depth),
		}
	}
	graphNode.NotePath = node.NotePath
	graphNode.NodeID = node.NodeID
	if !node.Ref.IsZero() {
		ref := node.Ref
		graphNode.NodeRef = &ref
	}
	graphNode.SourceLocator = node.SourceLocator
	graphNode.ResolvedType = node.TypeName
	if node.Label != "" {
		graphNode.Label = node.Label
	}
	if graphNode.Module == "" {
		graphNode.Module = moduleKey(firstGraphString(node.NotePath, node.Path), depth)
	}
	if kind == "embedded" {
		graphNode.Kind = "embedded"
		graphNode.Lang = "markdown"
		graphNode.Module = "embedded:" + node.NodeID
	}
	nodes[node.ID] = graphNode
}

func firstGraphString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
