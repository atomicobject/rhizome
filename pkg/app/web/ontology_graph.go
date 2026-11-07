package web

import (
	"context"
	"errors"
	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func (s *Server) buildGraphWithNodeRead(ctx context.Context, req noderead.GraphRequest, depth int) (map[string]graphpresentation.GraphNode, []graphpresentation.GraphEdge, *noderead.GraphDiagnostics, error) {
	if s == nil || s.runtime == nil {
		return map[string]graphpresentation.GraphNode{}, nil, nil, nil
	}
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return nil, nil, nil, errors.New("graph index unavailable")
	}
	var schema *ontoschema.Schema
	if defs, err := s.ontologyDefinitions(); err == nil && defs != nil {
		schema = defs.schema
	}
	graph, err := noderead.NewService(s.cfg.VaultDef, &obsidian.Note{}, intelStore, schema).NewScope(noderead.ScopeOptions{}).Graph(ctx, req)
	if err != nil {
		return nil, nil, nil, err
	}
	nodes := make(map[string]graphpresentation.GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		s.mergeReadGraphNode(nodes, node, depth)
	}
	filterIgnoredGraphNodes(s, nodes)
	edges := graphpresentation.ProjectEdges(nodes, graph.Edges)
	return nodes, edges, graph.Diagnostics, nil
}

func filterIgnoredGraphNodes(s *Server, nodes map[string]graphpresentation.GraphNode) {
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

func (s *Server) mergeReadGraphNode(nodes map[string]graphpresentation.GraphNode, node noderead.GraphEndpoint, depth int) {
	if node.ID == "" {
		return
	}
	kind := string(node.Kind)
	if kind == "" {
		kind = "note"
	}
	graphNode, ok := nodes[node.ID]
	if !ok {
		graphNode = graphpresentation.GraphNode{
			ID:     node.ID,
			Path:   firstGraphString(node.Path, node.NotePath),
			Label:  firstGraphString(node.Label, paths.NoteTitle(firstGraphString(node.NotePath, node.Path))),
			Kind:   kind,
			Lang:   nodeLang(kind, firstGraphString(node.Path, node.NotePath)),
			Module: moduleKey(firstGraphString(node.NotePath, node.Path), depth),
		}
	}
	projected := graphpresentation.ProjectNode(node)
	graphNode.NotePath = projected.NotePath
	graphNode.NodeID = projected.NodeID
	if projected.NodeRef != nil {
		graphNode.NodeRef = projected.NodeRef
	}
	graphNode.SourceLocator = projected.SourceLocator
	graphNode.ResolvedType = projected.ResolvedType
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
