package pushdown

import (
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// PathPredicateVariants returns the authored canonical path form used when
// matching a notePath/path predicate. The caller must not invent an extension:
// provider ownership is resolved before this SQL boundary.
func PathPredicateVariants(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{string(paths.NormalizeNotePath(value))}
}

// UnwrapWikilink strips `[[...]]` wrappers and aliases (`[[Path|Alias]]` →
// `Path`). Used for link-typed predicate values before resolution.
func UnwrapWikilink(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]]"), "[[")
		if idx := strings.Index(raw, "|"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

// FieldValueRow coerces a string predicate value into the typed shape stored
// in ontology_node_field_values. Numeric, boolean, date, and datetime fields
// gain typed bindings; link fields get the unwrapped target path. The
// `valueNormFn` is applied to populate ValueNorm, allowing callers to pick
// their normalization (graphql uses normalizeRootFilterValue; views use
// strings.ToLower for backward compatibility).
func FieldValueRow(field *ontology.Field, value string, valueNormFn func(string) string) codeanchor.IntelOntologyNodeFieldValue {
	value = strings.TrimSpace(value)
	if valueNormFn == nil {
		valueNormFn = strings.ToLower
	}
	row := codeanchor.IntelOntologyNodeFieldValue{ValueText: value, ValueNorm: valueNormFn(value)}
	if field == nil {
		return row
	}
	if field.Kind == ontology.FieldKindLink {
		row.TargetNotePath = UnwrapWikilink(value)
		return row
	}
	switch strings.ToLower(strings.TrimSpace(field.TypeName)) {
	case "boolean", "bool":
		if parsed, err := strconv.ParseBool(strings.ToLower(value)); err == nil {
			row.ValueBool = &parsed
		}
	case "int", "integer":
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			row.ValueInt = &parsed
		}
	case "float", "number":
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			row.ValueReal = &parsed
		}
	case "date":
		if len(value) == len("2006-01-02") {
			row.ValueDate = &value
		}
	case "datetime":
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			canonical := parsed.Format(time.RFC3339)
			row.ValueDateTime = &canonical
			row.ValueNorm = valueNormFn(canonical)
		}
	}
	return row
}
