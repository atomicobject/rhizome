package actions

import (
	"context"
	"fmt"
	"strings"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
)

type CodeModeOntologySchemaRuntime interface {
	Discover(context.Context, string) (ontologyquery.SchemaDiscovery, error)
}

type CodeModeOntologySchemaService struct{ Runtime CodeModeOntologySchemaRuntime }

func (s CodeModeOntologySchemaService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	response, err := s.Runtime.Discover(ctx, codeModeString(input, "type"))
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return codeModeSuccess(response)
}

type CodeModeOntologyQueryRuntime interface {
	Execute(context.Context, string, map[string]any) (ontologyquery.Result, error)
}

type CodeModeOntologyQueryService struct{ Runtime CodeModeOntologyQueryRuntime }

func (s CodeModeOntologyQueryService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	rawQuery := codeModeString(input, "query")
	if rawQuery == "" {
		return codeModeFailure(fmt.Errorf("query is required"), 1)
	}
	variables, err := codeModeObject(input, "variables")
	if err != nil {
		return codeModeFailure(err, 1)
	}
	result, err := s.Runtime.Execute(ctx, rawQuery, variables)
	if err != nil {
		return codeModeFailure(err, 1)
	}
	exit := 0
	if len(result.Errors) > 0 {
		exit = 1
	}
	return codeModeOutcome(result, exit)
}

type CodeModeOntologyReferenceRuntime interface {
	Render(context.Context, string, bool) (any, error)
}

type CodeModeOntologyReferenceService struct {
	Runtime CodeModeOntologyReferenceRuntime
}

func (s CodeModeOntologyReferenceService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	response, err := s.Runtime.Render(ctx, codeModeString(input, "type"), codeModeBool(input, "compact"))
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return codeModeSuccess(response)
}

type CodeModeTextRuntime interface {
	Render(context.Context, []string) (string, error)
}

type CodeModeOntologyAuthoringService struct{ Runtime CodeModeTextRuntime }

func (s CodeModeOntologyAuthoringService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	requested := codeModeStrings(input, "types")
	if len(requested) == 0 {
		for _, item := range strings.Split(codeModeString(input, "type"), ",") {
			if item = strings.TrimSpace(item); item != "" {
				requested = append(requested, item)
			}
		}
	}
	text, err := s.Runtime.Render(ctx, requested)
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return CodeModeTextSuccess(text)
}

type CodeModeInspectRuntime interface {
	Inspect(context.Context, []string) (any, error)
}

type CodeModeOntologyInspectService struct{ Runtime CodeModeInspectRuntime }

func (s CodeModeOntologyInspectService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	inputs := codeModeStrings(input, "inputs")
	if len(inputs) == 0 {
		return codeModeFailure(fmt.Errorf("inputs is required"), 1)
	}
	response, err := s.Runtime.Inspect(ctx, inputs)
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return codeModeSuccess(response)
}

type CodeModeRationaleRuntime interface {
	Query(context.Context, string, []string) (any, error)
}

type CodeModeRationaleService struct{ Runtime CodeModeRationaleRuntime }

func (s CodeModeRationaleService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	response, err := s.Runtime.Query(ctx, codeModeString(input, "path"), codeModeStrings(input, "kinds"))
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return codeModeSuccess(response)
}

func codeModeObject(input map[string]any, key string) (map[string]any, error) {
	if input[key] == nil {
		return nil, nil
	}
	value, ok := input[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return value, nil
}
