package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func graphNodeRowsByID(rows []readmodel.GraphNodeRow) map[string]readmodel.GraphNodeRow {
	out := make(map[string]readmodel.GraphNodeRow, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.NodeID)
		if id == "" {
			continue
		}
		out[id] = row
		if ref := nodeRefFromGraphNodeRow(row); strings.TrimSpace(ref.NodeID) != "" {
			out[ref.NodeID] = row
		}
	}
	return out
}

func graphEmbedsSourceID(row readmodel.GraphNodeRow, visibleRows map[string]readmodel.GraphNodeRow, allRows map[string]readmodel.GraphNodeRow) string {
	for parentID := strings.TrimSpace(row.ParentNodeID); parentID != ""; {
		if parent, ok := visibleRows[parentID]; ok && ontology.NodeKind(parent.NodeKind) == ontology.NodeKindEmbedded {
			return graphEndpointID(GraphEndpointEmbedded, parent.NodeID)
		}
		parent, ok := allRows[parentID]
		if !ok {
			break
		}
		if ontology.NodeKind(parent.NodeKind) == ontology.NodeKindNote {
			return graphEndpointID(GraphEndpointNote, parent.NotePath)
		}
		parentID = strings.TrimSpace(parent.ParentNodeID)
	}
	return graphEndpointID(GraphEndpointNote, row.NotePath)
}

func graphSourceEndpointIDs(refs []ontology.NodeRef, rows []readmodel.GraphNodeRow) map[string]struct{} {
	out := map[string]struct{}{}
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() {
			continue
		}
		if ref.Kind != ontology.NodeKindEmbedded && ref.Kind != ontology.NodeKindSection && strings.TrimSpace(ref.NodeID) == "" && strings.TrimSpace(ref.Fragment) == "" && strings.TrimSpace(ref.Structural) == "" {
			out[graphEndpointID(GraphEndpointNote, ref.NotePath)] = struct{}{}
			continue
		}
		for _, row := range rows {
			if !graphSourceRefMatchesRow(ref, row) {
				continue
			}
			id := graphEndpointID(graphEndpointKindForOntologyNodeKind(row.NodeKind), row.NodeID)
			if id != "" {
				out[id] = struct{}{}
			}
		}
	}
	return out
}

func graphSourceRefMatchesRow(ref ontology.NodeRef, row readmodel.GraphNodeRow) bool {
	if strings.TrimSpace(ref.NotePath) != "" && strings.TrimSpace(ref.NotePath) != strings.TrimSpace(row.NotePath) {
		return false
	}
	if strings.TrimSpace(ref.NodeID) != "" {
		rowRef := nodeRefFromGraphNodeRow(row)
		if strings.TrimSpace(ref.NodeID) != strings.TrimSpace(row.NodeID) &&
			strings.TrimSpace(ref.NodeID) != strings.TrimSpace(row.BlockID) &&
			strings.TrimSpace(ref.NodeID) != strings.TrimSpace(row.Fragment) &&
			strings.TrimSpace(ref.NodeID) != strings.TrimSpace(rowRef.NodeID) {
			return false
		}
	}
	if ref.Kind != "" && row.NodeKind != "" && string(ref.Kind) != row.NodeKind {
		return false
	}
	fragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	if fragment != "" {
		rowFragment := strings.TrimPrefix(strings.TrimSpace(row.Fragment), "#")
		rowBlock := "^" + strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^")
		if fragment != rowFragment && fragment != rowBlock && "#"+fragment != strings.TrimPrefix(strings.TrimSpace(row.SourceLocator), row.NotePath) {
			return false
		}
	}
	if strings.TrimSpace(ref.Structural) != "" && strings.TrimSpace(ref.Structural) != strings.TrimSpace(row.Fragment) && strings.TrimSpace(ref.Structural) != strings.TrimSpace(row.BlockID) {
		return false
	}
	return true
}

func graphEndpointInScope(sourceIDs map[string]struct{}, a, b string, endpointScoped bool) bool {
	if len(sourceIDs) == 0 {
		return !endpointScoped
	}
	_, aOK := sourceIDs[a]
	_, bOK := sourceIDs[b]
	return aOK || bOK
}

func graphOntologyNodeVisible(row readmodel.GraphNodeRow, options graphOptions, sourceIDs map[string]struct{}) bool {
	kind := ontology.NodeKind(row.NodeKind)
	switch kind {
	case ontology.NodeKindEmbedded:
		return options.includeEmbedded || graphEndpointInSourceSet(sourceIDs, GraphEndpointEmbedded, row.NodeID)
	case ontology.NodeKindSection:
		return graphEndpointInSourceSet(sourceIDs, GraphEndpointSection, row.NodeID)
	default:
		return false
	}
}

func graphEndpointInSourceSet(sourceIDs map[string]struct{}, kind GraphEndpointKind, nodeID string) bool {
	if len(sourceIDs) == 0 || strings.TrimSpace(nodeID) == "" {
		return false
	}
	_, ok := sourceIDs[graphEndpointID(kind, nodeID)]
	return ok
}

func graphDocEdges(ctx context.Context, store ReadStore, paths []string, prefixes []string, limit int, includeCode bool, includeCodeEdges bool) ([]readmodel.GraphDocEdgeRow, error) {
	return store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{
		Paths:            paths,
		PathPrefixes:     prefixes,
		IncludeCode:      includeCode,
		IncludeCodeEdges: includeCodeEdges,
		Limit:            limit,
	})
}
