package pushdown

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/paths"
	"sort"
	"strings"
)

// Match evaluates materialized field rows using the same typed columns as SQL.
func Match(predicate readmodel.FieldPredicate, rows []readmodel.FieldValueRow) bool {
	op := strings.ToLower(strings.TrimSpace(string(predicate.Op)))
	if op == "" {
		op = "eq"
	}
	if op == "exists" || op == "missing" {
		present := false
		for _, row := range rows {
			present = present || row.ValueNorm != ""
		}
		return present == (op == "exists")
	}
	for _, operand := range predicate.Values {
		kind := PredicateValueKind(operand)
		right := ScalarFromRow(operand, kind)
		for _, row := range rows {
			matched := false
			switch {
			case op == "contains":
				matched = strings.Contains(row.ValueNorm, operand.ValueNorm)
			case operand.TargetNodeID != "":
				matched = row.TargetNodeID == operand.TargetNodeID
			case operand.TargetNotePath != "":
				matched = row.TargetNotePath == string(paths.Normalize(operand.TargetNotePath)) ||
					row.TargetNotePath == string(paths.EnsureNoteExtension(operand.TargetNotePath, ".md")) ||
					(operand.ValueNorm != "" && row.ValueNorm == operand.ValueNorm)
			default:
				comparison := op
				if comparison == "neq" {
					comparison = "eq"
				}
				matched = ScalarFromRow(row, kind).Matches(right, comparison)
			}
			if matched {
				return op != "neq"
			}
		}
		if op != "in" {
			break
		}
	}
	return op == "neq"
}

// SortItems prepares each key once, then applies SQL's null and stable ordering.
func SortItems[T any](items []T, sorts []readmodel.FieldSort, values func(T, string) []readmodel.FieldValueRow, stable func(T) StableKey) {
	if len(sorts) == 0 || len(items) < 2 {
		return
	}
	type entry struct {
		item   T
		keys   []Scalar
		stable StableKey
	}
	entries := make([]entry, len(items))
	for i, item := range items {
		keys := make([]Scalar, len(sorts))
		for j, spec := range sorts {
			keys[j] = AggregateSortRows(values(item, spec.FieldName), spec.ValueKind, spec.Desc)
		}
		entries[i] = entry{item, keys, stable(item)}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		for k, spec := range sorts {
			if order := entries[i].keys[k].CompareSort(entries[j].keys[k], spec.Desc, spec.NullsLast); order != 0 {
				return order < 0
			}
		}
		return entries[i].stable.Compare(entries[j].stable) < 0
	})
	for i, entry := range entries {
		items[i] = entry.item
	}
}
