package ontology

import (
	"fmt"
	"strings"
	"unicode"
)

type PropertyCase string

const (
	PropertyCaseKebab     PropertyCase = "KEBAB"
	PropertyCaseCamel     PropertyCase = "CAMEL"
	PropertyCaseSnake     PropertyCase = "SNAKE"
	PropertyCaseAsDefined PropertyCase = "AS_DEFINED"
)

func propertyCaseArg(v any, fallback PropertyCase) PropertyCase {
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	switch PropertyCase(strings.ToUpper(strings.TrimSpace(s))) {
	case PropertyCaseCamel:
		return PropertyCaseCamel
	case PropertyCaseSnake:
		return PropertyCaseSnake
	case PropertyCaseAsDefined:
		return PropertyCaseAsDefined
	case PropertyCaseKebab:
		return PropertyCaseKebab
	default:
		return fallback
	}
}

// DefaultPropertyName returns the authored property key for a field given the
// enclosing scope's PropertyCase. Exported for use from subpackages (e.g. the
// query resolver) that need to reconstruct inline-property keys lazily.
func DefaultPropertyName(fieldName string, propertyCase PropertyCase) string {
	return defaultPropertyName(fieldName, propertyCase)
}

func defaultPropertyName(fieldName string, propertyCase PropertyCase) string {
	name := strings.TrimSpace(fieldName)
	if name == "" {
		return ""
	}
	switch propertyCase {
	case PropertyCaseCamel:
		words := splitPropertyWords(name)
		if len(words) == 0 {
			return name
		}
		var b strings.Builder
		b.WriteString(words[0])
		for _, word := range words[1:] {
			if word == "" {
				continue
			}
			b.WriteString(strings.ToUpper(word[:1]))
			b.WriteString(word[1:])
		}
		return b.String()
	case PropertyCaseSnake:
		return strings.Join(splitPropertyWords(name), "_")
	case PropertyCaseAsDefined:
		return name
	case PropertyCaseKebab:
		fallthrough
	default:
		return strings.Join(splitPropertyWords(name), "-")
	}
}

func propertyNamesFromArgs(sourceArg any, sourcesArg any, fallback string) (string, []string, error) {
	names := make([]string, 0, 4)
	if source := stringArg(sourceArg, ""); source != "" {
		names = append(names, source)
	}
	more, err := propertyNameListArg(sourcesArg)
	if err != nil {
		return "", nil, err
	}
	names = append(names, more...)
	if len(names) == 0 && strings.TrimSpace(fallback) != "" {
		names = append(names, fallback)
	}
	names = dedupePropertyNames(names)
	if len(names) == 0 {
		return "", nil, nil
	}
	return names[0], append([]string(nil), names[1:]...), nil
}

func propertyNameListArg(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected list, got %T", v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("expected string entry, got %T", item)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func dedupePropertyNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

// FieldSourceNames returns the primary authored property name plus any aliases.
func FieldSourceNames(field *Field) []string {
	if field == nil {
		return nil
	}
	names := make([]string, 0, 1+len(field.SourceAliases))
	if strings.TrimSpace(field.Source) != "" {
		names = append(names, field.Source)
	}
	names = append(names, field.SourceAliases...)
	return dedupePropertyNames(names)
}

func splitPropertyWords(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var words []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, strings.ToLower(string(current)))
		current = current[:0]
	}
	runes := []rune(s)
	for i, r := range runes {
		if r == '-' || r == '_' || unicode.IsSpace(r) {
			flush()
			continue
		}
		if len(current) > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		current = append(current, r)
	}
	flush()
	return words
}
