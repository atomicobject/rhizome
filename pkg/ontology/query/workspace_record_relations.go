package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

// Containment exists even when a binding declares no semantic edge.
func workspaceRecordContainment(projection *ontology.NodeProjection, workspace *ontology.NodeWorkspace) ([]workspaceRelationItem, error) {
	var items []workspaceRelationItem
	if parent := workspace.Node.ParentRef; parent != nil {
		items = append(items, workspaceRelationItem{Ref: *parent, Title: workspaceRefTitle(*parent), RelationName: "parent", Provenance: "containment", Direction: noderead.TraversalDirectionInbound, Structural: true})
	}
	children, err := projection.Prepared.Children(projection.Ref)
	if err != nil {
		return items, err
	}
	for _, child := range children {
		items = append(items, workspaceRelationItem{Ref: child, Title: workspaceRefTitle(child), RelationName: "children", Provenance: "containment", Direction: noderead.TraversalDirectionOutbound, Structural: true})
	}
	return items, nil
}

// Reuse the provider-authored fragment backlink index; host links alone do not
// establish a backlink to any particular record in that host.
func (e *executor) workspaceRecordBacklinks(ctx context.Context, ref ontology.NodeRef) ([]workspaceRelationItem, error) {
	index, err := e.inboundSectionNeighborIndex(ctx, ref.NotePath)
	if err != nil {
		return nil, err
	}
	var items []workspaceRelationItem
	for _, path := range index[inboundSectionNeighborKey(ref.NotePath, ref.Fragment)] {
		source := ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}
		items = append(items, workspaceRelationItem{Ref: source, Title: workspaceRefTitle(source), Provenance: "backlink", Direction: noderead.TraversalDirectionInbound})
	}
	return items, nil
}
