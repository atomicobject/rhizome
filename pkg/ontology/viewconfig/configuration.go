package viewconfig

import (
	"fmt"
	"time"
)

// jsonValue copies configuration into JSON-encodable form. yaml.v3 decodes a
// nested map with any non-string key (`1:`, `true:`, `2026-01-02:`, `~:`) as
// map[interface{}]interface{}; yamlKeyText turns those keys back into text.
func jsonValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = jsonValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[yamlKeyText(key)] = jsonValue(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = jsonValue(item)
		}
		return out
	default:
		return value
	}
}

// yamlKeyText approximates the authored scalar for a decoded non-string key.
func yamlKeyText(key any) string {
	switch key := key.(type) {
	case nil:
		return "null"
	case time.Time:
		if key.Equal(key.Truncate(24*time.Hour)) && key.Location() == time.UTC {
			return key.Format(time.DateOnly)
		}
		return key.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(key)
	}
}
