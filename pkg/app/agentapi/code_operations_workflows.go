package agentapi

import (
	"fmt"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"reflect"
)

func codeQueryRecipeDescriptor() ToolDescriptor {
	d := runtimeRead(codeLocal(agentCLI(agentCapability(descriptor("query_recipe", "query-recipe", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.QueryRecipeTool), 18), 23, AgentCLICategoryOntology), CodeLocalService.QueryRecipe), runtimeReadActions("run"))

	recipeArray := codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(queryrecipe.Recipe{}), map[reflect.Type]bool{})}
	summaryArray := codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(queryrecipe.RecipeSummary{}), map[reflect.Type]bool{})}
	issueArray := codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(queryrecipe.Issue{}), map[reflect.Type]bool{})}

	c := contract("List, validate, or run a saved query recipe.", input(map[string]codeSchema{"op": enumeration("list", "validate", "run"), "path": str(), "id": str(), "anchor": stringsSchema(), "inputs": unknownObject()}, "op"), codeSchema{"oneOf": []any{
		object([]string{"recipes"}, map[string]codeSchema{"recipes": summaryArray, "issues": issueArray}),
		object([]string{"ok", "issueCount", "recipes"}, map[string]codeSchema{"ok": boolean(), "issueCount": integer(), "recipes": recipeArray, "issues": issueArray, "dependencies": unknownObject()}),
		schemaForType(reflect.TypeOf(queryrecipe.RunResult{}), map[reflect.Type]bool{}),
	}}, map[string]any{"op": "list"}, "reads recipes and, for run, the ontology projection")
	return completeCodeDescriptor(d, c, []string{"agent query-recipe list", "agent query-recipe validate", "agent query-recipe run"}, map[string]codeInputFieldMetadata{
		"op":     {Description: "Recipe operation: list, validate, or run."},
		"id":     {Description: "Recipe identifier for run. It may be omitted only when exactly one recipe is available."},
		"inputs": {Description: "Named JSON values for a run; values are converted to recipe input strings."},
		"anchor": {Description: "Additional anchor values merged into a run's inputs."},
	}, "list and validate operate on discovered recipes; run selects a recipe, merges inputs and anchors, then executes its compiled query.")
}

func codeViewDescriptor() ToolDescriptor {
	d := runtimeRead(codeLocal(localMutatingDescriptor("view", "view", 24, AgentCLICategoryOntology), CodeLocalService.View), runtimeReadActions("list", "show", "validate", "run"))
	d.validateCodeInput = func(value map[string]any) error {
		if value["action"] == "show" || value["action"] == "run" {
			return requireInputString(value, "id")
		}
		return nil
	}

	c := contract("List, show, validate, run, or eject a configured view.", input(map[string]codeSchema{"action": enumeration("list", "show", "validate", "run", "eject"), "path": str(), "id": str(), "variant": enumeration("table"), "search": str(), "filters": objectsSchema(), "sort": objectsSchema(), "group": str(), "offset": integer(), "first": integer(), "inputs": unknownObject(), "omitCapabilities": boolean()}, "action"), codeSchema{"oneOf": []any{
		schemaForType(reflect.TypeOf(appviews.Catalog{}), map[reflect.Type]bool{}),
		schemaForType(reflect.TypeOf(appviews.CatalogEntry{}), map[reflect.Type]bool{}),
		object([]string{"ok", "issueCount", "catalog"}, map[string]codeSchema{"ok": boolean(), "issueCount": integer(), "catalog": schemaForType(reflect.TypeOf(appviews.Catalog{}), map[reflect.Type]bool{}), "issues": objectsSchema()}),
		schemaForType(reflect.TypeOf(appviews.ExecuteResponse{}), map[reflect.Type]bool{}),
		schemaForType(reflect.TypeOf(appviews.EjectResult{}), map[reflect.Type]bool{}),
	}}, map[string]any{"action": "list"}, "reads view definitions and, for run, the ontology projection", "eject copies a bundled view folder into .rhizome/views and requires read-write")
	return completeCodeDescriptor(d, c, []string{"agent view list", "agent view show", "agent view validate", "agent view run", "agent view eject"}, map[string]codeInputFieldMetadata{
		"action":           {Description: "View operation: list, show, validate, run, or eject."},
		"id":               {Description: "View identifier. Required for show, run, and eject."},
		"path":             {Description: "Optional view YAML file or directory; omission uses the configured view location."},
		"inputs":           {Description: "Named JSON values passed to a run; values are converted to view input strings."},
		"omitCapabilities": {Description: "Leave the per-field filter, sort, and edit capability catalog out of a run's response; rows, columns, groups, profile, stats, and warnings remain.", Default: false, HasDefault: true},
	}, "show, run, and eject require id; list and validate operate on the selected view catalog; eject needs read-write.")
}

func codeValidateDescriptor() ToolDescriptor {
	d := codeLocal(localMutatingDescriptor("validate", "validate", 2, AgentCLICategoryValidation), CodeLocalService.Validate)

	c := contract("List checks, run a selector, or plan/apply safe fixes.", input(map[string]codeSchema{"action": enumeration("run", "list", "fix"), "selector": str(), "apply": boolean(), "maxIssues": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeImages": boolean(), "scopeNote": str(), "scopeTarget": str(), "scopeRef": str(), "allowHistorical": boolean()}, "action"), codeSchema{"oneOf": []any{
		schemaForType(reflect.TypeOf(actions.ValidationCatalog{}), map[reflect.Type]bool{}),
		schemaForType(reflect.TypeOf(validationrun.ValidationResult{}), map[reflect.Type]bool{}),
	}}, map[string]any{"action": "run", "selector": "default"}, "reads validation inputs", "fix apply may modify files with read-write authority")
	return completeCodeDescriptor(d, c, []string{"agent validate", "agent validate list", "agent validate fix"}, map[string]codeInputFieldMetadata{
		"action":    {Description: "Validation operation: list, run, or fix."},
		"selector":  {Description: "Optional named validation selector; omission uses the workflow default for run or fix."},
		"maxIssues": {Description: "Maximum issues returned by the code-mode validation workflow.", Default: 20, HasDefault: true},
	}, "fix only applies changes when apply=true and the connection has read-write authority; outcomes retain selector and remediation evidence.")
}

func codeCurrentUserDescriptor() ToolDescriptor {
	d := codeLocal(localMutatingDescriptor("current_user", "current-user", 25, AgentCLICategoryIdentity), CodeLocalService.CurrentUser)
	d.validateCodeInput = func(value map[string]any) error {
		if value["action"] == "set" {
			return requireInputString(value, "personTitleOrRef")
		}
		return nil
	}
	c := contract("Show, set, or validate the configured current Person.", input(map[string]codeSchema{"action": enumeration("show", "set", "validate"), "personTitleOrRef": str()}, "action"), schemaForType(reflect.TypeOf(identity.Resolution{}), map[reflect.Type]bool{}), map[string]any{"action": "show"}, "reads identity configuration", "set writes ignored per-vault identity configuration with read-write authority")
	return completeCodeDescriptor(d, c, []string{"agent current-user show", "agent current-user set", "agent current-user validate"}, map[string]codeInputFieldMetadata{
		"action":           {Description: "Identity operation: show, set, or validate."},
		"personTitleOrRef": {Description: "Person title or reference. Required for set, which requires read-write authority."},
	}, "set requires personTitleOrRef and a read-write connection; show and validate are read-only identity checks.")
}

func codeStartDescriptor() ToolDescriptor {
	d := codeLocal(localDescriptor("start", "start", 1, AgentCLICategorySession), CodeLocalService.Start)
	c := contract("Start or reuse a session and return bounded context.", input(map[string]codeSchema{"profile": enumeration("code", "vault"), "intent": str(), "contextFiles": stringsSchema(), "files": stringsSchema(), "submoduleDepth": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeTags": boolean(), "recencyCascade": boolean(), "graphSummary": boolean(), "includeOntology": boolean(), "timings": boolean()}), output(map[string]codeSchema{"sessionId": str(), "durationMs": integer(), "surfaceCommand": str(), "notes": stringsSchema(), "code": codeSurfaceSchema(), "vaultContext": schemaForType(reflect.TypeOf(mcpapi.ContextTextResponse{}), map[reflect.Type]bool{}), "ontology": unknownObject(), "ontologyError": str(), "diagnostics": unknownObject(), "runtime": object([]string{"state", "ready"}, map[string]codeSchema{"state": enumeration("connected", "starting", "access_denied", "disabled", "unavailable"), "ready": boolean(), "code": str(), "message": str()})}), map[string]any{"intent": "inspect persistent code mode", "profile": "code"}, "reads bounded repository context", "checks local runtime access and may request startup", "may persist opportunistic session state")
	return completeCodeDescriptor(d, c, []string{"agent start"}, map[string]codeInputFieldMetadata{
		"profile":        {Description: "Bootstrap profile: code or vault."},
		"includeTags":    {Description: "Include tag context in bootstrap output.", Default: true, HasDefault: true},
		"recencyCascade": {Description: "Include recency-cascade graph context when applicable.", Default: true, HasDefault: true},
	}, "Bootstrap creates or reuses a session and returns compact code discovery plus bounded context; richer selectors make indexed work explicit.")
}

func codeSurfaceDescriptor() ToolDescriptor {
	d := ToolDescriptor{Name: "surface", Kind: ToolKindLocal, Surfaces: SurfaceCodeMode, Availability: AvailabilityAlways, Mutation: MutationNever, CodeLocal: CodeLocalService.Surface}
	c := contract("Describe all agent commands or one exact command path.", input(map[string]codeSchema{"command": str()}), output(map[string]codeSchema{"version": str(), "command": str(), "mode": str(), "statusSnapshotKind": str(), "vaultPath": str(), "defaultBudgetChars": integer(), "capabilities": schemaForType(reflect.TypeOf(mcpapi.CapabilitiesResponse{}), map[reflect.Type]bool{}), "indexes": unknownObject(), "compression": unknownObject(), "notes": stringsSchema(), "retrievalLoop": stringsSchema(), "commands": objectsSchema(), "selected": schemaForType(reflect.TypeOf(SurfaceCommand{}), map[reflect.Type]bool{})}), map[string]any{}, "reads catalog and runtime capability metadata")
	return completeCodeDescriptor(d, c, []string{"agent surface"}, map[string]codeInputFieldMetadata{
		"command": {Description: "Optional exact agent command path to select; omit for the complete surface."},
	}, "Omit command for all agent command metadata or select one exact command path.")
}

func codeNoteMoveDescriptor() ToolDescriptor {
	d := ToolDescriptor{
		Name: "note_move", AgentCommand: "note move", Kind: ToolKindLocal,
		Surfaces: SurfaceCodeMode, Availability: AvailabilityReadWrite, Mutation: MutationReadWrite,
		CodeLocal: CodeLocalService.NoteMove,
	}
	d.validateCodeInput = func(value map[string]any) error {
		_, source := value["source"]
		_, sources := value["sources"]
		_, target := value["target"]
		_, folder := value["toFolder"]
		if !source && !sources {
			return fmt.Errorf("input.source or input.sources is required")
		}
		if folder {
			if target {
				return fmt.Errorf("input.target cannot be combined with input.toFolder")
			}
			return nil
		}
		if !source || sources || !target {
			return fmt.Errorf("input.source and input.target are required unless sources and toFolder are used")
		}
		return nil
	}

	c := contract("Move notes or attachments and update backlinks.", input(map[string]codeSchema{"source": str(), "target": str(), "sources": stringsSchema(), "toFolder": str(), "overwrite": boolean(), "updateBacklinks": boolean()}), schemaForType(reflect.TypeOf(actions.MoveSummary{}), map[reflect.Type]bool{}), map[string]any{"source": "docs/old.md", "target": "docs/new.md"}, "moves files", "may rewrite backlinks with read-write authority")
	return completeCodeDescriptor(d, c, []string{"note move"}, map[string]codeInputFieldMetadata{
		"source":          {Description: "One source path. Use with target, or combine sources with toFolder."},
		"sources":         {Description: "Multiple source paths. Requires toFolder and read-write authority."},
		"target":          {Description: "Destination for exactly one source; cannot be combined with toFolder."},
		"toFolder":        {Description: "Destination folder for sources; cannot be combined with target."},
		"updateBacklinks": {Description: "Rewrite backlinks while moving.", Default: true, HasDefault: true},
	}, "Use source plus target for one move, or sources plus toFolder for a batch; execution requires a read-write connection.")
}

func codeActionListDescriptor() ToolDescriptor {
	d := ToolDescriptor{Name: "action_list", Kind: ToolKindLocal, Surfaces: SurfaceCodeMode, Availability: AvailabilityAlways, Mutation: MutationNever, CodeLocal: CodeLocalService.ActionList}
	c := contract("List the repository's module actions with their inputs.", input(nil), codeSchema{"type": "array", "items": object([]string{"id", "module", "name", "description", "inputs"}, map[string]codeSchema{
		"id": str(), "module": str(), "name": str(), "description": str(),
		"inputs": codeSchema{"type": "object", "additionalProperties": object([]string{"kind"}, map[string]codeSchema{"kind": enumeration("string", "int", "bool", "list"), "required": boolean(), "enum": stringsSchema()})},
	})}, map[string]any{}, "reads module action declarations")
	return completeCodeDescriptor(d, c, []string{"module <module>"}, nil, "Declarations that fail validation are left out; rzm validate actions reports them.")
}

func codeActionRunDescriptor() ToolDescriptor {
	// An action runs whatever its module's command does, so it may write.
	d := ToolDescriptor{Name: "action_run", Kind: ToolKindLocal, Surfaces: SurfaceCodeMode, Availability: AvailabilityAlways, Mutation: MutationAlways, CodeLocal: CodeLocalService.ActionRun}
	c := contract("Run one module action and return its envelope.", input(map[string]codeSchema{"id": str(), "input": unknownObject()}, "id"), object([]string{"status"}, map[string]codeSchema{
		"status": enumeration("ok", "refused", "error"), "result": unknownValue(), "reason": str(), "stderrTail": str(),
	}), map[string]any{"id": "compounding/health"}, "runs the module's command or code-mode script in the repository root", "requires a trusted repository")
	return completeCodeDescriptor(d, c, []string{"module <module> <action>"}, map[string]codeInputFieldMetadata{
		"id":    {Description: "Action id, <module>/<name>, as action_list returns it."},
		"input": {Description: "Input object matching the action's declared inputs; omit when it declares none."},
	}, "status is ok with the action's result, refused with a reason (including an untrusted repository), or error with a reason and stderrTail. An unknown id or input that does not match the declaration fails the call without running anything.")
}
