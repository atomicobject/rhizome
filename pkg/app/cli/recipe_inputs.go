package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseRecipeInputsJSON converts the JSON input envelope used by saved query
// recipes and views into the string representation consumed by their
// compilers. Structured values remain JSON so code mode and flag-based calls
// have the same input vocabulary.
func ParseRecipeInputsJSON(raw string) (map[string]string, error) {
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for rawKey, value := range payload {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return nil, fmt.Errorf("input keys must be non-empty")
		}
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case json.Number:
			out[key] = typed.String()
		case bool:
			out[key] = fmt.Sprint(typed)
		case nil:
			out[key] = ""
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				return nil, fmt.Errorf("input %q must be a string, number, boolean, array, object, or null", key)
			}
			out[key] = string(encoded)
		}
	}
	return out, nil
}
