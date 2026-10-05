package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/pkg/app/agentstart"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

type codeLocalService struct{ readWrite bool }

// callAgentCodeLocal delegates route selection to the authoritative catalog.
func callAgentCodeLocal(ctx context.Context, name string, input map[string]any, readWrite bool) (agentcode.CallOutcome, bool) {
	return agentapi.CallCodeLocal(ctx, codeLocalService{readWrite: readWrite}, name, input)
}

func (codeLocalService) Surface(_ context.Context, input map[string]any) agentapi.CallOutcome {
	payload, err := selectAgentSurface(codeModeString(input, "command"))
	if err != nil {
		return codeModeFailure(err)
	}
	return codeModeSuccess(payload)
}
func (codeLocalService) Start(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return callAgentCodeStart(ctx, input)
}
func (service codeLocalService) CurrentUser(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeCurrentUserService{Runtime: commandCodeModeCurrentUserRuntime{}, ReadWrite: service.readWrite}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) NextID(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeNextIDService{Allocator: commandCodeModeNextIDAllocator{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) OntologyQuerySchema(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologySchemaService{Runtime: commandCodeModeOntologySchemaRuntime{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) OntologyQuery(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyQueryService{Runtime: commandCodeModeOntologyQueryRuntime{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) OntologyReference(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyReferenceService{Runtime: commandCodeModeOntologyReferenceRuntime{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) OntologyAuthoringGuide(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyAuthoringService{Runtime: commandCodeModeOntologyAuthoringRuntime{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) OntologyInspect(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyInspectService{Runtime: commandCodeModeOntologyInspectRuntime{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) QueryRecipe(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return callAgentCodeQueryRecipe(ctx, input)
}
func (service codeLocalService) View(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return callAgentCodeView(ctx, input, service.readWrite)
}
func (service codeLocalService) Validate(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return callAgentCodeValidate(ctx, input, service.readWrite)
}
func (codeLocalService) CodeRationale(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeRationaleService{Runtime: commandCodeModeRationaleRuntime{Input: input}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (codeLocalService) RenameHeading(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeRenameHeadingService{Renamer: commandCodeModeHeadingRenamer{}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (service codeLocalService) NoteMove(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeNoteMoveService{Mover: commandCodeModeNoteMover{}, ReadWrite: service.readWrite, RenderText: renderMoveSummaryText}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}

func codeModeSuccess(payload any) agentcode.CallOutcome {
	return codeModeWorkflowOutcome(actions.CodeModeSuccess(payload))
}

func callAgentCodeStart(ctx context.Context, input map[string]any) agentcode.CallOutcome {
	response, err := executeAgentStart(ctx, agentstart.Request{
		Profile: codeModeString(input, "profile"), Intent: codeModeString(input, "intent"),
		ContextFiles: codeModeStrings(input, "contextFiles"), Files: codeModeStrings(input, "files"),
		BudgetChars: codeModeInt(input, "budgetChars"), SubmoduleDepth: codeModeInt(input, "submoduleDepth"),
		SkipAnchors: codeModeBool(input, "skipAnchors"), SkipEmbeds: codeModeBool(input, "skipEmbeds"),
		IncludeTags: codeModeBoolDefault(input, "includeTags", true), RecencyCascade: codeModeBoolDefault(input, "recencyCascade", true),
		GraphSummary: codeModeBool(input, "graphSummary"), IncludeOntology: codeModeBool(input, "includeOntology"),
		Timings: codeModeBool(input, "timings"), SessionID: codeModeString(input, "sessionId"),
	})
	if err != nil {
		return codeModeFailure(err)
	}
	return codeModeSuccess(response)
}

type commandCodeModeCurrentUserRuntime struct{}

func (commandCodeModeCurrentUserRuntime) Show(ctx context.Context) (identity.Resolution, error) {
	return showCurrentUser(ctx)
}
func (commandCodeModeCurrentUserRuntime) Set(ctx context.Context, ref string) (identity.Resolution, error) {
	return setCurrentUser(ctx, ref)
}
func (commandCodeModeCurrentUserRuntime) Validate(ctx context.Context) (identity.Resolution, error) {
	return validateCurrentUser(ctx)
}

type commandCodeModeNextIDAllocator struct{}

func (commandCodeModeNextIDAllocator) Allocate(ctx context.Context, typeName string, count int, paths []string) (*idalloc.Result, error) {
	return allocateAgentNextID(ctx, typeName, count, paths)
}

type commandCodeModeOntologySchemaRuntime struct{}

func (commandCodeModeOntologySchemaRuntime) Discover(ctx context.Context, typeName string) (ontologyquery.SchemaDiscovery, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return ontologyquery.SchemaDiscovery{}, err
	}
	ontologySchema, schema, err := loadOntologyExecutableSchema(vaultDef.BasePath())
	if err != nil {
		return ontologyquery.SchemaDiscovery{}, err
	}
	return ontologyquery.DiscoverSchema(ontologySchema, schema, typeName)
}

type commandCodeModeOntologyQueryRuntime struct{}

func (commandCodeModeOntologyQueryRuntime) Execute(ctx context.Context, rawQuery string, variables map[string]any) (ontologyquery.Result, error) {
	runtime, prepared, cleanup, err := prepareOntologyQuery(ctx, rawQuery, variables)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return ontologyquery.Result{}, err
	}
	return ontologyquery.ExecutePrepared(ctx, runtime.deps(), runtime.schema, runtime.execSchema, prepared), nil
}

type commandCodeModeOntologyReferenceRuntime struct{}

func (commandCodeModeOntologyReferenceRuntime) Render(ctx context.Context, rawType string, compact bool) (any, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return nil, err
	}
	schema, _, err := loadOntologyExecutableSchema(vaultDef.BasePath())
	if err != nil {
		return nil, err
	}
	typeName, err := singleOntologyTypeFilter(parseOntologyTypes(rawType))
	if err != nil {
		return nil, err
	}
	if compact {
		return reference.Compact(schema, typeName)
	}
	payload, err := reference.RenderJSON(schema, typeName)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

type commandCodeModeOntologyAuthoringRuntime struct{}

func (commandCodeModeOntologyAuthoringRuntime) Render(ctx context.Context, requested []string) (string, error) {
	return renderAgentOntologyAuthoringGuide(ctx, requested)
}

type commandCodeModeOntologyInspectRuntime struct{}

func (commandCodeModeOntologyInspectRuntime) Inspect(ctx context.Context, inputs []string) (any, error) {
	return buildOntologyInspectPayload(ctx, inputs)
}

type commandCodeModeRationaleRuntime struct{ Input map[string]any }

func (runtime commandCodeModeRationaleRuntime) Query(ctx context.Context, path string, kinds []string) (any, error) {
	cfg, preparedRuntime, err := prepareAgentJSONTool(ctx, 0, "code_rationale", "agent.code-rationale", runtime.Input)
	if err != nil {
		return nil, err
	}
	if preparedRuntime != nil {
		defer preparedRuntime.Close()
	}
	if cfg.IndexedContextUnavailable != nil {
		return nil, fmt.Errorf(`{"code":%q,"message":"code_rationale requires the managed indexed read model","remediation":%q}`, cfg.IndexedContextUnavailable.WarningCode, cfg.IndexedContextUnavailable.Remediation)
	}
	return queryAgentCodeRationale(ctx, cfg.GetIntelStore(), cfg.VaultPath, path, kinds)
}

type commandCodeModeHeadingRenamer struct{}

func (commandCodeModeHeadingRenamer) Rename(ctx context.Context, params actions.RenameHeadingParams) (actions.RenameHeadingResult, error) {
	return executeRenameHeading(ctx, params)
}

type commandCodeModeNoteMover struct{}

func (commandCodeModeNoteMover) Move(ctx context.Context, params actions.MoveParams) (actions.MoveSummary, error) {
	return executeMoveNotes(ctx, params)
}
