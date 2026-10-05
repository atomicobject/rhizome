package agentapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeOperationCatalogCoversConcreteCLISurface(t *testing.T) {
	want := map[string]bool{
		"check_paths": true, "evaluate_batch": true, "evaluate": true, "files": true, "file_context": true, "vault_context": true, "semantic_query": true,
		"code_symbol": true, "code_references": true, "code_symbol_context": true, "external_references": true,
		"find_connections": true, "graph_path": true, "list_tags": true, "list_properties": true,
		"community_list": true, "report": true, "vault_health": true, "ontology_query_schema": true,
		"ontology_query": true, "query_recipe": true, "node_link": true, "note_rename_heading": true,
		"start": true, "ontology_reference": true, "ontology_authoring_guide": true, "ontology_inspect": true,
		"view": true, "validate": true, "current_user": true, "next_id": true, "code_rationale": true,
		"surface": true, "note_move": true,
	}
	leaves := map[string]string{}
	for _, descriptor := range CodeOperationDescriptors() {
		require.Truef(t, want[descriptor.Name], "unexpected code operation %q", descriptor.Name)
		delete(want, descriptor.Name)
		require.NotEmpty(t, descriptor.CodeCLILeaves)
		require.Truef(t, descriptor.Handler != nil || descriptor.CodeLocal != nil,
			"code operation %q has no executable route", descriptor.Name)
		if descriptor.Kind == ToolKindLocal {
			require.NotNilf(t, descriptor.CodeLocal, "local code operation %q has no local route", descriptor.Name)
		}
		for _, leaf := range descriptor.CodeCLILeaves {
			previous, duplicate := leaves[leaf]
			require.Falsef(t, duplicate, "CLI leaf %q covered by both %q and %q", leaf, previous, descriptor.Name)
			leaves[leaf] = descriptor.Name
		}
		require.Len(t, descriptor.CodeContract.Interpretation, 1, descriptor.Name)
		require.NotEmpty(t, descriptor.CodeContract.Interpretation[0], descriptor.Name)
		require.True(t, json.Valid(descriptor.CodeContract.InputSchema))
		require.True(t, json.Valid(descriptor.CodeContract.OutputSchema))
		var output map[string]any
		require.NoError(t, json.Unmarshal(descriptor.CodeContract.OutputSchema, &output))
		if output["type"] == "object" {
			require.NotEmpty(t, output["properties"], descriptor.Name)
		}
	}
	require.Empty(t, want)
	require.Len(t, leaves, 44)
}

func TestCodeCatalogOwnsLocalOverrides(t *testing.T) {
	for _, name := range []string{
		"surface", "start", "current_user", "next_id", "ontology_query_schema",
		"ontology_query", "ontology_reference", "ontology_authoring_guide", "ontology_inspect",
		"query_recipe", "view", "validate", "code_rationale", "note_rename_heading", "note_move",
	} {
		descriptor, ok := CodeOperationDescriptor(name)
		require.True(t, ok, name)
		require.NotNilf(t, descriptor.CodeLocal, "code operation %q must select its local override in the catalog", name)
	}
}

func TestValidateCodeInputUsesSelectedContract(t *testing.T) {
	require.NoError(t, ValidateCodeInput("files", map[string]any{"inputs": []any{"README.md"}, "includeContent": true}))
	require.NoError(t, ValidateCodeInput("files", map[string]any{"continuationToken": "opaque"}))
	require.ErrorContains(t, ValidateCodeInput("files", map[string]any{"inputs": []any{"README.md"}, "unknown": true}), "unknown field")
	require.ErrorContains(t, ValidateCodeInput("query_recipe", map[string]any{}), "op is required")
	require.NoError(t, ValidateCodeInput("query_recipe", map[string]any{"op": "run"}))
	for _, operation := range []string{"query_recipe", "view"} {
		input := map[string]any{"id": "fixture", "inputs": map[string]any{"list": []any{"a"}, "object": map[string]any{"n": 1}, "null": nil, "number": 3, "bool": true}}
		if operation == "query_recipe" {
			input["op"] = "run"
		} else {
			input["action"] = "run"
		}
		require.NoError(t, ValidateCodeInput(operation, input))
	}
	require.ErrorContains(t, ValidateCodeInput("query_recipe", map[string]any{"op": "show"}), "list, validate, run")
	require.NoError(t, ValidateCodeInput("ontology_query", map[string]any{"query": "query { types { name } }", "variables": map[string]any{}}))
}

func TestCodeContractsDescribeDefaultsAndConditionalInputs(t *testing.T) {
	for _, test := range []struct {
		operation string
		field     string
		defaulted any
	}{
		{"files", "includeContent", true},
		{"files", "dedupe", true},
		{"code_symbol", "contextLines", float64(3)},
		{"code_references", "limit", float64(20)},
		{"code_symbol_context", "includeTests", true},
		{"find_connections", "limit", float64(25)},
		{"vault_health", "staleDays", float64(90)},
		{"next_id", "count", float64(1)},
	} {
		descriptor, ok := CodeOperationDescriptor(test.operation)
		require.True(t, ok, test.operation)
		property := codeInputProperty(t, descriptor.CodeContract.InputSchema, test.field)
		require.NotEmpty(t, property["description"], test.operation+"."+test.field)
		require.Equal(t, test.defaulted, property["default"], test.operation+"."+test.field)
	}

	for _, test := range []struct {
		operation string
		valid     map[string]any
		invalid   map[string]any
	}{
		{"files", map[string]any{"continuationToken": "opaque"}, map[string]any{}},
		{"semantic_query", map[string]any{"queries": []any{"indexing"}, "continuationToken": "opaque"}, map[string]any{"continuationToken": "opaque"}},
		{"view", map[string]any{"action": "show", "id": "inbox"}, map[string]any{"action": "show"}},
		{"find_connections", map[string]any{"text": "related notes"}, map[string]any{}},
		{"note_move", map[string]any{"source": "old.md", "target": "new.md"}, map[string]any{"sources": []any{"old.md"}}},
	} {
		require.NoError(t, ValidateCodeInput(test.operation, test.valid), test.operation)
		require.Error(t, ValidateCodeInput(test.operation, test.invalid), test.operation)
	}
}

func codeInputProperty(t *testing.T, raw json.RawMessage, name string) map[string]any {
	t.Helper()
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	properties := schema["properties"].(map[string]any)
	property, ok := properties[name].(map[string]any)
	require.True(t, ok, name)
	return property
}

func TestReflectedSchemasPreserveKnownNestedAndEmbeddedFields(t *testing.T) {
	files, ok := CodeOperationDescriptor("files")
	require.True(t, ok)
	var filesSchema map[string]any
	require.NoError(t, json.Unmarshal(files.CodeContract.OutputSchema, &filesSchema))
	fileItems := filesSchema["properties"].(map[string]any)["files"].(map[string]any)["items"].(map[string]any)
	require.Contains(t, fileItems["properties"], "path")
	require.Contains(t, fileItems["properties"], "content")

	validation, ok := CodeOperationDescriptor("validate")
	require.True(t, ok)
	var validationSchema map[string]any
	require.NoError(t, json.Unmarshal(validation.CodeContract.OutputSchema, &validationSchema))
	alternatives := validationSchema["oneOf"].([]any)
	resultProperties := alternatives[1].(map[string]any)["properties"]
	require.Contains(t, resultProperties, "selectedChecks")
	require.Contains(t, resultProperties, "selector")
}

func TestStartContractIncludesCompactCodeCatalog(t *testing.T) {
	start, ok := CodeOperationDescriptor("start")
	require.True(t, ok)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(start.CodeContract.OutputSchema, &schema))
	properties := schema["properties"].(map[string]any)
	code := properties["code"].(map[string]any)
	codeProperties := code["properties"].(map[string]any)
	require.Equal(t, []any{"formatVersion", "versionHash", "operations", "examples"}, code["required"])
	require.Contains(t, codeProperties, "operations")
	require.Contains(t, codeProperties, "examples")
	operation := codeProperties["operations"].(map[string]any)["items"].(map[string]any)
	require.Equal(t, []any{"name", "method", "summary", "effects"}, operation["required"])
	require.NotContains(t, operation["properties"], "inputSchema")
	require.NotContains(t, operation["properties"], "outputSchema")
}

func TestKnownHandlerResultsKeepTheirActualFields(t *testing.T) {
	for operation, fields := range map[string][]string{
		"find_connections":    {"inputType", "matches", "count"},
		"graph_path":          {"From", "To", "Hops", "Path"},
		"node_link":           {"ensure", "locators"},
		"note_rename_heading": {"path", "oldHeading", "newHeading", "applied", "matchedReferences", "rewritten"},
	} {
		descriptor, ok := CodeOperationDescriptor(operation)
		require.True(t, ok)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(descriptor.CodeContract.OutputSchema, &schema))
		properties := schema["properties"]
		for _, field := range fields {
			require.Contains(t, properties, field, operation)
		}
	}
}

func TestOutputSchemasRetainJSONNullability(t *testing.T) {
	type response struct {
		Label  *string        `json:"label"`
		Items  []string       `json:"items"`
		Counts map[string]int `json:"counts"`
	}
	payload, err := json.Marshal(response{})
	require.NoError(t, err)
	require.JSONEq(t, `{"label":null,"items":null,"counts":null}`, string(payload))
	schema := schemaForType(reflect.TypeOf(response{}), map[reflect.Type]bool{})
	properties := schema["properties"].(map[string]any)
	require.Contains(t, properties["label"].(codeSchema), "anyOf")
	require.Equal(t, []any{"array", "null"}, properties["items"].(codeSchema)["type"])
	require.Equal(t, []any{"object", "null"}, properties["counts"].(codeSchema)["type"])
}

func TestCodeSemanticQueryAcceptsMixedPerQueryModes(t *testing.T) {
	require.NoError(t, ValidateCodeInput("semantic_query", map[string]any{"queries": []any{"plain text", map[string]any{"text": "overview question", "mode": "overview"}, map[string]any{"text": "docs question", "mode": "doc_code"}}, "compact": true}))
	require.Error(t, ValidateCodeInput("semantic_query", map[string]any{"queries": []any{map[string]any{"mode": "overview"}}}))
	require.Error(t, ValidateCodeInput("semantic_query", map[string]any{"queries": []any{42}}))
	require.NoError(t, ValidateCodeInput("ontology_reference", map[string]any{"type": "Project", "compact": true}))
	require.ErrorContains(t, ValidateCodeInput("ontology_reference", map[string]any{"compact": true}), "type is required")
}
