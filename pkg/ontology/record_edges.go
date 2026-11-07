package ontology

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func recordEdges(projection *NodeProjection, schema *ontoschema.Schema, now int64) []semdb.OntologyEdgeRow {
	if projection.Type == nil {
		return nil
	}
	var out []semdb.OntologyEdgeRow
	appendEdge := func(edge *ontoschema.RecordEdge, target NodeRef, occurrences []readmodel.RelationOccurrence) {
		if edge == nil {
			return
		}
		source := projection.Ref
		if edge.Direction == ontoschema.NeighborDirectionInbound {
			source, target = target, source
		}
		evidence := ""
		if len(occurrences) > 0 {
			encoded, _ := json.Marshal(occurrences)
			evidence = string(encoded)
		}
		out = append(out, semdb.OntologyEdgeRow{SrcPath: source.NotePath, SrcNodeID: source.NodeID, RelationName: edge.Name, DstPath: target.NotePath, DstNodeID: target.NodeID, DstType: target.TypeName, Provenance: "field", Structural: true, SchemaHash: schema.Hash, UpdatedAt: now, OccurrencesJSON: evidence})
	}
	for _, field := range projection.Type.Fields {
		binding := projection.Fields[field.Name]
		if field.RecordContains != nil {
			for _, target := range binding.ChildRefs {
				appendEdge(field.RecordContains.Edge, target, nil)
			}
		}
		if field.Record != nil && field.Record.References != nil {
			for _, occurrence := range binding.ReferenceOccurrences {
				if occurrence.Target != nil {
					appendEdge(field.Record.References.Edge, *occurrence.Target, []readmodel.RelationOccurrence{{Ordinal: occurrence.Ordinal, Value: occurrence.Authored, At: occurrence.At, Node: projection.Ref.NodeID, Field: field.Name}})
				}
			}
		}
	}
	return out
}
