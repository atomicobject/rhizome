package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveNeighborhoodSelectionSet(ctx context.Context, source ontology.NodeRef, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	first := intValue(args["first"], 20)
	if first <= 0 {
		first = 20
	}
	if first > nestedFieldFirstMax {
		first = nestedFieldFirstMax
	}
	req := noderead.NeighborhoodRequest{
		Sources:           []ontology.NodeRef{source},
		Direction:         traversalDirectionValue(args["direction"]),
		RelationNames:     optionalSingle(stringValue(args["relation"])),
		IncludeStructural: true,
		IncludeAmbient:    true,
		TargetTypes:       optionalSingle(stringValue(args["type"])),
		FirstPerSource:    first,
		FirstTotal:        first,
		Hydrate:           noderead.HydrateOptions{Profile: noderead.HydrateSummary},
	}
	result := noderead.NeighborhoodResult{}
	if e.loaders != nil && e.loaders.scope != nil {
		var err error
		result, err = e.loaders.scope.Neighborhood(ctx, req)
		if err != nil {
			e.addError(path, err.Error())
			result = noderead.NeighborhoodResult{}
		}
	}
	truncated := false
	if len(result.Sources) > 0 {
		truncated = result.Sources[0].Truncated
	}
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "edges":
			edges := make([]any, 0, len(result.Edges))
			for _, edge := range result.Edges {
				edges = append(edges, e.resolveRelationEdgeSelectionSet(edge, current.SelectionSet, append(path, key)))
			}
			out[key] = edges
		case "nodes":
			nodes := make([]any, 0, len(result.Nodes))
			for _, record := range result.Nodes {
				if node, err := e.rootNodeFromRef(ctx, record.Ref); err == nil && node != nil {
					nodes = append(nodes, e.resolveAnyNodeSelectionSet(ctx, node, current.SelectionSet, append(path, key)))
				}
			}
			out[key] = nodes
		case "truncated":
			out[key] = truncated
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeNeighborhood", current.Name))
		}
	}
	return out
}

func (e *executor) emptyNeighborhoodSelectionSet(set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "edges", "nodes":
			out[key] = []any{}
		case "truncated":
			out[key] = false
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeNeighborhood", field.Name))
		}
	}
	return out
}

func (e *executor) resolveRelationEdgeSelectionSet(edge noderead.NeighborhoodEdge, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "source":
			out[key] = nodeRefGraphQLValue(edge.Source)
		case "target":
			out[key] = nodeRefGraphQLValue(edge.Target)
		case "direction":
			out[key] = traversalDirectionGraphQLValue(edge.Direction)
		case "relation":
			out[key] = emptyNil(edge.RelationName)
		case "provenance":
			out[key] = emptyNil(edge.Provenance)
		case "structural":
			out[key] = edge.Structural
		case "targetType":
			out[key] = emptyNil(edge.TargetType)
		case "depth":
			out[key] = edge.Depth
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeRelationEdge", field.Name))
		}
	}
	return out
}

func (e *executor) resolveLocalGraphSelectionSet(ctx context.Context, source ontology.NodeRef, field *ast.Field, path []string) map[string]any {
	args := e.fieldArgs(field)
	nodeLimit := boundedInt(args["nodeLimit"], 50, 500)
	edgeLimit := boundedInt(args["edgeLimit"], 100, 1000)
	result := noderead.GraphResult{}
	if e.loaders != nil && e.loaders.scope != nil {
		var err error
		req := noderead.GraphRequest{
			Sources:          []ontology.NodeRef{source},
			Profile:          graphProfileValue(args["profile"]),
			NodeLimit:        nodeLimit,
			EdgeLimit:        edgeLimit,
			IncludeCodeEdges: boolValue(args["includeCodeEdges"]),
			Diagnostics:      false,
		}
		result, err = e.loaders.scope.Graph(ctx, req)
		if err == nil && len(result.Nodes) == 0 && len(result.Edges) == 0 && (source.Kind == ontology.NodeKindEmbedded || source.Kind == ontology.NodeKindSection) {
			req.Sources = []ontology.NodeRef{{NotePath: source.NotePath, Kind: ontology.NodeKindNote, TypeName: source.TypeName}}
			result, err = e.loaders.scope.Graph(ctx, req)
		}
		if err != nil {
			e.addError(path, err.Error())
			result = noderead.GraphResult{}
		}
	}
	truncated := len(result.Nodes) >= nodeLimit || len(result.Edges) >= edgeLimit
	out := make(map[string]any)
	for _, selection := range field.SelectionSet {
		current, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(current)
		switch current.Name {
		case "nodes":
			nodes := make([]any, 0, len(result.Nodes))
			for _, node := range result.Nodes {
				nodes = append(nodes, e.resolveLocalGraphNodeSelectionSet(node, current.SelectionSet, append(path, key)))
			}
			out[key] = nodes
		case "edges":
			edges := make([]any, 0, len(result.Edges))
			for _, edge := range result.Edges {
				edges = append(edges, e.resolveLocalGraphEdgeSelectionSet(edge, current.SelectionSet, append(path, key)))
			}
			out[key] = edges
		case "truncated":
			out[key] = truncated
		case "diagnostics":
			out[key] = result.Diagnostics
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraph", current.Name))
		}
	}
	return out
}

func (e *executor) emptyLocalGraphSelectionSet(set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "nodes", "edges":
			out[key] = []any{}
		case "truncated":
			out[key] = false
		case "diagnostics":
			out[key] = nil
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraph", field.Name))
		}
	}
	return out
}

func (e *executor) resolveLocalGraphNodeSelectionSet(node noderead.GraphEndpoint, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "id":
			out[key] = node.ID
		case "ref":
			if strings.TrimSpace(queryLocatorKey(node.Ref)) == "" {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(node.Ref)
			}
		case "nodeKind":
			out[key] = strings.ToUpper(string(node.Kind))
		case "path":
			out[key] = emptyNil(node.Path)
		case "notePath":
			out[key] = emptyNil(node.NotePath)
		case "nodeId":
			out[key] = emptyNil(node.NodeID)
		case "typeName":
			out[key] = emptyNil(node.TypeName)
		case "title":
			out[key] = firstNonEmpty(node.Label, node.Path, node.ID)
		case "sourceLocator":
			out[key] = emptyNil(node.SourceLocator)
		case "parentId":
			out[key] = emptyNil(node.ParentID)
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraphNode", field.Name))
		}
	}
	return out
}

func (e *executor) resolveLocalGraphEdgeSelectionSet(edge noderead.GraphReadEdge, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "source":
			out[key] = edge.Source
		case "target":
			out[key] = edge.Target
		case "kind":
			out[key] = edge.Kind
		case "relation":
			out[key] = emptyNil(edge.RelationName)
		case "relationLabel":
			out[key] = emptyNil(edge.RelationLabel)
		case "provenance":
			out[key] = emptyNil(edge.Provenance)
		case "structural":
			out[key] = edge.Structural
		case "weight":
			out[key] = edge.Weight
		case "confidence":
			out[key] = edge.Confidence
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on LocalGraphEdge", field.Name))
		}
	}
	return out
}
