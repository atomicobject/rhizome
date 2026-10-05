package views

import (
	"cmp"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func sortRows(rows []TableRow, specs []viewconfig.SortSpec, capabilities []FieldCapability) {
	if len(specs) == 0 || len(rows) < 2 {
		return
	}
	caps := capabilityMap(capabilities)
	// Keep keys attached to original row positions while sorting an index slice.
	// Hydrated scalar parsing and repeated-value reduction happen once per row.
	order := make([]int, len(rows))
	values := make([]any, len(rows)*len(specs))
	for i, row := range rows {
		order[i] = i
		for j, spec := range specs {
			value, _ := fieldValue(row, spec.Field)
			if cap := caps[spec.Field]; indexedScalarSort(cap) {
				descending := strings.EqualFold(spec.Direction, "desc")
				if cap.ValueKind == "int" || cap.ValueKind == "real" {
					value = indexedNumericValue(value, cap.ValueKind, descending)
				} else {
					text, valid := indexedLexicalValue(value, cap.ValueKind, descending)
					value = nil
					if valid {
						value = text
					}
				}
			}
			values[i*len(specs)+j] = value
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		for k, spec := range specs {
			left := values[order[i]*len(specs)+k]
			right := values[order[j]*len(specs)+k]
			var comparison int
			if cap := caps[spec.Field]; indexedScalarSort(cap) {
				// Indexed scalar sorts keep missing values last in either direction.
				if left == nil || right == nil {
					if left == nil && right == nil {
						continue
					}
					return right == nil
				}
				switch cap.ValueKind {
				case "int":
					comparison = cmp.Compare(left.(int64), right.(int64))
				case "real":
					comparison = cmp.Compare(left.(float64), right.(float64))
				default:
					comparison = strings.Compare(left.(string), right.(string))
				}
			} else {
				comparison = compareSortValues(left, right)
			}
			if comparison == 0 {
				continue
			}
			if strings.EqualFold(spec.Direction, "desc") {
				return comparison > 0
			}
			return comparison < 0
		}
		return nodeRefKey(rows[order[i]].Ref) < nodeRefKey(rows[order[j]].Ref)
	})
	sorted := make([]TableRow, len(rows))
	for i, original := range order {
		sorted[i] = rows[original]
	}
	copy(rows, sorted)
}

func indexedScalarSort(cap FieldCapability) bool {
	if !cap.IndexedSortable {
		return false
	}
	switch cap.ValueKind {
	case "string", "enum", "id", "url", "date", "datetime", "int", "real":
		return true
	default:
		return false
	}
}

// indexedSortText matches ontology_node_field_values.value_norm, including
// authored link wrappers and aliases, even when the field is declared String.
func indexedSortText(value any) string {
	text := strings.ToLower(strings.TrimSpace(valueString(value)))
	text = strings.TrimPrefix(text, "[[")
	text = strings.TrimSuffix(text, "]]")
	if index := strings.Index(text, "|"); index >= 0 {
		text = strings.TrimSpace(text[:index])
	}
	return text
}

// Dates use the index's text columns. DateTime retains its authored offset and
// RFC3339 precision, rather than comparing instants or fractional seconds.
func indexedLexicalValue(value any, kind string, descending bool) (string, bool) {
	// SQL selects MIN/MAX after normalizing each repeated field value.
	if values, ok := listValues(value); ok {
		var selected string
		var valid bool
		for _, raw := range values {
			candidate, candidateValid := indexedLexicalValue(raw, kind, descending)
			if !candidateValid {
				continue
			}
			if !valid || (!descending && candidate < selected) || (descending && candidate > selected) {
				selected, valid = candidate, true
			}
		}
		return selected, valid
	}
	if value == nil {
		return "", false
	}
	if kind == "date" || kind == "datetime" {
		text := strings.TrimSpace(valueString(value))
		layout := time.RFC3339
		if kind == "date" {
			layout = "2006-01-02"
			if len(text) != len(layout) {
				return "", false
			}
		}
		parsed, err := time.Parse(layout, text)
		if err != nil {
			return "", false
		}
		return parsed.Format(layout), true
	}
	return indexedSortText(value), true
}

// Match typed index columns: parse failures and SQLite's NaN binding are NULL.
// Keep Int values as int64 so adjacent large integers remain distinct.
func indexedNumericValue(value any, kind string, descending bool) any {
	// SQL sorts repeated field values by their MIN or MAX.
	if values, ok := listValues(value); ok {
		var selected any
		for _, raw := range values {
			candidate := indexedNumericValue(raw, kind, descending)
			if candidate == nil {
				continue
			}
			if selected == nil {
				selected = candidate
				continue
			}
			var comparison int
			if kind == "int" {
				comparison = cmp.Compare(candidate.(int64), selected.(int64))
			} else {
				comparison = cmp.Compare(candidate.(float64), selected.(float64))
			}
			if (!descending && comparison < 0) || (descending && comparison > 0) {
				selected = candidate
			}
		}
		return selected
	}

	text := strings.TrimSpace(valueString(value))
	if kind == "int" {
		if number, err := strconv.ParseInt(text, 10, 64); err == nil {
			return number
		}
		return nil
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil && !math.IsNaN(number) {
		return number
	}
	return nil
}
