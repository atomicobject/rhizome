package noderead

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func (s *Scope) recordMatchesPredicates(ctx context.Context, record NodeRecord, predicates []readmodel.FieldPredicate) (bool, error) {
	for _, predicate := range predicates {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		field := s.recordSchemaField(record, predicate.FieldName)
		rows := s.scalarFieldRows(record, predicate.FieldName, field)
		if field != nil && field.Kind == ontoschema.FieldKindLink {
			values := s.FieldValues(record, predicate.FieldName)
			rows = make([]readmodel.FieldValueRow, 0, len(values))
			for _, value := range values {
				row := ontology.FieldValueRow(field, value)
				op := strings.ToLower(strings.TrimSpace(string(predicate.Op)))
				if row.ValueNorm != "" && op != "exists" && op != "missing" {
					resolved, err := s.resolveLinkFieldRowLocked(ctx, row)
					if err != nil {
						return false, err
					}
					row = resolved
				}
				rows = append(rows, row)
			}
		}
		if !pushdown.Match(predicate, rows) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Scope) resolveLinkFieldRowLocked(ctx context.Context, row readmodel.FieldValueRow) (readmodel.FieldValueRow, error) {
	cache, err := s.notePathCacheLocked(ctx)
	if err != nil {
		return row, err
	}
	targetInput := row.TargetNotePath
	row.TargetNotePath = ""
	if cache == nil {
		return row, nil
	}
	target, ok := cache.ResolveNoteTarget(targetInput)
	if !ok {
		return row, nil
	}
	typeName := ""
	staged := false
	index, err := s.overlayIndexLocked(ctx)
	if err != nil {
		return row, err
	}
	if index != nil {
		for key := range index.ItemKeysByTouchedPath[target.Path] {
			if record := index.RecordsByRef[key]; record.Ref.Kind == ontology.NodeKindNote {
				typeName, staged = record.TypeName, true
				if ontoschema.OntologyTypeVisibility(typeName) == ontoschema.TypeVisibilityInternal {
					typeName = ""
				}
				break
			}
		}
	}
	if !staged {
		types, err := s.typesByPathsLocked(ctx, []string{target.Path})
		if err != nil {
			return row, err
		}
		typeName = types[target.Path].TypeName
	}
	// Catalog link resolution retains the requested fragment on a note ref.
	ref := ontology.NodeRef{NotePath: target.Path, Fragment: target.Fragment, Kind: ontology.NodeKindNote, TypeName: strings.TrimSpace(typeName)}
	encoded, err := json.Marshal(ref)
	if err != nil {
		return row, err
	}
	row.TargetNotePath = ref.NotePath
	row.TargetTypeName = ref.TypeName
	row.TargetNodeID = ontology.OntologyNodeID(ref)
	row.TargetRefJSON = string(encoded)
	row.TargetSourceLocator = ontology.NodeSourceLocator(ref)
	return row, nil
}
