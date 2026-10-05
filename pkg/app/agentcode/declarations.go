package agentcode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

func generateDeclarations(description DescribeResponse) (string, error) {
	var out strings.Builder
	out.WriteString("export interface CallOptions { signal?: AbortSignal; timeoutMs?: number; sessionId?: string; }\n")
	out.WriteString("export interface ClientOptions { executablePath: string; vaultPath: string; sessionId?: string; readWrite?: boolean; concurrency?: number; timeoutMs?: number; outputLimitBytes?: number; }\n")
	out.WriteString("export interface CallOutcome<T> { ok: boolean; exitCode: number; payload: T | null; stdout: string; stderr: string; diagnostic: unknown; }\n")
	out.WriteString("export class CodeModeError extends Error { readonly code: string; readonly details: unknown; }\n\n")
	methods := make([]string, 0, len(description.Operations))
	for _, operation := range description.Operations {
		prefix := typePrefix(operation.Name)
		input, inputDefs, err := projectRootSchema(prefix+"Input", operation.InputSchema)
		if err != nil {
			return "", fmt.Errorf("project %s input: %w", operation.Name, err)
		}
		output, outputDefs, err := projectRootSchema(prefix+"Output", operation.OutputSchema)
		if err != nil {
			return "", fmt.Errorf("project %s output: %w", operation.Name, err)
		}
		out.WriteString(inputDefs)
		out.WriteString(input)
		out.WriteString(outputDefs)
		out.WriteString(output)
		method := camelCase(operation.Name)
		methods = append(methods, fmt.Sprintf("  %s(input: %sInput, options?: CallOptions): Promise<CallOutcome<%sOutput>>;", method, prefix, prefix))
	}
	out.WriteString("export interface Client {\n")
	for _, method := range methods {
		out.WriteString(method + "\n")
	}
	out.WriteString("  close(): Promise<void>;\n}\n")
	out.WriteString("export function createClient(options: ClientOptions): Client;\n")
	return out.String(), nil
}

func camelCase(value string) string {
	if value == "" {
		return ""
	}
	var out strings.Builder
	upper := false
	for _, r := range value {
		if r == '_' || r == '-' {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		out.WriteRune(r)
	}
	return out.String()
}

func projectRootSchema(name string, raw json.RawMessage) (string, string, error) {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return "", "", err
	}
	defs, _ := schema["$defs"].(map[string]any)
	aliases := make(map[string]string, len(defs))
	keys := make([]string, 0, len(defs))
	for key := range defs {
		keys = append(keys, key)
		aliases[key] = name + typePrefix(key)
	}
	sort.Strings(keys)
	var definitions strings.Builder
	for _, key := range keys {
		typeText, err := projectSchema(defs[key], aliases)
		if err != nil {
			return "", "", err
		}
		definitions.WriteString("export type " + aliases[key] + " = " + typeText + ";\n")
	}
	typeText, err := projectSchema(schema, aliases)
	if err != nil {
		return "", "", err
	}
	return "export type " + name + " = " + typeText + ";\n\n", definitions.String(), nil
}

func projectSchema(raw any, aliases map[string]string) (string, error) {
	schema, ok := raw.(map[string]any)
	if !ok || len(schema) == 0 {
		return "unknown", nil
	}
	if ref, ok := schema["$ref"].(string); ok {
		const prefix = "#/$defs/"
		if !strings.HasPrefix(ref, prefix) || aliases[strings.TrimPrefix(ref, prefix)] == "" {
			return "", fmt.Errorf("unsupported schema reference %q", ref)
		}
		return aliases[strings.TrimPrefix(ref, prefix)], nil
	}
	if values, ok := schema["enum"].([]any); ok {
		parts := make([]string, 0, len(values))
		for _, value := range values {
			encoded, _ := json.Marshal(value)
			parts = append(parts, string(encoded))
		}
		return strings.Join(parts, " | "), nil
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		if variants, ok := schema[key].([]any); ok {
			parts := make([]string, 0, len(variants))
			for _, variant := range variants {
				part, err := projectSchema(variant, aliases)
				if err != nil {
					return "", err
				}
				parts = append(parts, part)
			}
			return strings.Join(parts, " | "), nil
		}
	}
	if types, ok := schema["type"].([]any); ok {
		parts := make([]string, 0, len(types))
		for _, typ := range types {
			variant := make(map[string]any, len(schema))
			for key, value := range schema {
				variant[key] = value
			}
			variant["type"] = typ
			part, err := projectSchema(variant, aliases)
			if err != nil {
				return "", err
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, " | "), nil
	}
	switch schema["type"] {
	case "null":
		return "null", nil
	case "string":
		return "string", nil
	case "integer", "number":
		return "number", nil
	case "boolean":
		return "boolean", nil
	case "array":
		item, err := projectSchema(schema["items"], aliases)
		if err != nil {
			return "", err
		}
		if strings.Contains(item, " | ") {
			item = "(" + item + ")"
		}
		return item + "[]", nil
	case "object":
		properties, _ := schema["properties"].(map[string]any)
		required := map[string]bool{}
		if values, ok := schema["required"].([]any); ok {
			for _, value := range values {
				if key, ok := value.(string); ok {
					required[key] = true
				}
			}
		}
		keys := make([]string, 0, len(properties))
		for key := range properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys)+1)
		for _, key := range keys {
			typeText, err := projectSchema(properties[key], aliases)
			if err != nil {
				return "", err
			}
			optional := "?"
			if required[key] {
				optional = ""
			}
			encoded, _ := json.Marshal(key)
			parts = append(parts, string(encoded)+optional+": "+typeText+";")
		}
		switch additional := schema["additionalProperties"].(type) {
		case bool:
			if additional {
				parts = append(parts, "[key: string]: unknown;")
			}
		case map[string]any:
			value, err := projectSchema(additional, aliases)
			if err != nil {
				return "", err
			}
			parts = append(parts, "[key: string]: "+value+";")
		}
		return "{ " + strings.Join(parts, " ") + " }", nil
	default:
		return "unknown", nil
	}
}

func typePrefix(value string) string {
	var out strings.Builder
	upper := true
	for _, r := range value {
		if r == '_' || r == '-' {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		out.WriteRune(r)
	}
	return out.String()
}
