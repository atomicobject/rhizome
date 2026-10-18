package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func extractFieldValues(doc *noteDoc, field *ontoschema.Field) []string {
	if doc == nil || field == nil {
		return nil
	}
	switch field.SourceKind {
	case ontoschema.FieldSourceInline:
		for _, name := range ontoschema.FieldSourceNames(field) {
			if values := normalizeStringValues(caseInsensitiveInlineValues(doc.Inline, name)); len(values) > 0 {
				return values
			}
		}
		return nil
	default:
		for _, name := range ontoschema.FieldSourceNames(field) {
			value := caseInsensitiveFrontmatterValue(doc.Frontmatter, name)
			if value == nil {
				continue
			}
			values := FieldTextValues(field, value)
			if len(values) > 0 || caseInsensitiveFrontmatterHasKey(doc.Frontmatter, name) {
				return values
			}
		}
		return nil
	}
}

func caseInsensitiveFrontmatterValue(values map[string]interface{}, key string) interface{} {
	if len(values) == 0 {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	matched := ""
	found := false
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle && (!found || currentKey < matched) {
			matched, found = currentKey, true
		}
	}
	if found {
		return values[matched]
	}

	return nil
}

func caseInsensitiveInlineValues(values map[string][]string, key string) []string {
	if len(values) == 0 {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	matched := ""
	found := false
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle && (!found || currentKey < matched) {
			matched, found = currentKey, true
		}
	}
	if found {
		return values[matched]
	}

	return nil
}

func fieldPresent(doc *noteDoc, field *ontoschema.Field) bool {
	if doc == nil || field == nil {
		return false
	}
	switch field.SourceKind {
	case ontoschema.FieldSourceInline:
		for _, name := range ontoschema.FieldSourceNames(field) {
			if caseInsensitiveInlineHasKey(doc.Inline, name) {
				return true
			}
		}
		return false
	default:
		for _, name := range ontoschema.FieldSourceNames(field) {
			if caseInsensitiveFrontmatterHasKey(doc.Frontmatter, name) {
				return true
			}
		}
		return false
	}
}

func caseInsensitiveFrontmatterHasKey(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	if _, ok := values[key]; ok {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return true
		}
	}
	return false
}

func caseInsensitiveInlineHasKey(values map[string][]string, key string) bool {
	if len(values) == 0 {
		return false
	}
	if _, ok := values[key]; ok {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return true
		}
	}
	return false
}

func normalizeStringValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func validateScalarValue(raw string, field *ontoschema.Field, schema *ontoschema.Schema) bool {
	raw = strings.TrimSpace(raw)
	switch field.Kind {
	case ontoschema.FieldKindEnum:
		return ontoschema.EnumTypeForName(schema, field.TypeName).HasValue(raw)
	case ontoschema.FieldKindScalar:
		_, ok := ontoschema.ParseScalar(field, raw)
		return ok
	default:
		return true
	}
}

// UnwrapLinkValue removes balanced wikilink markup and an authored alias.
// Plain values retain their literal punctuation.
func UnwrapLinkValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]]"), "[[")
		if idx := strings.Index(raw, "|"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(v interface{}) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
