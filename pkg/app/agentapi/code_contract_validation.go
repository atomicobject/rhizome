package agentapi

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// ValidateCodeInput validates the small JSON Schema subset emitted above.
func ValidateCodeInput(operation string, value map[string]any) error {
	descriptor, ok := CodeOperationDescriptor(operation)
	if !ok {
		return fmt.Errorf("unknown code operation %q", operation)
	}
	var schema map[string]any
	if err := json.Unmarshal(descriptor.CodeContract.InputSchema, &schema); err != nil {
		return fmt.Errorf("invalid catalog schema for %q: %w", operation, err)
	}
	if err := validateSchemaValue("input", value, schema); err != nil {
		return err
	}
	if descriptor.validateCodeInput != nil {
		return descriptor.validateCodeInput(value)
	}
	return nil
}

func requireInputString(value map[string]any, key string) error {
	if strings.TrimSpace(stringValue(value[key])) == "" {
		return fmt.Errorf("input.%s is required", key)
	}
	return nil
}
func stringValue(value any) string { text, _ := value.(string); return text }

func validateSchemaValue(path string, value any, schema map[string]any) error {
	if alternatives, ok := schema["oneOf"].([]any); ok {
		for _, raw := range alternatives {
			if candidate, ok := raw.(map[string]any); ok && validateSchemaValue(path, value, candidate) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s does not match an allowed shape", path)
	}
	switch schema["type"] {
	case "null":
		if value != nil {
			return fmt.Errorf("%s must be null", path)
		}
	case "object":
		objectValue, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		for _, required := range schemaArray(schema["required"]) {
			key, _ := required.(string)
			if _, present := objectValue[key]; !present {
				return fmt.Errorf("%s.%s is required", path, key)
			}
		}
		if additional, present := schema["additionalProperties"].(bool); present && !additional {
			unknown := []string{}
			for key := range objectValue {
				if _, known := properties[key]; !known {
					unknown = append(unknown, key)
				}
			}
			if len(unknown) > 0 {
				sort.Strings(unknown)
				return fmt.Errorf("%s contains unknown field %q", path, unknown[0])
			}
		}
		for key, child := range objectValue {
			if rawChild, present := properties[key]; present {
				if childSchema, ok := rawChild.(map[string]any); ok {
					if err := validateSchemaValue(path+"."+key, child, childSchema); err != nil {
						return err
					}
				}
			} else if rawAdditional, ok := schema["additionalProperties"].(map[string]any); ok {
				if err := validateSchemaValue(path+"."+key, child, rawAdditional); err != nil {
					return err
				}
			}
		}
	case "array":
		reflected := reflect.ValueOf(value)
		if !reflected.IsValid() || (reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array) {
			return fmt.Errorf("%s must be an array", path)
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i := 0; i < reflected.Len(); i++ {
				if err := validateSchemaValue(fmt.Sprintf("%s[%d]", path, i), reflected.Index(i).Interface(), itemSchema); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "integer":
		if number, ok := value.(float64); ok {
			if math.Trunc(number) != number {
				return fmt.Errorf("%s must be an integer", path)
			}
		} else if _, ok := value.(int); !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
	}
	if allowed := schemaArray(schema["enum"]); len(allowed) > 0 {
		actual, _ := value.(string)
		for _, candidate := range allowed {
			if actual == candidate {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %s", path, strings.Join(stringValues(allowed), ", "))
	}
	return nil
}
func schemaArray(value any) []any { items, _ := value.([]any); return items }
func stringValues(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
