package views

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"sort"
	"strings"
)

func sortRows(rows []TableRow, specs []viewconfig.SortSpec, capabilities []FieldCapability) {
	if len(specs) == 0 || len(rows) < 2 {
		return
	}
	caps := capabilityMap(capabilities)
	order := make([]int, len(rows))
	values := make([]any, len(rows)*len(specs))
	for i, row := range rows {
		order[i] = i
		for j, spec := range specs {
			value, exists := fieldValue(row, spec.Field)
			if cap := caps[spec.Field]; indexedScalarSort(cap) {
				value = pushdown.AggregateSortRows(indexedScalarRows(value, exists, cap.ValueKind), cap.ValueKind, strings.EqualFold(spec.Direction, "desc"))
			}
			values[i*len(specs)+j] = value
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		for k, spec := range specs {
			left := values[order[i]*len(specs)+k]
			right := values[order[j]*len(specs)+k]
			desc := strings.EqualFold(spec.Direction, "desc")
			if cap := caps[spec.Field]; indexedScalarSort(cap) {
				if comparison := left.(pushdown.Scalar).CompareSort(right.(pushdown.Scalar), desc, true); comparison != 0 {
					return comparison < 0
				}
			} else if comparison := compareSortValues(left, right); comparison != 0 {
				return (comparison < 0) != desc
			}
		}
		return viewRowStableKey(rows[order[i]]).Compare(viewRowStableKey(rows[order[j]])) < 0
	})
	sorted := make([]TableRow, len(rows))
	for i, original := range order {
		sorted[i] = rows[original]
	}
	copy(rows, sorted)
}

func viewRowStableKey(row TableRow) pushdown.StableKey {
	return pushdown.StableKey{NotePath: firstNonEmpty(row.Ref.NotePath, row.Path), StartByte: row.Ref.StartByte, NodeID: ontology.OntologyNodeID(row.Ref)}
}

func indexedScalarSort(cap FieldCapability) bool {
	return cap.IndexedSortable && cap.ValueKind != "relation"
}

func indexedScalarField(kind string) *ontoschema.Field {
	if kind == "real" {
		kind = "Float"
	}
	return &ontoschema.Field{Kind: ontoschema.FieldKindScalar, TypeName: kind}
}

func indexedScalarRows(value any, present bool, kind string) []readmodel.FieldValueRow {
	field := indexedScalarField(kind)
	if values, ok := listValues(value); ok {
		var rows []readmodel.FieldValueRow
		for _, item := range values {
			rows = append(rows, indexedScalarRows(item, false, kind)...)
		}
		if len(rows) == 0 && present {
			rows = append(rows, ontology.FieldValueRow(field, ""))
		}
		return rows
	}
	texts := ontology.FieldTextValues(field, value)
	texts = pushdown.PresentSortValues(field, texts, present)
	rows := make([]readmodel.FieldValueRow, len(texts))
	for i, text := range texts {
		rows[i] = ontology.FieldValueRow(field, text)
	}
	return rows
}
