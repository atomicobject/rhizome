package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

type codeLocalService struct {
	*vaultCommandOptions
	readWrite bool
}

// callAgentCodeLocal delegates route selection to the authoritative catalog.
func (commandOptions *vaultCommandOptions) callAgentCodeLocal(ctx context.Context, name string, input map[string]any, readWrite bool) (agentcode.CallOutcome, bool) {
	return agentapi.CallCodeLocal(ctx, codeLocalService{vaultCommandOptions: commandOptions, readWrite: readWrite}, name, input)
}

func (commandOptions codeLocalService) Surface(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	payload, err := commandOptions.selectAgentSurface(ctx, codeModeString(input, "command"))
	if err != nil {
		return codeModeFailure(err)
	}
	return codeModeSuccess(payload)
}
func (commandOptions codeLocalService) Start(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return commandOptions.callAgentCodeStart(ctx, input)
}
func (service codeLocalService) CurrentUser(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeCurrentUserService{Runtime: commandCodeModeCurrentUserRuntime{vaultCommandOptions: service.vaultCommandOptions}, ReadWrite: service.readWrite}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) NextID(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeNextIDService{Allocator: commandCodeModeNextIDAllocator{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) OntologyQuerySchema(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologySchemaService{Runtime: commandCodeModeOntologySchemaRuntime{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) OntologyQuery(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyQueryService{Runtime: commandCodeModeOntologyQueryRuntime{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) OntologyReference(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyReferenceService{Runtime: commandCodeModeOntologyReferenceRuntime{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) OntologyAuthoringGuide(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyAuthoringService{Runtime: commandCodeModeOntologyAuthoringRuntime{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) OntologyInspect(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeOntologyInspectService{Runtime: commandCodeModeOntologyInspectRuntime{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) QueryRecipe(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return commandOptions.callAgentCodeQueryRecipe(ctx, input)
}
func (service codeLocalService) View(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return service.callAgentCodeView(ctx, input, service.readWrite)
}
func (service codeLocalService) Validate(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	return callAgentCodeValidate(ctx, input, service.readWrite)
}
func (commandOptions codeLocalService) CodeRationale(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeRationaleService{Runtime: commandCodeModeRationaleRuntime{vaultCommandOptions: commandOptions.vaultCommandOptions, Input: input}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (commandOptions codeLocalService) RenameHeading(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeRenameHeadingService{Renamer: commandCodeModeHeadingRenamer{vaultCommandOptions: commandOptions.vaultCommandOptions}}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}
func (service codeLocalService) NoteMove(ctx context.Context, input map[string]any) agentapi.CallOutcome {
	workflow := actions.CodeModeNoteMoveService{Mover: commandCodeModeNoteMover{vaultCommandOptions: service.vaultCommandOptions}, ReadWrite: service.readWrite, RenderText: renderMoveSummaryText}
	return codeModeWorkflowOutcome(workflow.Call(ctx, input))
}

func codeModeSuccess(payload any) agentcode.CallOutcome {
	return codeModeWorkflowOutcome(actions.CodeModeSuccess(payload))
}

func (commandOptions *vaultCommandOptions) callAgentCodeStart(ctx context.Context, input map[string]any) agentcode.CallOutcome {
	response, err := commandOptions.executeAgentStart(ctx, agentcode.StartRequest{
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

type commandCodeModeCurrentUserRuntime struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeCurrentUserRuntime) Show(ctx context.Context) (identity.Resolution, error) {
	return commandOptions.showCurrentUser(ctx)
}
func (commandOptions commandCodeModeCurrentUserRuntime) Set(ctx context.Context, ref string) (identity.Resolution, error) {
	return commandOptions.setCurrentUser(ctx, ref)
}
func (commandOptions commandCodeModeCurrentUserRuntime) Validate(ctx context.Context) (identity.Resolution, error) {
	return commandOptions.validateCurrentUser(ctx)
}

type commandCodeModeNextIDAllocator struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeNextIDAllocator) Allocate(ctx context.Context, typeName string, count int, paths []string) (*idalloc.Result, error) {
	return commandOptions.allocateAgentNextID(ctx, typeName, count, paths)
}

type commandCodeModeOntologySchemaRuntime struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeOntologySchemaRuntime) Discover(ctx context.Context, typeName string) (ontologyquery.SchemaDiscovery, error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return ontologyquery.SchemaDiscovery{}, err
	}
	ontologySchema, schema, err := loadOntologyExecutableSchema(vaultDef.BasePath())
	if err != nil {
		return ontologyquery.SchemaDiscovery{}, err
	}
	return ontologyquery.DiscoverSchema(ontologySchema, schema, typeName)
}

type commandCodeModeOntologyQueryRuntime struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeOntologyQueryRuntime) Execute(ctx context.Context, rawQuery string, variables map[string]any) (ontologyquery.Result, error) {
	runtime, prepared, cleanup, err := commandOptions.prepareOntologyQuery(ctx, rawQuery, variables)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return ontologyquery.Result{}, err
	}
	return runtime.run(ctx, prepared)
}

type commandCodeModeOntologyReferenceRuntime struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeOntologyReferenceRuntime) Render(ctx context.Context, rawType string, compact bool) (any, error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
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
		return schemadoc.CompactReference(schema, typeName, identity.CurrentUserType)
	}
	payload, err := schemadoc.RenderReferenceJSON(schema, typeName, identity.CurrentUserType)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

type commandCodeModeOntologyAuthoringRuntime struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeOntologyAuthoringRuntime) Render(ctx context.Context, requested []string) (string, error) {
	return commandOptions.renderAgentOntologyAuthoringGuide(ctx, requested)
}

type commandCodeModeOntologyInspectRuntime struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeOntologyInspectRuntime) Inspect(ctx context.Context, inputs []string) (any, error) {
	return commandOptions.buildOntologyInspectPayload(ctx, inputs)
}

type commandCodeModeRationaleRuntime struct {
	*vaultCommandOptions
	Input map[string]any
}

func (runtime commandCodeModeRationaleRuntime) Query(ctx context.Context, path string, kinds []string) (any, error) {
	cfg, preparedRuntime, err := runtime.prepareAgentJSONTool(ctx, 0, "code_rationale", "agent.code-rationale", runtime.Input)
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

type commandCodeModeHeadingRenamer struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeHeadingRenamer) Rename(ctx context.Context, params actions.RenameHeadingParams) (actions.RenameHeadingResult, error) {
	return commandOptions.executeRenameHeading(ctx, params)
}

type commandCodeModeNoteMover struct{ *vaultCommandOptions }

func (commandOptions commandCodeModeNoteMover) Move(ctx context.Context, params actions.MoveParams) (actions.MoveSummary, error) {
	return commandOptions.executeMoveNotes(ctx, params)
}
