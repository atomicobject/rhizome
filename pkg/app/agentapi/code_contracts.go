package agentapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

// CodeOperationContract is the selectively loaded application contract for a
// catalog operation. Transport envelopes are deliberately outside this type.
type CodeOperationContract struct {
	Summary        string          `json:"summary"`
	Effects        []string        `json:"effects"`
	InputSchema    json.RawMessage `json:"inputSchema"`
	OutputSchema   json.RawMessage `json:"outputSchema"`
	Example        map[string]any  `json:"example"`
	Interpretation []string        `json:"interpretation"`
}

func CodeOperationDescriptor(name string) (ToolDescriptor, bool) {
	for _, descriptor := range toolCatalog {
		if descriptor.Name == name && descriptor.CodeContract != nil {
			return cloneToolDescriptor(descriptor), true
		}
	}
	return ToolDescriptor{}, false
}

func CodeOperationDescriptors() []ToolDescriptor {
	var descriptors []ToolDescriptor
	for _, descriptor := range toolCatalog {
		if descriptor.CodeContract != nil {
			descriptors = append(descriptors, cloneToolDescriptor(descriptor))
		}
	}
	return descriptors
}

func cloneContractMap(value map[string]any) map[string]any {
	payload, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(payload, &cloned)
	return cloned
}

type codeSchema map[string]any

func str() codeSchema           { return codeSchema{"type": "string"} }
func boolean() codeSchema       { return codeSchema{"type": "boolean"} }
func integer() codeSchema       { return codeSchema{"type": "integer"} }
func stringsSchema() codeSchema { return codeSchema{"type": "array", "items": str()} }
func objectsSchema() codeSchema { return codeSchema{"type": "array", "items": unknownObject()} }
func unknownObject() codeSchema { return codeSchema{"type": "object", "additionalProperties": true} }
func unknownValue() codeSchema  { return codeSchema{} }
func enumeration(values ...string) codeSchema {
	items := make([]any, len(values))
	for i := range values {
		items[i] = values[i]
	}
	return codeSchema{"type": "string", "enum": items}
}
func object(required []string, properties map[string]codeSchema) codeSchema {
	p := map[string]any{}
	for key, value := range properties {
		p[key] = value
	}
	schema := codeSchema{"type": "object", "additionalProperties": false, "properties": p}
	if len(required) > 0 {
		r := make([]any, len(required))
		for i := range required {
			r[i] = required[i]
		}
		schema["required"] = r
	}
	return schema
}
func output(properties map[string]codeSchema) codeSchema {
	p := map[string]any{}
	for key, value := range properties {
		p[key] = value
	}
	return codeSchema{"type": "object", "additionalProperties": true, "properties": p}
}
func raw(schema codeSchema) json.RawMessage { payload, _ := json.Marshal(schema); return payload }
func input(extra map[string]codeSchema, required ...string) codeSchema {
	properties := map[string]codeSchema{"sessionId": str(), "budgetChars": integer()}
	for key, value := range extra {
		properties[key] = value
	}
	return object(required, properties)
}
func contract(summary string, in, out codeSchema, example map[string]any, effects ...string) *CodeOperationContract {
	return &CodeOperationContract{
		Summary: summary, Effects: effects, InputSchema: raw(in), OutputSchema: raw(out), Example: example,
	}
}

func typedContract(summary string, in codeSchema, example map[string]any, effects ...string) *CodeOperationContract {
	return contract(summary, in, unknownValue(), example, effects...)
}

// initializeCodeContracts constructs schemas once, then projects them onto the
// authoritative catalog. The temporary map is discarded after package init.
func initializeCodeContracts() {
	objs := objectsSchema()
	entries := map[string]struct {
		contract *CodeOperationContract
		leaves   []string
	}{
		"check_paths":    {checkPathsContract(), []string{"agent check-paths"}},
		"evaluate_batch": {evaluateBatchContract(), []string{"agent evaluate-batch"}},
		"evaluate":       {evaluateContract(), []string{"agent evaluate"}},
		"files": {contract("Read files selected by input expressions.", input(map[string]codeSchema{
			"inputs": stringsSchema(), "continuationToken": str(), "includeContent": codeSchema{"oneOf": []any{boolean(), enumeration("true", "false", "compress")}}, "intent": str(), "limit": integer(), "maxDepth": integer(), "dedupe": boolean(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeFrontmatter": boolean(), "includeBacklinks": boolean(), "absolutePaths": boolean(), "suppressTags": stringsSchema(), "noSuppress": boolean(),
		}), unknownValue(), map[string]any{"inputs": []string{"pkg/app/agentapi/catalog.go"}, "includeContent": true}, "reads selected files", "may update session dedupe bookkeeping", "provide inputs for a first page or continuationToken for a later page"), []string{"agent files"}},
		"file_context":             {typedContract("Build bounded context for selected files or directories.", input(map[string]codeSchema{"files": stringsSchema(), "profile": str(), "intent": str(), "exclude_note_paths": stringsSchema(), "exclude_doc_paths": stringsSchema(), "anchorKinds": stringsSchema(), "ensureLinkTargets": enumeration("never", "plan", "apply"), "submoduleDepth": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean()}), map[string]any{"files": []string{"pkg/app/agentapi/catalog.go"}}, "reads files and indexed context", "agent CLI rejects apply"), []string{"agent file-context"}},
		"vault_context":            {typedContract("Build bounded repository or vault context.", input(map[string]codeSchema{"profile": enumeration("code", "vault"), "intent": str(), "contextFiles": stringsSchema(), "files": stringsSchema(), "submoduleDepth": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeTags": boolean(), "recencyCascade": boolean(), "graphSummary": boolean(), "includeOntology": boolean(), "requestScope": enumeration("minimal_bootstrap", "indexed_bootstrap", "rich"), "keyPatterns": stringsSchema()}), map[string]any{"profile": "code", "files": []string{"pkg/app/agentapi"}}, "reads repository guidance and requested indexed context"), []string{"agent vault-context"}},
		"semantic_query":           {typedContract("Search notes, docs, and indexed code.", input(map[string]codeSchema{"queries": codeSchema{"type": "array", "items": codeSchema{"oneOf": []any{str(), object([]string{"text"}, map[string]codeSchema{"text": str(), "mode": str()})}}}, "paths": stringsSchema(), "types": stringsSchema(), "scope": enumeration("all", "code", "docs", "tests"), "pathPrefix": str(), "folder": str(), "noteType": str(), "mode": str(), "continuationToken": str(), "limit": integer(), "explain": boolean(), "requireExactSymbol": boolean(), "includeTests": boolean(), "excludeNotes": boolean(), "timings": boolean(), "compact": boolean()}), map[string]any{"queries": []string{"persistent code mode"}, "scope": "code"}, "reads search and code indexes", "provide queries for a first page or continuationToken for a later page"), []string{"agent semantic-query"}},
		"code_symbol":              {typedContract("Resolve one indexed code symbol.", input(map[string]codeSchema{"symbol": str(), "path": str(), "language": str(), "contextLines": integer(), "maxBytes": integer()}, "symbol"), map[string]any{"symbol": "agentapi.ToolDescriptor"}, "reads the existing code index"), []string{"agent code-symbol"}},
		"code_references":          {typedContract("Return indexed definitions, callers, and callees.", input(map[string]codeSchema{"symbol": str(), "path": str(), "language": str(), "limit": integer(), "includeDefinitions": boolean(), "includeCallers": boolean(), "includeCallees": boolean()}, "symbol"), map[string]any{"symbol": "agentapi.ToolDescriptor"}, "reads the existing code index"), []string{"agent code-references"}},
		"code_symbol_context":      {typedContract("Build a compact indexed evidence packet for a symbol.", input(map[string]codeSchema{"symbol": str(), "path": str(), "language": str(), "includeTests": boolean()}, "symbol"), map[string]any{"symbol": "agentapi.ToolDescriptor"}, "reads the existing code index and source snippets"), []string{"agent code-symbol-context"}},
		"external_references":      {typedContract("Find local uses of an exact external code target.", input(map[string]codeSchema{"handle": str(), "ecosystem": str(), "module": str(), "symbolPrefix": str(), "limit": integer()}), map[string]any{"ecosystem": "go", "module": "github.com/mark3labs/mcp-go"}, "reads the existing code index"), []string{"agent external-references"}},
		"find_connections":         {typedContract("Find related notes for a note path or text.", input(map[string]codeSchema{"note": str(), "text": str(), "limit": integer()}), map[string]any{"note": "docs/specs/technical/persistent-agent-code-mode.md"}, "reads graph and semantic indexes"), []string{"agent find-connections"}},
		"graph_path":               {typedContract("Find a bounded typed graph path.", input(map[string]codeSchema{"from": str(), "to": str(), "maxHops": integer()}, "from", "to"), map[string]any{"from": "README.md", "to": "pkg/app/agentapi/catalog.go"}, "reads the ontology graph"), []string{"agent graph-path"}},
		"list_tags":                {typedContract("List tags matching optional patterns.", input(map[string]codeSchema{"match": stringsSchema()}), map[string]any{}, "reads note metadata"), []string{"agent list-tags"}},
		"list_properties":          {typedContract("List property names, types, values, and counts.", input(map[string]codeSchema{"source": enumeration("all", "frontmatter", "inline"), "match": stringsSchema(), "only": stringsSchema(), "valueLimit": integer(), "maxValues": integer(), "excludeTags": boolean(), "verbose": boolean(), "valueCounts": boolean()}), map[string]any{"source": "all"}, "reads note metadata"), []string{"agent list-properties"}},
		"community_list":           {typedContract("List graph communities and representative notes.", input(map[string]codeSchema{"maxCommunities": integer(), "maxTopNotes": integer()}), map[string]any{}, "reads graph analysis"), []string{"agent community-list"}},
		"report":                   {typedContract("Run one indexed code intelligence report.", input(map[string]codeSchema{"op": enumeration("doc_coverage", "complexity", "hotspots", "rationale_attention", "relatedness", "code_similarity"), "paths": stringsSchema(), "limit": integer(), "minLines": integer(), "includeTests": boolean()}, "op"), map[string]any{"op": "hotspots"}, "reads the existing code index"), []string{"agent report"}},
		"vault_health":             {typedContract("Compute selected vault health metrics.", input(map[string]codeSchema{"include": stringsSchema(), "staleDays": integer(), "limit": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeImages": boolean()}), map[string]any{}, "reads files and graph analysis"), []string{"agent vault-health"}},
		"ontology_query_schema":    {contract("Return the full executable schema or one selected type fragment with root arguments.", input(map[string]codeSchema{"type": str()}), schemaForType(reflect.TypeOf(ontologyquery.SchemaDiscovery{}), map[reflect.Type]bool{}), map[string]any{}, "reads ontology configuration"), []string{"agent ontology-query-schema"}},
		"ontology_query":           {contract("Execute a read-only ontology GraphQL query. Typed root coverage is in extensions.typedRoots by response alias.", input(map[string]codeSchema{"query": str(), "variables": unknownObject()}, "query"), output(map[string]codeSchema{"data": unknownObject(), "errors": objs, "extensions": unknownObject()}), map[string]any{"query": "query { types { name } }"}, "reads the ontology projection", "query data is intentionally dynamic"), []string{"agent ontology-query"}},
		"query_recipe":             {typedContract("List, validate, or run a saved query recipe.", input(map[string]codeSchema{"op": enumeration("list", "validate", "run"), "path": str(), "id": str(), "anchor": stringsSchema(), "inputs": unknownObject()}, "op"), map[string]any{"op": "list"}, "reads recipes and, for run, the ontology projection"), []string{"agent query-recipe list", "agent query-recipe validate", "agent query-recipe run"}},
		"node_link":                {typedContract("Resolve or plan durable node link targets.", input(map[string]codeSchema{"refs": codeSchema{"type": "array", "items": codeSchema{"oneOf": []any{str(), unknownObject()}}}, "targets": stringsSchema(), "ensure": enumeration("never", "plan", "apply")}), map[string]any{"targets": []string{"docs/example.md#Heading"}, "ensure": "plan"}, "reads ontology and notes", "agent CLI rejects apply"), []string{"agent node-link"}},
		"note_rename_heading":      {typedContract("Plan a heading rename and inbound link rewrites.", input(map[string]codeSchema{"path": str(), "oldHeading": str(), "newHeading": str(), "apply": boolean(), "upgradeToBlockId": enumeration("auto", "always", "never"), "fallback": enumeration("heading", "block-id")}, "path", "oldHeading", "newHeading"), map[string]any{"path": "docs/example.md", "oldHeading": "Old", "newHeading": "New"}, "reads target and inbound links", "agent CLI rejects apply"), []string{"agent note-rename-heading"}},
		"start":                    {contract("Start or reuse a session and return bounded context.", input(map[string]codeSchema{"profile": enumeration("code", "vault"), "intent": str(), "contextFiles": stringsSchema(), "files": stringsSchema(), "submoduleDepth": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeTags": boolean(), "recencyCascade": boolean(), "graphSummary": boolean(), "includeOntology": boolean(), "timings": boolean()}), output(map[string]codeSchema{"sessionId": str(), "durationMs": integer(), "surfaceCommand": str(), "notes": stringsSchema(), "code": codeSurfaceSchema(), "vaultContext": schemaForType(reflect.TypeOf(mcpapi.ContextTextResponse{}), map[reflect.Type]bool{}), "ontology": unknownObject(), "ontologyError": str(), "diagnostics": unknownObject()}), map[string]any{"intent": "inspect persistent code mode", "profile": "code"}, "reads bounded repository context", "may persist opportunistic session state"), []string{"agent start"}},
		"ontology_reference":       {typedContract("Render ontology reference JSON; compact authoring contracts require a type.", input(map[string]codeSchema{"type": str(), "compact": boolean()}), map[string]any{}, "reads ontology configuration"), []string{"agent ontology-reference"}},
		"ontology_authoring_guide": {contract("Render ontology note-authoring guidance.", input(map[string]codeSchema{"type": str(), "types": stringsSchema()}), str(), map[string]any{}, "reads ontology configuration"), []string{"agent ontology-authoring-guide"}},
		"ontology_inspect":         {typedContract("Inspect ontology inputs and live projection.", input(map[string]codeSchema{"inputs": stringsSchema()}, "inputs"), map[string]any{"inputs": []string{"Spec"}}, "reads ontology configuration and projection"), []string{"agent ontology-inspect"}},
		"view":                     {typedContract("List, show, validate, run, or eject a configured view.", input(map[string]codeSchema{"action": enumeration("list", "show", "validate", "run", "eject"), "path": str(), "id": str(), "variant": enumeration("table"), "search": str(), "filters": objectsSchema(), "sort": objectsSchema(), "group": str(), "offset": integer(), "first": integer(), "inputs": unknownObject(), "omitCapabilities": boolean()}, "action"), map[string]any{"action": "list"}, "reads view definitions and, for run, the ontology projection", "eject copies a bundled view folder into .rhizome/views and requires read-write"), []string{"agent view list", "agent view show", "agent view validate", "agent view run", "agent view eject"}},
		"validate":                 {typedContract("List checks, run a selector, or plan/apply safe fixes.", input(map[string]codeSchema{"action": enumeration("run", "list", "fix"), "selector": str(), "apply": boolean(), "maxIssues": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeImages": boolean(), "scopeNote": str(), "scopeTarget": str(), "scopeRef": str(), "allowHistorical": boolean()}, "action"), map[string]any{"action": "run", "selector": "default"}, "reads validation inputs", "fix apply may modify files with read-write authority"), []string{"agent validate", "agent validate list", "agent validate fix"}},
		"current_user":             {contract("Show, set, or validate the configured current Person.", input(map[string]codeSchema{"action": enumeration("show", "set", "validate"), "personTitleOrRef": str()}, "action"), schemaForType(reflect.TypeOf(identity.Resolution{}), map[reflect.Type]bool{}), map[string]any{"action": "show"}, "reads identity configuration", "set writes ignored per-vault identity configuration with read-write authority"), []string{"agent current-user show", "agent current-user set", "agent current-user validate"}},
		"next_id":                  {typedContract("Allocate identifiers for a typed note family.", input(map[string]codeSchema{"type": str(), "count": integer(), "paths": stringsSchema()}, "type"), map[string]any{"type": "Spec", "count": 1}, "reads ontology and identifier claims", "may refresh ontology metadata; returned identifiers are not reserved"), []string{"agent next-id"}},
		"code_rationale":           {contract("List extracted rationale comments from indexed code.", input(map[string]codeSchema{"path": str(), "kinds": stringsSchema()}), output(map[string]codeSchema{"count": integer(), "rationale": codeSchema{"type": "array", "items": object([]string{"id", "path", "kind", "content", "startLine", "endLine"}, map[string]codeSchema{"id": str(), "path": str(), "symbolFqn": str(), "kind": str(), "content": str(), "startLine": integer(), "endLine": integer(), "fingerprint": str()})}}), map[string]any{"path": "pkg/app/agentapi"}, "reads the existing code index"), []string{"agent code-rationale"}},
		"surface":                  {contract("Describe all agent commands or one exact command path.", input(map[string]codeSchema{"command": str()}), output(map[string]codeSchema{"version": str(), "command": str(), "mode": str(), "statusSnapshotKind": str(), "vaultPath": str(), "defaultBudgetChars": integer(), "capabilities": schemaForType(reflect.TypeOf(mcpapi.CapabilitiesResponse{}), map[reflect.Type]bool{}), "indexes": unknownObject(), "compression": unknownObject(), "notes": stringsSchema(), "retrievalLoop": stringsSchema(), "commands": objs, "selected": schemaForType(reflect.TypeOf(SurfaceCommand{}), map[reflect.Type]bool{})}), map[string]any{}, "reads catalog and runtime capability metadata"), []string{"agent surface"}},
		"note_move":                {typedContract("Move notes or attachments and update backlinks.", input(map[string]codeSchema{"source": str(), "target": str(), "sources": stringsSchema(), "toFolder": str(), "overwrite": boolean(), "updateBacklinks": boolean()}), map[string]any{"source": "docs/old.md", "target": "docs/new.md"}, "moves files", "may rewrite backlinks with read-write authority"), []string{"note move"}},
	}
	responseTypes := sharedResponseTypes()
	for i := range toolCatalog {
		name := toolCatalog[i].Name
		entry, ok := entries[name]
		if !ok {
			continue
		}
		if responseType, typed := responseTypes[name]; typed {
			entry.contract.OutputSchema = raw(schemaForType(responseType, map[reflect.Type]bool{}))
		}
		switch name {
		case "ontology_reference":
			entry.contract.OutputSchema = raw(object([]string{"types"}, map[string]codeSchema{"types": codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(ontology.TypeDoc{}), map[reflect.Type]bool{})}, "format": str(), "schemaHash": str(), "expand": schemaForType(reflect.TypeOf([]reference.Expansion{}), map[reflect.Type]bool{})}))
		case "ontology_inspect":
			entry.contract.OutputSchema = raw(schemaForType(reflect.TypeOf(ontology.InspectResult{}), map[reflect.Type]bool{}))
		case "next_id":
			entry.contract.OutputSchema = raw(schemaForType(reflect.TypeOf(idalloc.Result{}), map[reflect.Type]bool{}))
		case "note_move":
			entry.contract.OutputSchema = raw(schemaForType(reflect.TypeOf(actions.MoveSummary{}), map[reflect.Type]bool{}))
		case "note_rename_heading":
			entry.contract.OutputSchema = raw(schemaForType(reflect.TypeOf(actions.RenameHeadingResult{}), map[reflect.Type]bool{}))
		case "graph_path":
			entry.contract.OutputSchema = raw(schemaForType(reflect.TypeOf(graphalg.PathResult{}), map[reflect.Type]bool{}))
		case "node_link":
			entry.contract.OutputSchema = raw(object([]string{"ensure", "locators"}, map[string]codeSchema{
				"ensure": str(), "locators": codeSchema{"type": "object", "additionalProperties": schemaForType(reflect.TypeOf(ontology.NodeLocator{}), map[reflect.Type]bool{})},
			}))
		case "find_connections":
			chunk := object([]string{"heading", "breadcrumb", "chunkIndex", "score", "queryHeading", "queryBreadcrumb", "queryChunkIndex", "queryText"}, map[string]codeSchema{"heading": str(), "breadcrumb": str(), "chunkIndex": integer(), "score": {"type": "number"}, "queryHeading": str(), "queryBreadcrumb": str(), "queryChunkIndex": integer(), "queryText": str(), "text": str()})
			match := object([]string{"path", "title", "score", "heading", "breadcrumb", "chunkIndex", "queryHeading", "queryBreadcrumb", "queryChunkIndex", "queryText", "reason", "chunks"}, map[string]codeSchema{"path": str(), "title": str(), "score": {"type": "number"}, "heading": str(), "breadcrumb": str(), "chunkIndex": integer(), "queryHeading": str(), "queryBreadcrumb": str(), "queryChunkIndex": integer(), "queryText": str(), "reason": str(), "text": str(), "chunks": {"type": "array", "items": chunk}})
			entry.contract.OutputSchema = raw(object([]string{"inputType", "matches", "count"}, map[string]codeSchema{"inputType": enumeration("note", "text"), "matches": {"type": "array", "items": match}, "count": integer(), "sessionId": str(), "dedupeHits": integer(), "note": str(), "skipped": integer()}))
		case "validate":
			entry.contract.OutputSchema = raw(codeSchema{"oneOf": []any{
				schemaForType(reflect.TypeOf(actions.ValidationCatalog{}), map[reflect.Type]bool{}),
				schemaForType(reflect.TypeOf(validationrun.ValidationResult{}), map[reflect.Type]bool{}),
			}})
		case "query_recipe":
			recipeArray := codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(queryrecipe.Recipe{}), map[reflect.Type]bool{})}
			summaryArray := codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(queryrecipe.RecipeSummary{}), map[reflect.Type]bool{})}
			issueArray := codeSchema{"type": "array", "items": schemaForType(reflect.TypeOf(queryrecipe.Issue{}), map[reflect.Type]bool{})}
			entry.contract.OutputSchema = raw(codeSchema{"oneOf": []any{
				object([]string{"recipes"}, map[string]codeSchema{"recipes": summaryArray, "issues": issueArray}),
				object([]string{"ok", "issueCount", "recipes"}, map[string]codeSchema{"ok": boolean(), "issueCount": integer(), "recipes": recipeArray, "issues": issueArray, "dependencies": unknownObject()}),
				schemaForType(reflect.TypeOf(queryrecipe.RunResult{}), map[reflect.Type]bool{}),
			}})
		case "view":
			entry.contract.OutputSchema = raw(codeSchema{"oneOf": []any{
				schemaForType(reflect.TypeOf(appviews.Catalog{}), map[reflect.Type]bool{}),
				schemaForType(reflect.TypeOf(appviews.CatalogEntry{}), map[reflect.Type]bool{}),
				object([]string{"ok", "issueCount", "catalog"}, map[string]codeSchema{"ok": boolean(), "issueCount": integer(), "catalog": schemaForType(reflect.TypeOf(appviews.Catalog{}), map[reflect.Type]bool{}), "issues": objectsSchema()}),
				schemaForType(reflect.TypeOf(appviews.ExecuteResponse{}), map[reflect.Type]bool{}),
				schemaForType(reflect.TypeOf(appviews.EjectResult{}), map[reflect.Type]bool{}),
			}})
		}
		applyCodeContractMetadata(name, entry.contract)
		toolCatalog[i].CodeContract = entry.contract
		toolCatalog[i].CodeCLILeaves = entry.leaves
	}
}

// codeSurfaceSchema describes the compact catalog returned by agent start.
// The full contracts remain available only through selected code describe calls.
func codeSurfaceSchema() codeSchema {
	operation := object([]string{"name", "method", "summary", "effects"}, map[string]codeSchema{
		"name": str(), "method": str(), "summary": str(), "effects": stringsSchema(),
	})
	return object([]string{"formatVersion", "versionHash", "operations", "examples"}, map[string]codeSchema{
		"formatVersion": str(), "versionHash": str(),
		"operations": {"type": "array", "items": operation}, "examples": stringsSchema(),
	})
}

func sharedResponseTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		"files":               reflect.TypeOf(mcpapi.FilesResponse{}),
		"file_context":        reflect.TypeOf(mcpapi.ContextTextResponse{}),
		"vault_context":       reflect.TypeOf(mcpapi.ContextTextResponse{}),
		"semantic_query":      reflect.TypeOf(mcpapi.SemanticQueryResponse{}),
		"code_symbol":         reflect.TypeOf(mcpapi.CodeSymbolResponse{}),
		"code_references":     reflect.TypeOf(mcpapi.CodeReferencesResponse{}),
		"code_symbol_context": reflect.TypeOf(mcpapi.CodeSymbolContextResponse{}),
		"check_paths": reflect.TypeOf(struct {
			Paths []mcpapi.PathCheck `json:"paths"`
		}{}),
		"external_references": reflect.TypeOf(mcpapi.ExternalReferencesResponse{}),
		"list_tags":           reflect.TypeOf(mcpapi.TagListResponse{}),
		"list_properties":     reflect.TypeOf(mcpapi.PropertyListResponse{}),
		"community_list":      reflect.TypeOf(mcpapi.CommunityListResponse{}),
		"report":              reflect.TypeOf(mcpapi.ReportResponse{}),
		"vault_health":        reflect.TypeOf(mcpapi.VaultHealthResponse{}),
	}
}

// schemaForType projects the ordinary JSON-bearing Go shapes used by handler
// responses. Interfaces and maps remain dynamic at their exact field, rather
// than turning the entire operation result into an untyped object.
func schemaForType(typ reflect.Type, visiting map[reflect.Type]bool) codeSchema {
	if typ.Kind() == reflect.Pointer {
		return codeSchema{"anyOf": []any{schemaForType(typ.Elem(), visiting), codeSchema{"type": "null"}}}
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return unknownValue()
	}
	if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
		return codeSchema{"type": []any{"string", "null"}}
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return codeSchema{"type": "string", "format": "date-time"}
	}
	if visiting[typ] {
		return unknownObject()
	}
	switch typ.Kind() {
	case reflect.String:
		return str()
	case reflect.Bool:
		return boolean()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return integer()
	case reflect.Float32, reflect.Float64:
		return codeSchema{"type": "number"}
	case reflect.Slice:
		return codeSchema{"type": []any{"array", "null"}, "items": schemaForType(typ.Elem(), visiting)}
	case reflect.Array:
		return codeSchema{"type": "array", "items": schemaForType(typ.Elem(), visiting)}
	case reflect.Map:
		if typ.Key().Kind() == reflect.String {
			return codeSchema{"type": []any{"object", "null"}, "additionalProperties": schemaForType(typ.Elem(), visiting)}
		}
		return unknownObject()
	case reflect.Interface:
		return unknownValue()
	case reflect.Struct:
		visiting[typ] = true
		defer delete(visiting, typ)
		properties := map[string]codeSchema{}
		required := []string{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if field.Anonymous && name == "" {
				embedded := schemaForType(field.Type, visiting)
				if embeddedProperties, ok := embedded["properties"].(map[string]any); ok {
					for key, rawProperty := range embeddedProperties {
						if property, ok := rawProperty.(codeSchema); ok {
							properties[key] = property
						} else if property, ok := rawProperty.(map[string]any); ok {
							properties[key] = codeSchema(property)
						}
					}
				}
				for _, rawRequired := range schemaArray(embedded["required"]) {
					if key, ok := rawRequired.(string); ok {
						required = append(required, key)
					}
				}
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = schemaForType(field.Type, visiting)
			if options != "omitempty" && !strings.Contains(options, ",omitempty") {
				required = append(required, name)
			}
		}
		return object(required, properties)
	default:
		return unknownObject()
	}
}
