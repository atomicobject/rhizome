package agentapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// CodeOperationContract is the selectively loaded application contract for a
// catalog operation. Transport envelopes are deliberately outside this type.
type CodeOperationContract struct {
	Summary        string          `json:"summary"`
	Effects        []string        `json:"effects"`
	InputSchema    json.RawMessage `json:"inputSchema"`
	OutputSchema   json.RawMessage `json:"outputSchema"`
	Example        map[string]any  `json:"example"`
	Interpretation []string        `json:"interpretation"`
}

func CodeOperationDescriptor(name string) (ToolDescriptor, bool) {
	for _, descriptor := range toolCatalog {
		if descriptor.Name == name && descriptor.CodeContract != nil {
			return cloneToolDescriptor(descriptor), true
		}
	}
	return ToolDescriptor{}, false
}

func CodeOperationDescriptors() []ToolDescriptor {
	var descriptors []ToolDescriptor
	for _, descriptor := range toolCatalog {
		if descriptor.CodeContract != nil {
			descriptors = append(descriptors, cloneToolDescriptor(descriptor))
		}
	}
	return descriptors
}

func cloneContractMap(value map[string]any) map[string]any {
	payload, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(payload, &cloned)
	return cloned
}

type codeSchema map[string]any

func str() codeSchema           { return codeSchema{"type": "string"} }
func boolean() codeSchema       { return codeSchema{"type": "boolean"} }
func integer() codeSchema       { return codeSchema{"type": "integer"} }
func stringsSchema() codeSchema { return codeSchema{"type": "array", "items": str()} }
func objectsSchema() codeSchema { return codeSchema{"type": "array", "items": unknownObject()} }
func unknownObject() codeSchema { return codeSchema{"type": "object", "additionalProperties": true} }
func unknownValue() codeSchema  { return codeSchema{} }
func enumeration(values ...string) codeSchema {
	items := make([]any, len(values))
	for i := range values {
		items[i] = values[i]
	}
	return codeSchema{"type": "string", "enum": items}
}
func object(required []string, properties map[string]codeSchema) codeSchema {
	p := map[string]any{}
	for key, value := range properties {
		p[key] = value
	}
	schema := codeSchema{"type": "object", "additionalProperties": false, "properties": p}
	if len(required) > 0 {
		r := make([]any, len(required))
		for i := range required {
			r[i] = required[i]
		}
		schema["required"] = r
	}
	return schema
}
func output(properties map[string]codeSchema) codeSchema {
	p := map[string]any{}
	for key, value := range properties {
		p[key] = value
	}
	return codeSchema{"type": "object", "additionalProperties": true, "properties": p}
}
func raw(schema codeSchema) json.RawMessage { payload, _ := json.Marshal(schema); return payload }
func input(extra map[string]codeSchema, required ...string) codeSchema {
	properties := map[string]codeSchema{"sessionId": str(), "budgetChars": integer()}
	for key, value := range extra {
		properties[key] = value
	}
	return object(required, properties)
}
func contract(summary string, in, out codeSchema, example map[string]any, effects ...string) *CodeOperationContract {
	return &CodeOperationContract{
		Summary: summary, Effects: effects, InputSchema: raw(in), OutputSchema: raw(out), Example: example,
	}
}

// codeSurfaceSchema describes the compact catalog returned by agent start.
// The full contracts remain available only through selected code describe calls.
func codeSurfaceSchema() codeSchema {
	operation := object([]string{"name", "method", "summary", "effects"}, map[string]codeSchema{
		"name": str(), "method": str(), "summary": str(), "effects": stringsSchema(),
	})
	return object([]string{"formatVersion", "versionHash", "operations", "examples"}, map[string]codeSchema{
		"formatVersion": str(), "versionHash": str(),
		"operations": {"type": "array", "items": operation}, "examples": stringsSchema(),
	})
}

// schemaForType projects the ordinary JSON-bearing Go shapes used by handler
// responses. Interfaces and maps remain dynamic at their exact field, rather
// than turning the entire operation result into an untyped object.
func schemaForType(typ reflect.Type, visiting map[reflect.Type]bool) codeSchema {
	if typ.Kind() == reflect.Pointer {
		return codeSchema{"anyOf": []any{schemaForType(typ.Elem(), visiting), codeSchema{"type": "null"}}}
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return unknownValue()
	}
	if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
		return codeSchema{"type": []any{"string", "null"}}
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return codeSchema{"type": "string", "format": "date-time"}
	}
	if visiting[typ] {
		return unknownObject()
	}
	switch typ.Kind() {
	case reflect.String:
		return str()
	case reflect.Bool:
		return boolean()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return integer()
	case reflect.Float32, reflect.Float64:
		return codeSchema{"type": "number"}
	case reflect.Slice:
		return codeSchema{"type": []any{"array", "null"}, "items": schemaForType(typ.Elem(), visiting)}
	case reflect.Array:
		return codeSchema{"type": "array", "items": schemaForType(typ.Elem(), visiting)}
	case reflect.Map:
		if typ.Key().Kind() == reflect.String {
			return codeSchema{"type": []any{"object", "null"}, "additionalProperties": schemaForType(typ.Elem(), visiting)}
		}
		return unknownObject()
	case reflect.Interface:
		return unknownValue()
	case reflect.Struct:
		visiting[typ] = true
		defer delete(visiting, typ)
		properties := map[string]codeSchema{}
		required := []string{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if field.Anonymous && name == "" {
				embedded := schemaForType(field.Type, visiting)
				if embeddedProperties, ok := embedded["properties"].(map[string]any); ok {
					for key, rawProperty := range embeddedProperties {
						if property, ok := rawProperty.(codeSchema); ok {
							properties[key] = property
						} else if property, ok := rawProperty.(map[string]any); ok {
							properties[key] = codeSchema(property)
						}
					}
				}
				for _, rawRequired := range schemaArray(embedded["required"]) {
					if key, ok := rawRequired.(string); ok {
						required = append(required, key)
					}
				}
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = schemaForType(field.Type, visiting)
			if options != "omitempty" && !strings.Contains(options, ",omitempty") {
				required = append(required, name)
			}
		}
		return object(required, properties)
	default:
		return unknownObject()
	}
}
