package ontology

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"math"
	"strconv"
	"strings"
	"time"
)

// FieldValueRow applies the same coercion to catalog values and query operands.
func FieldValueRow(field *ontoschema.Field, raw string) readmodel.FieldValueRow {
	text := strings.TrimSpace(raw)
	row := readmodel.FieldValueRow{
		ValueText: text,
		ValueNorm: normalizeOntologyFieldValue(text),
		ValueKind: "string",
	}
	if field == nil {
		return row
	}
	if field.IsIdentifier {
		row.ValueNorm = ontoschema.NormalizeIdentifierSemanticValue(text)
	}
	switch field.Kind {
	case ontoschema.FieldKindEnum:
		row.ValueKind = "enum"
		return row
	case ontoschema.FieldKindLink:
		row.ValueKind = "link"
		row.TargetNotePath = UnwrapLinkValue(text)
		row.ValueNorm = normalizeOntologyFieldValue(row.TargetNotePath)
		return row
	}
	value, ok := ontoschema.ParseScalar(field, text)
	if !ok {
		if field.Kind == ontoschema.FieldKindScalar && ontoschema.ScalarKindForType(field.TypeName) == ontoschema.ScalarKindReal {
			if number, err := strconv.ParseFloat(text, 64); err == nil && math.IsInf(number, 0) {
				row.ValueKind = string(ontoschema.ScalarKindReal)
				row.ValueReal = &number
			}
		}
		return row
	}
	row.ValueKind = string(value.Kind)
	switch value.Kind {
	case ontoschema.ScalarKindBoolean:
		row.ValueBool = &value.Bool
	case ontoschema.ScalarKindInt:
		row.ValueInt = &value.Int
	case ontoschema.ScalarKindReal:
		row.ValueReal = &value.Real
	case ontoschema.ScalarKindDate:
		row.ValueDate = &value.Canonical
		row.ValueNorm = normalizeOntologyFieldValue(value.Canonical)
	case ontoschema.ScalarKindDateTime:
		row.ValueDateTime = &value.Canonical
		row.ValueNorm = normalizeOntologyFieldValue(value.Canonical)
	}
	return row
}

// FieldTextValues preserves a DateTime timestamp even when YAML decoded midnight.
func FieldTextValues(field *ontoschema.Field, raw any) []string {
	if !scalarFieldSourceSupported(raw) {
		return nil
	}
	switch value := raw.(type) {
	case nil:
		return nil
	case []string:
		return normalizeStringValues(value)
	case []any:
		var values []string
		for _, item := range value {
			values = append(values, FieldTextValues(field, item)...)
		}
		return values
	case bool:
		return []string{strconv.FormatBool(value)}
	case int:
		return []string{strconv.Itoa(value)}
	case int64:
		return []string{strconv.FormatInt(value, 10)}
	case float64:
		return []string{strconv.FormatFloat(value, 'f', -1, 64)}
	case time.Time:
		if field != nil && ontoschema.ScalarKindForType(field.TypeName) == ontoschema.ScalarKindDateTime {
			return []string{value.Format(time.RFC3339Nano)}
		}
		if value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0 {
			return []string{value.Format("2006-01-02")}
		}
		return []string{value.Format(time.RFC3339Nano)}
	case string:
		return normalizeStringValues([]string{value})
	case json.Number:
		return []string{value.String()}
	default:
		return nil
	}
}

func scalarFieldSourceSupported(raw any) bool {
	switch value := raw.(type) {
	case map[string]any, map[any]any:
		return false
	case []any:
		for _, item := range value {
			switch item.(type) {
			case []any, []string, map[string]any, map[any]any:
				return false
			}
		}
	}
	return true
}
