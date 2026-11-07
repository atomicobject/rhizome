package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

// A provider's source-local node ID differs from the catalog's hashed row ID.
// Fragmentless relation endpoints still resolve through indexed rows, in one batch.
func (s *Scope) fillCatalogRowsBySourceNodeID(ctx context.Context, refs []ontology.NodeRef, rows map[string]readmodel.NodeRow) error {
	pending := map[string][]ontology.NodeRef{}
	var paths []string
	for _, ref := range refs {
		if s.service.Schema == nil {
			continue
		}
		typeDef := s.service.Schema.Types[ref.TypeName]
		if typeDef == nil || typeDef.SourceShape != ontoschema.EmbeddedSourceShapeRecord {
			continue
		}
		if _, ok := rows[RefIdentityKey(ref)]; ok || ref.NodeID == "" || ref.Fragment != "" {
			continue
		}
		key := nodeEndpointKey(ref.NotePath, ref.NodeID)
		pending[key] = append(pending[key], ref)
		paths = append(paths, ref.NotePath)
	}
	if len(paths) == 0 {
		return nil
	}
	candidates, err := s.service.Store.OntologyNodesByPaths(ctx, normalizeStrings(paths))
	if err != nil {
		return err
	}
	for _, row := range candidates {
		actual := nodeRefFromCatalogRow(row)
		for _, requested := range pending[nodeEndpointKey(actual.NotePath, actual.NodeID)] {
			if !nodeRecordMatchesRequestedRef(NodeRecord{Ref: actual}, requested) {
				continue
			}
			key := RefIdentityKey(requested)
			if previous, ok := rows[key]; ok && previous.NodeID != row.NodeID {
				return fmt.Errorf("%w: node ID %s in %s", ontology.ErrAmbiguousSourceSpanIdentity, requested.NodeID, requested.NotePath)
			}
			rows[key] = row
		}
	}
	return nil
}
