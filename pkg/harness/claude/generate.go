package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	args := []string{"-p", "--output-format", "json"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.Effort != "" {
		args = append(args, "--effort", request.Effort)
	}
	args = append(args, "--tools", "", "--disable-slash-commands", "--strict-mcp-config", "--permission-mode", "dontAsk", "--settings", `{"disableAllHooks":true}`)
	if len(request.Schema) > 0 {
		if !json.Valid(request.Schema) {
			return nil, harness.Phase(harness.ErrSchemaMismatch, errors.New("schema is not valid JSON"))
		}
		args = append(args, "--json-schema", string(request.Schema))
	}
	result, err := d.runner.Run(ctx, command.Spec{Path: path, Args: args, Stdin: strings.NewReader(request.Prompt)})
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
	var output struct {
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		IsError          bool            `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &output); err != nil {
		return nil, harness.Phase(harness.ErrDecode, err)
	}
	if output.IsError {
		if looksLikeAuthFailure(output.Result) {
			return nil, harness.Phase(harness.ErrNotLoggedIn, errors.New(output.Result))
		}
		return nil, harness.Phase(harness.ErrCommandFailed, errors.New(output.Result))
	}
	if len(request.Schema) > 0 {
		if len(output.StructuredOutput) == 0 || string(output.StructuredOutput) == "null" || !json.Valid(output.StructuredOutput) {
			return nil, harness.Phase(harness.ErrSchemaMismatch, errors.New("Claude did not return valid structured_output"))
		}
		if err := validateStructuredOutput(request.Schema, output.StructuredOutput); err != nil {
			return nil, harness.Phase(harness.ErrSchemaMismatch, err)
		}
		return append(json.RawMessage(nil), output.StructuredOutput...), nil
	}
	encoded, _ := json.Marshal(output.Result)
	return encoded, nil
}

func validateStructuredOutput(schemaJSON, outputJSON []byte) error {
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return err
	}
	var output any
	if err := json.Unmarshal(outputJSON, &output); err != nil {
		return err
	}
	expected, _ := schema["type"].(string)
	switch expected {
	case "object":
		object, ok := output.(map[string]any)
		if !ok {
			return errors.New("structured_output must be an object")
		}
		if required, ok := schema["required"].([]any); ok {
			for _, value := range required {
				key, _ := value.(string)
				if key != "" {
					if _, exists := object[key]; !exists {
						return fmt.Errorf("structured_output.%s is required", key)
					}
				}
			}
		}
	case "array":
		if _, ok := output.([]any); !ok {
			return errors.New("structured_output must be an array")
		}
	}
	return nil
}

func looksLikeAuthFailure(value string) bool {
	value = strings.ToLower(value)
	for _, fragment := range []string{"not logged in", "authentication required", "login required", "claude auth login", "please log in", "oauth token"} {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
