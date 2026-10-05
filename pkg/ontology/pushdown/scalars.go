package pushdown

import (
	"cmp"
	"math"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

type Scalar struct {
	kind    string
	integer int64
	number  float64
	text    string
	Present bool
}

type StableKey struct {
	NotePath  string
	StartByte int
	NodeID    string
}

func (left StableKey) Compare(right StableKey) int {
	if order := strings.Compare(left.NotePath, right.NotePath); order != 0 {
		return order
	}
	if order := cmp.Compare(left.StartByte, right.StartByte); order != 0 {
		return order
	}
	return strings.Compare(left.NodeID, right.NodeID)
}

func (left Scalar) Compare(right Scalar) int {
	switch left.kind {
	case "bool", "int":
		return cmp.Compare(left.integer, right.integer)
	case "real":
		return cmp.Compare(left.number, right.number)
	default:
		return strings.Compare(left.text, right.text)
	}
}

func PredicateValueKind(row codeanchor.IntelOntologyNodeFieldValue) string {
	switch {
	case row.ValueBool != nil:
		return "bool"
	case row.ValueInt != nil:
		return "int"
	case row.ValueReal != nil:
		return "real"
	case row.ValueDate != nil:
		return "date"
	case row.ValueDateTime != nil:
		return "datetime"
	default:
		return "string"
	}
}

func ScalarFromRow(row codeanchor.IntelOntologyNodeFieldValue, kind string) Scalar {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "boolean":
		kind = "bool"
	case "integer":
		kind = "int"
	case "float", "number":
		kind = "real"
	}
	value := Scalar{kind: kind}
	switch kind {
	case "bool":
		if row.ValueBool != nil {
			value.Present = true
			if *row.ValueBool {
				value.integer = 1
			}
		}
	case "int":
		if row.ValueInt != nil {
			value.integer, value.Present = *row.ValueInt, true
		}
	case "real":
		if row.ValueReal != nil && !math.IsNaN(*row.ValueReal) {
			value.number, value.Present = *row.ValueReal, true
		}
	case "date":
		if row.ValueDate != nil {
			value.text, value.Present = *row.ValueDate, true
		}
	case "datetime":
		if row.ValueDateTime != nil {
			value.text, value.Present = *row.ValueDateTime, true
		}
	default:
		value.text, value.Present = row.ValueNorm, true
	}
	return value
}

// StoredFieldValue keeps the materialized Date validation distinct from operand coercion.
func StoredFieldValue(field *ontology.Field, raw string) codeanchor.IntelOntologyNodeFieldValue {
	row := FieldValueRow(field, raw, NormalizeScalarText)
	if row.ValueDate != nil {
		if _, err := time.Parse("2006-01-02", *row.ValueDate); err != nil {
			row.ValueDate = nil
		}
	}
	return row
}

func StoredScalar(field *ontology.Field, raw, kind string) Scalar {
	return ScalarFromRow(StoredFieldValue(field, raw), kind)
}

func AggregateSortValues(field *ontology.Field, values []string, kind string, desc bool) Scalar {
	var best Scalar
	for _, raw := range values {
		value := StoredScalar(field, raw, kind)
		best = selectSortValue(best, value, desc)
	}
	return best
}

func PresentSortValues(field *ontology.Field, values []string, present bool) []string {
	if len(values) == 0 && present && field != nil && field.Kind != ontology.FieldKindLink {
		return []string{""}
	}
	return values
}

func NormalizeScalarText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "[[")
	value = strings.TrimSuffix(value, "]]")
	if alias := strings.Index(value, "|"); alias >= 0 {
		value = strings.TrimSpace(value[:alias])
	}
	return value
}

func AggregateSortRows(rows []codeanchor.IntelOntologyNodeFieldValue, kind string, desc bool) Scalar {
	var best Scalar
	for _, row := range rows {
		best = selectSortValue(best, ScalarFromRow(row, kind), desc)
	}
	return best
}

func selectSortValue(best, value Scalar, desc bool) Scalar {
	if !value.Present {
		return best
	}
	order := value.Compare(best)
	if !best.Present || (!desc && order < 0) || (desc && order > 0) {
		return value
	}
	return best
}

func (left Scalar) CompareSort(right Scalar, desc, nullsLast bool) int {
	switch {
	case !left.Present && !right.Present:
		return 0
	case !left.Present:
		if nullsLast || desc {
			return 1
		}
		return -1
	case !right.Present:
		if nullsLast || desc {
			return -1
		}
		return 1
	}
	order := left.Compare(right)
	if desc {
		return -order
	}
	return order
}

func (left Scalar) Matches(right Scalar, op string) bool {
	if !left.Present || !right.Present {
		return false
	}
	order := left.Compare(right)
	switch op {
	case "eq", "in":
		return order == 0
	case "gt":
		return order > 0
	case "gte":
		return order >= 0
	case "lt":
		return order < 0
	case "lte":
		return order <= 0
	}
	return false
}
