package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
)

func (d *Driver) Generate(ctx context.Context, request harness.GenerateRequest) (json.RawMessage, error) {
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		return nil, harness.Phase(harness.ErrNotInstalled, err)
	}
	if _, err := d.checkedVersion(ctx, path); err != nil {
		return nil, err
	}
	outputFile, err := os.CreateTemp("", "rhizome-codex-output-*.json")
	if err != nil {
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	outputPath := outputFile.Name()
	_ = outputFile.Close()
	defer os.Remove(outputPath)

	args := []string{"exec", "--ephemeral", "--skip-git-repo-check", "-s", "read-only"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.Effort != "" {
		encoded, _ := json.Marshal(request.Effort)
		args = append(args, "--config", "model_reasoning_effort="+string(encoded))
	}
	var schemaPath string
	if len(request.Schema) > 0 {
		if !json.Valid(request.Schema) {
			return nil, harness.Phase(harness.ErrSchemaMismatch, errors.New("schema is not valid JSON"))
		}
		schemaFile, err := os.CreateTemp("", "rhizome-codex-schema-*.json")
		if err != nil {
			return nil, harness.Phase(harness.ErrSpawn, err)
		}
		schemaPath = schemaFile.Name()
		if _, err := schemaFile.Write(request.Schema); err != nil {
			_ = schemaFile.Close()
			_ = os.Remove(schemaPath)
			return nil, harness.Phase(harness.ErrSpawn, err)
		}
		_ = schemaFile.Close()
		defer os.Remove(schemaPath)
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, "--output-last-message", outputPath, "-")
	result, err := d.runner.Run(ctx, command.Spec{
		Path:  path,
		Args:  args,
		Stdin: strings.NewReader(request.Prompt),
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, harness.Phase(harness.ErrTimeout, ctx.Err())
		}
		if isNotFound(err) {
			return nil, harness.Phase(harness.ErrNotInstalled, err)
		}
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if looksLikeAuthFailure(detail) {
			return nil, harness.Phase(harness.ErrNotLoggedIn, errors.New(detail))
		}
		return nil, harness.Phase(harness.ErrCommandFailed, fmt.Errorf("exit %d: %s", result.ExitCode, detail))
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, harness.Phase(harness.ErrDecode, err)
	}
	output = []byte(strings.TrimSpace(string(output)))
	if len(output) == 0 {
		return nil, harness.Phase(harness.ErrDecode, errors.New("codex did not write a last message"))
	}
	if !json.Valid(output) {
		if len(request.Schema) > 0 {
			return nil, harness.Phase(harness.ErrSchemaMismatch, errors.New("last message is not valid JSON"))
		}
		output, _ = json.Marshal(string(output))
	}
	if len(request.Schema) > 0 {
		if err := validateSchema(request.Schema, output); err != nil {
			return nil, harness.Phase(harness.ErrSchemaMismatch, err)
		}
	}
	return json.RawMessage(output), nil
}

func looksLikeAuthFailure(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "not logged in") ||
		strings.Contains(message, "authentication required") ||
		strings.Contains(message, "login required") ||
		strings.Contains(message, "codex login")
}

func validateSchema(schemaJSON, valueJSON []byte) error {
	var schema any
	var value any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return err
	}
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		return err
	}
	return validateValue(schema, value, "$", schema)
}

func validateValue(schema, value any, path string, root any) error {
	if allowed, ok := schema.(bool); ok {
		if allowed {
			return nil
		}
		return fmt.Errorf("%s is rejected by schema", path)
	}
	rules, ok := schema.(map[string]any)
	if !ok {
		return errors.New("schema must be an object or boolean")
	}
	if ref, _ := rules["$ref"].(string); strings.HasPrefix(ref, "#/") {
		resolved, err := resolveRef(root, ref)
		if err != nil {
			return err
		}
		return validateValue(resolved, value, path, root)
	}
	for _, keyword := range []string{"oneOf", "anyOf"} {
		if choices, ok := rules[keyword].([]any); ok {
			for _, choice := range choices {
				if validateValue(choice, value, path, root) == nil {
					return nil
				}
			}
			return fmt.Errorf("%s does not match %s", path, keyword)
		}
	}
	if choices, ok := rules["enum"].([]any); ok {
		matched := false
		for _, choice := range choices {
			if fmt.Sprint(choice) == fmt.Sprint(value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is not an allowed value", path)
		}
	}
	if expected, _ := rules["type"].(string); expected != "" && !matchesType(expected, value) {
		return fmt.Errorf("%s must be %s", path, expected)
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := rules["properties"].(map[string]any)
		if required, ok := rules["required"].([]any); ok {
			for _, name := range required {
				key, _ := name.(string)
				if _, exists := object[key]; !exists {
					return fmt.Errorf("%s.%s is required", path, key)
				}
			}
		}
		for key, propertySchema := range properties {
			if propertyValue, exists := object[key]; exists {
				if err := validateValue(propertySchema, propertyValue, path+"."+key, root); err != nil {
					return err
				}
			}
		}
		if allow, exists := rules["additionalProperties"].(bool); exists && !allow {
			for key := range object {
				if _, known := properties[key]; !known {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
			}
		}
		if additional, ok := rules["additionalProperties"].(map[string]any); ok {
			for key, propertyValue := range object {
				if _, known := properties[key]; !known {
					if err := validateValue(additional, propertyValue, path+"."+key, root); err != nil {
						return err
					}
				}
			}
		}
	}
	if array, ok := value.([]any); ok {
		if itemSchema, exists := rules["items"]; exists {
			for i, item := range array {
				if err := validateValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i), root); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func matchesType(expected string, value any) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return true
	}
}

func resolveRef(root any, ref string) (any, error) {
	current := root
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema reference %q does not resolve", ref)
		}
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		current, ok = object[segment]
		if !ok {
			return nil, fmt.Errorf("schema reference %q does not resolve", ref)
		}
	}
	return current, nil
}
