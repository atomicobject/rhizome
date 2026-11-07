package query

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/vektah/gqlparser/v2/ast"
)

// pendingRecordReferences holds a RECORD reference field's place in the
// response until fillRecordReferences resolves every such field together.
type pendingRecordReferences struct {
	node  *sectionRecord
	field *ontoschema.Field
	set   ast.SelectionSet
	path  []string
	first int
}

func (e *executor) resolveRecordReferenceField(_ context.Context, node *sectionRecord, field *ontoschema.Field, set ast.SelectionSet, path []string, first int) (any, bool) {
	return &pendingRecordReferences{node: node, field: field, set: set, path: append([]string(nil), path...), first: first}, true
}

// fillRecordReferences resolves reference fields level by level after the
// selection tree, so all fields at one depth share one indexed field-value
// read and one summary hydration however many parent records they have.
// Targets' own reference fields resolve in the next round.
func (e *executor) fillRecordReferences(ctx context.Context, data map[string]any) {
	for {
		var pending []*pendingRecordReferences
		walkResponseValues(data, func(value any) any {
			if item, ok := value.(*pendingRecordReferences); ok {
				pending = append(pending, item)
			}
			return value
		})
		if len(pending) == 0 {
			return
		}
		resolved := e.resolveRecordReferenceBatch(ctx, pending)
		walkResponseValues(data, func(value any) any {
			if item, ok := value.(*pendingRecordReferences); ok {
				return resolved[item]
			}
			return value
		})
	}
}

// Cross-host targets come from the shared indexed binding pass; current local
// occurrences can resolve without an index and never borrow a changed value.
func (e *executor) resolveRecordReferenceBatch(ctx context.Context, pending []*pendingRecordReferences) map[*pendingRecordReferences]any {
	out := make(map[*pendingRecordReferences]any, len(pending))
	var projections []*ontology.NodeProjection
	for _, item := range pending {
		if item.node.Projection != nil {
			projections = append(projections, item.node.Projection)
		}
	}
	var indexed map[string][]readmodel.FieldValueRow
	if len(projections) > 0 {
		var err error
		if indexed, err = e.loaders.scope.IndexedFieldValues(ctx, projections); err != nil {
			e.addError(pending[0].path, err.Error())
			return out
		}
	}
	refsByItem := make(map[*pendingRecordReferences][]ontology.NodeRef, len(pending))
	var summaryRefs []ontology.NodeRef
	var hostPaths []string
	for _, item := range pending {
		refs := recordReferenceTargets(item, indexed)
		// Cap before any target is hydrated or resolved.
		if item.field.List && item.first > 0 && len(refs) > item.first {
			refs = refs[:item.first]
		}
		refsByItem[item] = refs
		for _, ref := range refs {
			if ontology.IsRecordSelector(ref.Fragment) && !e.ontologyNodeRootSelectionNeedsProjection(ref.TypeName, item.set) {
				summaryRefs = append(summaryRefs, ref)
			} else {
				hostPaths = append(hostPaths, ref.NotePath)
			}
		}
	}
	// Other targets resolve one by one below; warm their hosts' cached
	// records and types together first.
	if _, err := e.loaders.ontologyTypesByPaths(ctx, hostPaths); err != nil {
		e.addError(pending[0].path, err.Error())
		return out
	}
	if _, err := e.loaders.recordsForPaths(ctx, hostPaths); err != nil {
		e.addError(pending[0].path, err.Error())
		return out
	}
	summaries := map[string]*resolvedNode{}
	if len(summaryRefs) > 0 {
		records, err := e.loaders.scope.Hydrate(ctx, summaryRefs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
		if err != nil {
			e.addError(pending[0].path, err.Error())
			return out
		}
		for _, record := range records {
			section := &sectionRecord{catalogRef: record.Ref, catalogTitle: record.Title, TypeName: record.TypeName, Note: &noteRecord{Path: record.Ref.NotePath}, indexedFields: record.FieldValues, PropertyCase: e.propertyCaseForType(record.TypeName)}
			summaries[noderead.RefIdentityKey(record.Ref)] = &resolvedNode{Section: section}
		}
	}
	for _, item := range pending {
		values := make([]any, 0, len(refsByItem[item]))
		failed := false
		for _, ref := range refsByItem[item] {
			target := summaries[noderead.RefIdentityKey(ref)]
			if target == nil {
				var err error
				if ref.Kind == ontology.NodeKindNote && ref.Fragment == "" && ref.NodeID == "" {
					// A document target names its note, which the warmed
					// loader serves without projecting the note's source.
					var note *noteRecord
					if note, err = e.rootNote(ctx, ref.NotePath); note != nil {
						target = &resolvedNode{Note: note}
					}
				} else {
					target, err = e.rootNodeFromRef(ctx, ref)
				}
				if err != nil {
					e.addError(item.path, err.Error())
					failed = true
					break
				}
			}
			if target != nil {
				values = append(values, e.resolveAnyNodeSelectionSet(ctx, target, item.set, item.path))
			}
		}
		switch {
		case failed:
			out[item] = nil
		case item.field.List:
			out[item] = values
		case len(values) > 0:
			out[item] = values[0]
		default:
			out[item] = nil
		}
	}
	return out
}

func recordReferenceTargets(item *pendingRecordReferences, indexed map[string][]readmodel.FieldValueRow) []ontology.NodeRef {
	node, field := item.node, item.field
	rows := node.indexedFields[strings.ToLower(field.Name)]
	if node.Projection == nil {
		var refs []ontology.NodeRef
		for _, row := range rows {
			if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	}
	rows = nil
	for _, row := range indexed[noderead.RefIdentityKey(node.Projection.Ref)] {
		if strings.EqualFold(row.FieldName, field.Name) {
			rows = append(rows, row)
		}
	}
	var refs []ontology.NodeRef
	for _, occurrence := range node.Projection.Fields[field.Name].ReferenceOccurrences {
		if ref, ok := recordOccurrenceTarget(occurrence, rows); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

// recordOccurrenceTarget honors the indexed outcome of one reference
// occurrence, including unresolved and ambiguous ones; only an occurrence the
// index has not seen falls back to its current local target.
func recordOccurrenceTarget(occurrence ontology.RecordReferenceOccurrence, rows []readmodel.FieldValueRow) (ontology.NodeRef, bool) {
	for _, row := range rows {
		if row.ListOrdinal == occurrence.Ordinal && row.ValueText == occurrence.Authored && row.ReferenceAt == occurrence.At {
			return noderead.IndexedLinkTargetRef(row)
		}
	}
	if occurrence.Target != nil {
		return *occurrence.Target, true
	}
	return ontology.NodeRef{}, false
}
