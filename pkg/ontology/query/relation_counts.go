package query

import (
	"context"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

type pendingRelationCount struct {
	ref        ontology.NodeRef
	field      string
	projection *ontology.NodeProjection
}

func (e *executor) relationCount(ref ontology.NodeRef, typeName, countName string, projection *ontology.NodeProjection) (any, bool) {
	field, ok := e.relationFieldForCount(typeName, countName)
	if !ok {
		return nil, false
	}
	return pendingRelationCount{ref: ref, field: field.Name, projection: projection}, true
}

// Defer counts until every nested selection is known, as with record facts.
func (e *executor) fillRelationCounts(ctx context.Context, data map[string]any) {
	refsByField := map[string]map[string]ontology.NodeRef{}
	projectionsByField := map[string][]*ontology.NodeProjection{}
	walkResponseValues(data, func(value any) any {
		if count, ok := value.(pendingRelationCount); ok {
			if refsByField[count.field] == nil {
				refsByField[count.field] = map[string]ontology.NodeRef{}
			}
			refsByField[count.field][noderead.RefIdentityKey(count.ref)] = count.ref
			if count.projection != nil {
				projectionsByField[count.field] = append(projectionsByField[count.field], count.projection)
			}
		}
		return value
	})
	counts := map[string]map[string]int{}
	fields := make([]string, 0, len(refsByField))
	for name := range refsByField {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	for _, field := range fields {
		refs := make([]ontology.NodeRef, 0, len(refsByField[field]))
		for _, ref := range refsByField[field] {
			refs = append(refs, ref)
		}
		selections := noderead.RelationCountSelectionsForField(e.schema, refs, field)
		result, err := e.loaders.scope.RelationCounts(ctx, noderead.RelationCountsRequest{Sources: refs, Selections: selections, SourceProjections: projectionsByField[field]})
		if err != nil {
			e.addError(nil, err.Error())
			continue
		}
		counts[field] = map[string]int{}
		for _, item := range result.Sources {
			counts[field][noderead.RefIdentityKey(item.Source)] = item.Count
		}
	}
	walkResponseValues(data, func(value any) any {
		if count, ok := value.(pendingRelationCount); ok {
			return counts[count.field][noderead.RefIdentityKey(count.ref)]
		}
		return value
	})
}
