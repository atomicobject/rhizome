package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
)

func TestBuildCodeModeViewRequestPreservesStructuredInputsAndPageIntent(t *testing.T) {
	request, err := BuildCodeModeViewRequest(map[string]any{
		"inputs": map[string]any{
			"list": []any{"alpha", "beta"}, "enabled": false, "object": map[string]any{"nested": true},
			"string": "value", "number": float64(3), "nullable": nil,
		},
		"offset": float64(0), "filters": []any{map[string]any{"field": "status", "op": "eq", "value": "open"}},
	})
	require.NoError(t, err)
	require.JSONEq(t, `["alpha","beta"]`, request.Inputs["list"])
	require.JSONEq(t, `{"nested":true}`, request.Inputs["object"])
	require.Equal(t, "value", request.Inputs["string"])
	require.Equal(t, "3", request.Inputs["number"])
	require.Equal(t, "false", request.Inputs["enabled"])
	require.Contains(t, request.Inputs, "nullable")
	require.Empty(t, request.Inputs["nullable"])
	require.True(t, request.PageSet)
	require.Len(t, request.Filters, 1)
}

func TestCodeModeQueryRecipeServicePreservesDomainFailureOutcome(t *testing.T) {
	service := CodeModeQueryRecipeService{Recipes: QueryRecipeService{
		Load: func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
			return []queryrecipe.Recipe{{ID: "broken"}}, []queryrecipe.Issue{{Code: "invalid_recipe"}}, nil
		},
	}}
	outcome := service.Call(context.Background(), map[string]any{"action": "run", "id": "broken"})
	require.False(t, outcome.OK)
	require.Equal(t, 1, outcome.ExitCode)
	require.NotNil(t, outcome.Payload)
	require.Empty(t, outcome.Stdout, "payload must not be repeated as stdout")
	require.Empty(t, outcome.Stderr)
}

type codeModeViewRuntimeStub struct {
	catalog appviews.Catalog
	run     appviews.ExecuteRequest
	ejected string
}

func (s *codeModeViewRuntimeStub) Catalog(context.Context) (appviews.Catalog, error) {
	return s.catalog, nil
}
func (s *codeModeViewRuntimeStub) View(context.Context, string) (appviews.CatalogEntry, error) {
	return appviews.CatalogEntry{}, nil
}
func (s *codeModeViewRuntimeStub) Execute(_ context.Context, _ string, request appviews.ExecuteRequest) (appviews.ExecuteResponse, error) {
	s.run = request
	return appviews.ExecuteResponse{}, nil
}
func (s *codeModeViewRuntimeStub) Eject(_ context.Context, id string) (appviews.EjectResult, error) {
	s.ejected = id
	return appviews.EjectResult{ID: id}, nil
}

type codeModeViewFactoryStub struct {
	runtime *codeModeViewRuntimeStub
	closed  bool
}

func (s *codeModeViewFactoryStub) Open(context.Context, string) (CodeModeViewRuntime, func(), error) {
	return s.runtime, func() { s.closed = true }, nil
}

func TestCodeModeViewServiceMapsValidationIssuesAndClosesRuntime(t *testing.T) {
	factory := &codeModeViewFactoryStub{runtime: &codeModeViewRuntimeStub{catalog: appviews.Catalog{
		Issues: []viewconfig.Issue{{Code: "invalid_view"}},
	}}}
	outcome := (CodeModeViewService{Factory: factory}).Call(context.Background(), map[string]any{"action": "validate"})
	require.Equal(t, 1, outcome.ExitCode)
	require.False(t, outcome.OK)
	require.True(t, factory.closed)
	require.IsType(t, CodeModeViewValidation{}, outcome.Payload)
}

func TestCodeModeViewEjectRequiresReadWrite(t *testing.T) {
	runtime := &codeModeViewRuntimeStub{}
	input := map[string]any{"action": "eject", "id": "group.briefing"}
	readOnly := (CodeModeViewService{Factory: &codeModeViewFactoryStub{runtime: runtime}}).Call(context.Background(), input)
	require.False(t, readOnly.OK)
	require.Empty(t, runtime.ejected, "a read-only connection must not write")
	readWrite := (CodeModeViewService{Factory: &codeModeViewFactoryStub{runtime: runtime}, ReadWrite: true}).Call(context.Background(), input)
	require.True(t, readWrite.OK)
	require.Equal(t, "group.briefing", runtime.ejected)
}

type validationProductRunnerStub struct {
	requests []ValidationProductRequest
	result   ValidationResult
	err      error
}

func (s *validationProductRunnerStub) Run(_ context.Context, request ValidationProductRequest) (ValidationResult, error) {
	s.requests = append(s.requests, request)
	return s.result, s.err
}

func TestCodeModeValidationServiceBuildsNonInteractiveAgentRequest(t *testing.T) {
	runner := &validationProductRunnerStub{result: ValidationResult{Result: validate.Result{OK: true}}}
	outcome := (CodeModeValidationService{Runner: runner, ReadWrite: true}).Call(context.Background(), map[string]any{
		"action": "fix", "apply": true, "selector": "broken-links", "scopeNote": "Notes/A B.md", "maxIssues": float64(31),
	})
	require.True(t, outcome.OK)
	require.Len(t, runner.requests, 1)
	request := runner.requests[0]
	require.Equal(t, validate.SurfaceAgent, request.Surface)
	require.Equal(t, []string{"broken-links"}, request.Selectors)
	require.Equal(t, 31, request.MaxIssues)
	require.True(t, request.Repair)
	require.True(t, request.Apply)
	require.True(t, request.NonInteractive)
	require.Equal(t, "rzm agent validate fix broken-links --apply --scope-note 'Notes/A B.md'", request.ApplyCommand)

	completed := []ValidationCheckOutcome{{Check: "broken-links", Outcome: validate.CheckOutcomeCompleted}}
	tests := []struct {
		name   string
		result ValidationResult
		code   int
		ok     bool
	}{
		{name: "clean", code: 0, ok: true, result: ValidationResult{
			Result: validate.Result{OK: true}, Selector: "default", EffectiveChecks: []string{"broken-links"}, Outcomes: completed,
		}},
		{name: "findings", code: 1, result: ValidationResult{
			Result: validate.Result{IssueCount: 1, Checks: []validate.CheckResult{{
				Name: "broken-links", IssueCount: 1,
				Issues: []validate.Issue{{Code: "broken_note_link", Path: "a.md", Line: 3, Message: "missing note"}},
			}}},
			Selector: "default", EffectiveChecks: []string{"broken-links"}, Outcomes: completed,
		}},
		{name: "blocked", code: 2, result: ValidationResult{
			Result: validate.Result{OK: false}, Selector: "code-anchors", EffectiveChecks: []string{"code-anchors"},
			Outcomes: []ValidationCheckOutcome{{Check: "code-anchors", Outcome: validate.CheckOutcomeBlocked, PreparationCommand: "rzm index"}},
		}},
	}
	for _, tt := range tests {
		t.Run("run "+tt.name, func(t *testing.T) {
			runner := &validationProductRunnerStub{result: tt.result}
			outcome := (CodeModeValidationService{Runner: runner}).Call(context.Background(), map[string]any{
				"action": "run", "selector": "default", "maxIssues": float64(31),
			})
			require.Equal(t, tt.code, outcome.ExitCode)
			require.Equal(t, tt.ok, outcome.OK)
			require.Equal(t, tt.result, outcome.Payload)
			require.Len(t, runner.requests, 1)
			require.Equal(t, []string{"default"}, runner.requests[0].Selectors)
			require.Equal(t, 31, runner.requests[0].MaxIssues)
			require.True(t, runner.requests[0].NonInteractive)
		})
	}
}

func TestCodeModeValidationServiceRejectsApplyBeforeRunning(t *testing.T) {
	runner := &validationProductRunnerStub{err: errors.New("must not run")}
	outcome := (CodeModeValidationService{Runner: runner}).Call(context.Background(), map[string]any{"action": "fix", "apply": true})
	require.Equal(t, validationrun.ValidationExitFailure, outcome.ExitCode)
	require.False(t, outcome.OK)
	require.Empty(t, runner.requests)
	require.Contains(t, outcome.Stderr, "read-write")
	require.JSONEq(t, `{"ok":false,"error":"validate fix apply requires a read-write code-mode connection","exitCode":2}`, outcome.Stderr)
}

type currentUserRuntimeStub struct{ sets int }

func (*currentUserRuntimeStub) Show(context.Context) (identity.Resolution, error) {
	return identity.Resolution{Configured: true}, nil
}
func (s *currentUserRuntimeStub) Set(context.Context, string) (identity.Resolution, error) {
	s.sets++
	return identity.Resolution{}, nil
}
func (*currentUserRuntimeStub) Validate(context.Context) (identity.Resolution, error) {
	return identity.Resolution{Found: true}, nil
}

func TestCodeModeCurrentUserServiceEnforcesAuthorityBeforeMutation(t *testing.T) {
	runtime := &currentUserRuntimeStub{}
	outcome := (CodeModeCurrentUserService{Runtime: runtime}).Call(context.Background(), map[string]any{
		"action": "set", "personTitleOrRef": "People/Drew.md",
	})
	require.False(t, outcome.OK)
	require.Zero(t, runtime.sets)
	require.Contains(t, outcome.Stderr, "write_requires_read_write")
}

type ontologyQueryRuntimeStub struct {
	query     string
	variables map[string]any
	result    ontologyquery.Result
}

func (s *ontologyQueryRuntimeStub) Execute(_ context.Context, query string, variables map[string]any) (ontologyquery.Result, error) {
	s.query, s.variables = query, variables
	return s.result, nil
}

func TestCodeModeOntologyQueryServicePreservesQueryDomainErrors(t *testing.T) {
	runtime := &ontologyQueryRuntimeStub{result: ontologyquery.Result{Errors: []ontologyquery.Error{{Message: "bad query"}}}}
	outcome := (CodeModeOntologyQueryService{Runtime: runtime}).Call(context.Background(), map[string]any{
		"query": "query Example { notes { path } }", "variables": map[string]any{"limit": float64(3)},
	})
	require.Equal(t, 1, outcome.ExitCode)
	require.Empty(t, outcome.Stdout, "payload must not be repeated as stdout")
	require.Empty(t, outcome.Stderr)
	require.Equal(t, "query Example { notes { path } }", runtime.query)
	require.Equal(t, float64(3), runtime.variables["limit"])
}
