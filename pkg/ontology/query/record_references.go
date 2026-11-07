package query

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/vektah/gqlparser/v2/ast"
)

// Cross-host targets come from the shared indexed binding pass; current local
// occurrences can resolve without an index and never borrow a changed value.
func (e *executor) resolveRecordReferenceField(ctx context.Context, node *sectionRecord, field *ontoschema.Field, set ast.SelectionSet, path []string) (any, bool) {
	rows := node.indexedFields[strings.ToLower(field.Name)]
	if node.Projection != nil {
		indexed, err := e.loaders.scope.IndexedFieldValues(ctx, []*ontology.NodeProjection{node.Projection})
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		rows = nil
		for _, row := range indexed[noderead.RefIdentityKey(node.Projection.Ref)] {
			if strings.EqualFold(row.FieldName, field.Name) {
				rows = append(rows, row)
			}
		}
	}
	var refs []ontology.NodeRef
	if node.Projection != nil {
		for _, occurrence := range node.Projection.Fields[field.Name].ReferenceOccurrences {
			indexedOutcome := false
			for _, row := range rows {
				if row.ListOrdinal == occurrence.Ordinal && row.ValueText == occurrence.Authored && row.ReferenceAt == occurrence.At {
					indexedOutcome = true
					if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
						refs = append(refs, ref)
					}
					break
				}
			}
			if !indexedOutcome && occurrence.Target != nil {
				refs = append(refs, *occurrence.Target)
			}
		}
	} else {
		for _, row := range rows {
			if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
				refs = append(refs, ref)
			}
		}
	}
	var summaryRefs []ontology.NodeRef
	for _, ref := range refs {
		if ontology.IsRecordSelector(ref.Fragment) && !e.ontologyNodeRootSelectionNeedsProjection(ref.TypeName, set) {
			summaryRefs = append(summaryRefs, ref)
		}
	}
	summaries := map[string]*resolvedNode{}
	if len(summaryRefs) > 0 {
		records, err := e.loaders.scope.Hydrate(ctx, summaryRefs, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		for _, record := range records {
			section := &sectionRecord{catalogRef: record.Ref, catalogTitle: record.Title, TypeName: record.TypeName, Note: &noteRecord{Path: record.Ref.NotePath}, indexedFields: record.FieldValues, PropertyCase: e.propertyCaseForType(record.TypeName)}
			summaries[noderead.RefIdentityKey(record.Ref)] = &resolvedNode{Section: section}
		}
	}
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		target := summaries[noderead.RefIdentityKey(ref)]
		var err error
		if target == nil {
			target, err = e.rootNodeFromRef(ctx, ref)
		}
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if target != nil {
			out = append(out, e.resolveAnyNodeSelectionSet(ctx, target, set, path))
		}
	}
	if field.List {
		return out, true
	}
	if len(out) == 0 {
		return nil, true
	}
	return out[0], true
}
