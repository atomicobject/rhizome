package schema

import "strings"

func validPropertyPath(path string) bool {
	for _, key := range strings.Split(path, ".") {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "*[]") {
			return false
		}
	}
	return true
}

// FrontmatterPathValue traverses only mappings. Each segment uses the same
// exact-first, lexical case-insensitive fallback as literal frontmatter reads.
func FrontmatterPathValue(values map[string]any, path string) (any, bool) {
	var value any = values
	for _, key := range strings.Split(path, ".") {
		mapping, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = mapping[key]
		if !ok {
			matched, found := "", false
			for candidate := range mapping {
				if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(key)) && (!found || candidate < matched) {
					matched, found = candidate, true
				}
			}
			if !found {
				return nil, false
			}
			value = mapping[matched]
		}
		if value == nil {
			return nil, false
		}
	}
	return value, true
}
