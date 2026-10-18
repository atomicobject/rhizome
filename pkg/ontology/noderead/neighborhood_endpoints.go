package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"strings"
)

func (s *Scope) neighborhoodEdgesForSourceRefs(sources []ontology.NodeRef, row semdb.OntologyEdgeRow, direction TraversalDirection, targetPath string, targetNodeID string, targetType string) []NeighborhoodEdge {
	out := make([]NeighborhoodEdge, 0, len(sources))
	for _, source := range sources {
		target := ontology.NodeRef{NotePath: targetPath, Kind: ontology.NodeKindNote, TypeName: targetType}
		if targetNodeID != "" {
			target.NodeID = targetNodeID
			target.Kind = ontology.NodeKindEmbedded
		}
		out = append(out, NeighborhoodEdge{
			Source:       source,
			Target:       target,
			Edge:         row,
			Direction:    direction,
			RelationName: row.RelationName,
			Provenance:   row.Provenance,
			Structural:   row.Structural,
			TargetType:   targetType,
			Depth:        1,
		})
	}
	return out
}

func (s *Scope) canonicalizeNeighborhoodEdgeRefsLocked(ctx context.Context, edges []NeighborhoodEdge) error {
	refs := make([]ontology.NodeRef, 0, len(edges)*2)
	for _, edge := range edges {
		refs = append(refs, edge.Source, edge.Target)
	}
	canonical, err := s.canonicalizeNodeRefsLocked(ctx, refs)
	if err != nil {
		return err
	}
	for i := range edges {
		edges[i].Source, edges[i].Target = canonical[2*i], canonical[2*i+1]
	}
	return nil
}

func (s *Scope) canonicalizeNodeRefsLocked(ctx context.Context, refs []ontology.NodeRef) ([]ontology.NodeRef, error) {
	embedded := make([]ontology.NodeRef, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.Kind == ontology.NodeKindEmbedded || ref.Kind == ontology.NodeKindSection {
			key := RefIdentityKey(ref)
			if !seen[key] {
				embedded = append(embedded, ref)
				seen[key] = true
			}
		}
	}
	var records []NodeRecord
	if s.hasOverlay() {
		overlay, missing, err := s.overlayRecordsByRefsLocked(ctx, embedded)
		if err != nil {
			return nil, err
		}
		records = overlay
		embedded = missing
	}
	if s.service.Store != nil {
		committed, _, err := s.embeddedRecordsFromCatalog(ctx, embedded)
		if err != nil {
			return nil, err
		}
		records = append(records, committed...)
	}
	byRequest := indexLoadedRecordsForRequests(refs, records)
	out := make([]ontology.NodeRef, len(refs))
	for i, ref := range refs {
		out[i] = NormalizeRef(ref)
		if record, ok := byRequest[RefIdentityKey(ref)]; ok {
			out[i] = record.Ref
		}
	}
	return out, nil
}

func sourceRefsForEndpoint(byPath map[string][]ontology.NodeRef, byEndpoint map[string][]ontology.NodeRef, path string, nodeID string) []ontology.NodeRef {
	if strings.TrimSpace(nodeID) != "" {
		return byEndpoint[nodeEndpointKey(path, nodeID)]
	}
	refs := byPath[path]
	out := make([]ontology.NodeRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == "" || ref.Kind == ontology.NodeKindNote {
			out = append(out, ref)
		}
	}
	return out
}

func nodeEndpointKey(path string, nodeID string) string {
	return strings.TrimSpace(path) + "\x00" + strings.TrimSpace(nodeID)
}
