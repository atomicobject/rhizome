package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/validate"
)

// CodeModeOutcome is the transport-neutral result of a local code-mode workflow.
type CodeModeOutcome struct {
	OK         bool
	ExitCode   int
	Payload    any
	Stdout     string
	Stderr     string
	Diagnostic any
}

func CodeModeSuccess(payload any) CodeModeOutcome { return codeModeOutcome(payload, 0) }

func CodeModeTextSuccess(text string) CodeModeOutcome {
	return CodeModeOutcome{OK: true, Payload: text}
}

func CodeModeOutcomeForExit(payload any, exitCode int) CodeModeOutcome {
	return codeModeOutcome(payload, exitCode)
}

func CodeModeFailure(err error, exitCode int) CodeModeOutcome {
	return codeModeFailure(err, exitCode)
}

func codeModeSuccess(payload any) CodeModeOutcome { return CodeModeSuccess(payload) }

func codeModeOutcome(payload any, exitCode int) CodeModeOutcome {
	if _, err := json.Marshal(payload); err != nil {
		return codeModeFailure(err, 1)
	}
	// Payload is the structured result; repeating it as stdout text doubles what the agent reads.
	return CodeModeOutcome{OK: exitCode == 0, ExitCode: exitCode, Payload: payload}
}

func codeModeFailure(err error, exitCode int) CodeModeOutcome {
	diagnostic := any(map[string]any{"error": err.Error()})
	if decoded := new(any); json.Unmarshal([]byte(err.Error()), decoded) == nil {
		diagnostic = *decoded
	}
	data, _ := json.Marshal(diagnostic)
	return CodeModeOutcome{ExitCode: exitCode, Stderr: string(data) + "\n", Diagnostic: diagnostic}
}

type CodeModeQueryRecipeService struct{ Recipes QueryRecipeService }

func (s CodeModeQueryRecipeService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	action := codeModeAction(input)
	path := codeModeString(input, "path")
	switch action {
	case "list":
		response, err := s.Recipes.List(path)
		if err != nil {
			return codeModeFailure(err, 1)
		}
		return codeModeSuccess(response)
	case "validate":
		response, err := s.Recipes.Validate(path)
		if err != nil {
			return codeModeFailure(err, 1)
		}
		if response.OK {
			return codeModeSuccess(response)
		}
		return codeModeOutcome(response, 1)
	case "run":
		inputs, err := codeModeStringMap(input, "inputs")
		if err != nil {
			return codeModeFailure(err, 1)
		}
		response, validation, err := s.Recipes.Run(ctx, path, codeModeString(input, "id"), inputs, codeModeStrings(input, "anchor"))
		if err != nil {
			return codeModeFailure(err, 1)
		}
		if validation != nil {
			return codeModeOutcome(validation, 1)
		}
		exit := 0
		if len(response.Result.Errors) > 0 {
			exit = 1
		}
		return codeModeOutcome(response, exit)
	default:
		return codeModeFailure(fmt.Errorf("query_recipe action must be list, validate, or run"), 1)
	}
}

type CodeModeViewRuntime interface {
	Catalog(context.Context) (appviews.Catalog, error)
	View(context.Context, string) (appviews.CatalogEntry, error)
	Execute(context.Context, string, appviews.ExecuteRequest) (appviews.ExecuteResponse, error)
	Eject(context.Context, string) (appviews.EjectResult, error)
}

type CodeModeViewFactory interface {
	Open(context.Context, string) (CodeModeViewRuntime, func(), error)
}

type CodeModeViewService struct {
	Factory   CodeModeViewFactory
	ReadWrite bool
}

type CodeModeViewValidation struct {
	OK         bool               `json:"ok"`
	IssueCount int                `json:"issueCount"`
	Catalog    appviews.Catalog   `json:"catalog"`
	Issues     []viewconfig.Issue `json:"issues,omitempty"`
}

func (s CodeModeViewService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	runtime, cleanup, err := s.Factory.Open(ctx, codeModeString(input, "path"))
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return codeModeFailure(err, 1)
	}
	switch codeModeAction(input) {
	case "list":
		catalog, err := runtime.Catalog(ctx)
		if err != nil {
			return codeModeFailure(err, 1)
		}
		return codeModeSuccess(catalog)
	case "show":
		entry, err := runtime.View(ctx, codeModeString(input, "id"))
		if err != nil {
			return codeModeFailure(err, 1)
		}
		return codeModeSuccess(entry)
	case "validate":
		catalog, err := runtime.Catalog(ctx)
		if err != nil {
			return codeModeFailure(err, 1)
		}
		response := CodeModeViewValidation{OK: len(catalog.Issues) == 0, IssueCount: len(catalog.Issues), Catalog: catalog, Issues: catalog.Issues}
		if response.OK {
			return codeModeSuccess(response)
		}
		return codeModeOutcome(response, 1)
	case "run":
		request, err := BuildCodeModeViewRequest(input)
		if err != nil {
			return codeModeFailure(err, 1)
		}
		response, err := runtime.Execute(ctx, codeModeString(input, "id"), request)
		if err != nil {
			return codeModeFailure(err, 1)
		}
		if codeModeBool(input, "omitCapabilities") {
			response.Capabilities = nil
		}
		return codeModeSuccess(response)
	case "eject":
		if !s.ReadWrite {
			return codeModeFailure(fmt.Errorf(`{"code":"write_requires_read_write","message":"view eject requires a read-write code-mode connection"}`), 1)
		}
		result, err := runtime.Eject(ctx, codeModeString(input, "id"))
		if err != nil {
			return codeModeFailure(err, 1)
		}
		return codeModeSuccess(result)
	default:
		return codeModeFailure(fmt.Errorf("view action must be list, show, validate, run, or eject"), 1)
	}
}

func BuildCodeModeViewRequest(input map[string]any) (appviews.ExecuteRequest, error) {
	inputs, err := codeModeStringMap(input, "inputs")
	if err != nil {
		return appviews.ExecuteRequest{}, err
	}
	request := appviews.ExecuteRequest{
		Variant: codeModeString(input, "variant"), Search: codeModeString(input, "search"),
		Page: appviews.PageRequest{Offset: codeModeInt(input, "offset"), First: codeModeInt(input, "first")}, Inputs: inputs,
	}
	if group := codeModeString(input, "group"); group != "" {
		request.Group = &viewconfig.GroupSpec{Field: group}
	}
	_, offsetSet := input["offset"]
	_, firstSet := input["first"]
	request.PageSet = offsetSet || firstSet
	if err := codeModeDecode(input, "filters", &request.Filters); err != nil {
		return appviews.ExecuteRequest{}, fmt.Errorf("filters: %w", err)
	}
	if err := codeModeDecode(input, "sort", &request.Sort); err != nil {
		return appviews.ExecuteRequest{}, fmt.Errorf("sort: %w", err)
	}
	return request, nil
}

type CodeModeValidationService struct {
	Runner    ValidationProductRunner
	ReadWrite bool
}

func (s CodeModeValidationService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	action := codeModeAction(input)
	if action == "list" {
		return codeModeSuccess(BuildValidationCatalog())
	}
	if action != "run" && action != "fix" {
		return codeModeValidationFailure(fmt.Errorf("validate action must be run, list, or fix"))
	}
	apply := action == "fix" && codeModeBool(input, "apply")
	if apply && !s.ReadWrite {
		return codeModeValidationFailure(fmt.Errorf("validate fix apply requires a read-write code-mode connection"))
	}
	selector := codeModeString(input, "selector")
	selectors := []string(nil)
	if selector != "" {
		selectors = []string{selector}
	}
	request := ValidationProductRequest{
		Selectors: selectors, Surface: validate.SurfaceAgent,
		MaxIssues: codeModeIntDefault(input, "maxIssues", 20), SkipAnchors: codeModeBool(input, "skipAnchors"),
		SkipEmbeds: codeModeBool(input, "skipEmbeds"), IncludeImages: codeModeBool(input, "includeImages"),
		ScopeNote: codeModeString(input, "scopeNote"), ScopeTarget: codeModeString(input, "scopeTarget"), ScopeRef: codeModeString(input, "scopeRef"),
		Repair: action == "fix", Apply: apply, AllowHistorical: codeModeBool(input, "allowHistorical"), NonInteractive: true,
	}
	request.ApplyCommand = BuildValidationProductApplyCommand(request)
	result, err := RunValidationProduct(ctx, s.Runner, request)
	if err != nil {
		return codeModeValidationFailure(err)
	}
	return codeModeOutcome(result, result.ExitCode())
}

func codeModeValidationFailure(err error) CodeModeOutcome {
	diagnostic := map[string]any{
		"ok": false, "error": err.Error(), "exitCode": validationrun.ValidationExitFailure,
	}
	return codeModeDiagnosticFailure(diagnostic, validationrun.ValidationExitFailure)
}

func codeModeAction(input map[string]any) string {
	if action := codeModeString(input, "action"); action != "" {
		return action
	}
	return codeModeString(input, "op")
}

func codeModeString(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}
func codeModeBool(input map[string]any, key string) bool { value, _ := input[key].(bool); return value }
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
func codeModeIntDefault(input map[string]any, key string, fallback int) int {
	if _, ok := input[key]; !ok {
		return fallback
	}
	return codeModeInt(input, key)
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
func codeModeStringMap(input map[string]any, key string) (map[string]string, error) {
	raw, ok := input[key].(map[string]any)
	if input[key] == nil {
		return map[string]string{}, nil
	}
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return ParseRecipeInputsJSON(string(encoded))
}
func codeModeDecode(input map[string]any, key string, target any) error {
	value, ok := input[key]
	if !ok || value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
