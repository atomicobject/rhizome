package cmd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
)

func (commandOptions *vaultCommandOptions) callAgentCodeQueryRecipe(ctx context.Context, input map[string]any) agentcode.CallOutcome {
	service := actions.CodeModeQueryRecipeService{Recipes: commandOptions.newQueryRecipeService(ctx)}
	return codeModeWorkflowOutcome(service.Call(ctx, input))
}

type commandCodeModeViewFactory struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeViewFactory) Open(ctx context.Context, path string) (actions.CodeModeViewRuntime, func(), error) {
	return commandOptions.buildViewService(ctx, path)
}

func (commandOptions *vaultCommandOptions) callAgentCodeView(ctx context.Context, input map[string]any, readWrite bool) agentcode.CallOutcome {
	service := actions.CodeModeViewService{Factory: commandCodeModeViewFactory{vaultCommandOptions: commandOptions}, ReadWrite: readWrite}
	return codeModeWorkflowOutcome(service.Call(ctx, input))
}

func callAgentCodeValidate(ctx context.Context, input map[string]any, readWrite bool) agentcode.CallOutcome {
	service := actions.CodeModeValidationService{Runner: newProductionValidationRunner(), ReadWrite: readWrite}
	return codeModeWorkflowOutcome(service.Call(ctx, input))
}

func codeModeWorkflowOutcome(outcome actions.CodeModeOutcome) agentcode.CallOutcome {
	return agentcode.CallOutcome{
		OK: outcome.OK, ExitCode: outcome.ExitCode, Payload: outcome.Payload,
		Stdout: outcome.Stdout, Stderr: outcome.Stderr, Diagnostic: outcome.Diagnostic,
	}
}

func codeModeString(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func codeModeBool(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
}

func codeModeBoolDefault(input map[string]any, key string, fallback bool) bool {
	value, ok := input[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func codeModeInt(input map[string]any, key string) int {
	switch value := input[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func codeModeStrings(input map[string]any, key string) []string {
	switch values := input[key].(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}
