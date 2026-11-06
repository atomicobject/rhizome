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
			if row.FieldName == field.Name {
				rows = append(rows, row)
			}
		}
	}
	var refs []ontology.NodeRef
	if node.Projection != nil {
		for _, occurrence := range node.Projection.Fields[field.Name].ReferenceOccurrences {
			if occurrence.Target != nil {
				refs = append(refs, *occurrence.Target)
				continue
			}
			for _, row := range rows {
				if row.ListOrdinal == occurrence.Ordinal && row.ValueText == occurrence.Authored && row.ReferenceAt == occurrence.At {
					if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
						refs = append(refs, ref)
					}
					break
				}
			}
		}
	} else {
		for _, row := range rows {
			if ref, ok := noderead.IndexedLinkTargetRef(row); ok {
				refs = append(refs, ref)
			}
		}
	}
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		target, err := e.rootNodeFromRef(ctx, ref)
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
