package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

func OntologyQuerySchemaTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx
		typeName, typeOK := request.GetArguments()["type"].(string)
		if _, present := request.GetArguments()["type"]; present && !typeOK {
			return mcp.NewToolResultError("ontology_query_schema: type must be a string"), nil
		}
		schema, err := ontology.LoadSchema(config.VaultPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("ontology_query_schema: %v", err)), nil
		}
		execSchema, err := ontologyquery.BuildExecutableSchema(schema)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("ontology_query_schema: %v", err)), nil
		}
		payload, err := ontologyquery.DiscoverSchema(schema, execSchema, typeName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("ontology_query_schema: %v", err)), nil
		}
		return respondJSON(payload, "marshal ontology query schema")
	}
}

func OntologyQueryTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		rawQuery, _ := args["query"].(string)
		rawQuery = strings.TrimSpace(rawQuery)
		if rawQuery == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		variables, err := objectArg(args, "variables")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err := runOntologyQuery(ctx, config, rawQuery, variables)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("ontology_query: %v", err)), nil
		}
		return respondJSON(resp, "marshal ontology query result")
	}
}

// QueryRecipeTool implements the query_recipe MCP tool.
//
// NOTE: this tool is co-located with ontology query helpers because it shares
// the same schema-loading and query-execution plumbing, but recipes themselves
// can target runtime-enriched roots (`code`, `agent`) that are not strictly
// ontology-bound. The tool name was promoted out of `ontology_query_recipe`
// alongside the CLI rename described in [[saved-query-recipes#^spec-0052-us5-ac6]].
func QueryRecipeTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		op, _ := args["op"].(string)
		op = strings.TrimSpace(op)
		if op == "" {
			return mcp.NewToolResultError("op is required"), nil
		}
		recipes, issues := loadQueryRecipes(config, stringArgMCP(args, "path"))
		sort.SliceStable(recipes, func(i, j int) bool { return recipes[i].ID < recipes[j].ID })
		switch op {
		case "list":
			return respondJSON(map[string]any{"recipes": queryrecipe.Summaries(recipes), "issues": issues}, "marshal query recipes")
		case "validate":
			execSchema, err := loadOntologyQueryExecutableSchema(config.VaultPath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query_recipe: %v", err)), nil
			}
			result := queryrecipe.Validate(recipes, execSchema)
			issues = append(issues, result.Issues...)
			return respondJSON(map[string]any{"ok": len(issues) == 0, "issueCount": len(issues), "recipes": result.Recipes, "issues": issues}, "marshal query recipe validation")
		case "run":
			if len(issues) > 0 {
				return respondJSON(map[string]any{"ok": false, "issueCount": len(issues), "recipes": recipes, "issues": issues}, "marshal query recipe validation")
			}
			recipe, err := selectQueryRecipeMCP(recipes, stringArgMCP(args, "id"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			inputs, err := inputStringMapArg(args, "inputs")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			inputs, err = queryrecipe.MergeAnchorInput(recipe, inputs, stringArrayArgMCP(args, "anchor"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			execSchema, err := loadOntologyQueryExecutableSchema(config.VaultPath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query_recipe: %v", err)), nil
			}
			compiled, recipeIssues := queryrecipe.Compile(recipe, execSchema, inputs)
			if len(recipeIssues) > 0 {
				return respondJSON(map[string]any{"ok": false, "issueCount": len(recipeIssues), "recipes": []queryrecipe.Recipe{recipe}, "issues": recipeIssues}, "marshal query recipe validation")
			}
			result, err := runOntologyQuery(ctx, config, compiled.Query, compiled.Variables)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query_recipe: %v", err)), nil
			}
			return respondJSON(queryrecipe.RunResult{
				Recipe:         recipe,
				Inputs:         inputs,
				Variables:      compiled.Variables,
				Query:          compiled.Query,
				OutputContract: recipe.OutputContract,
				Result:         result,
			}, "marshal query recipe run")
		default:
			return mcp.NewToolResultError("op must be one of list, validate, run"), nil
		}
	}
}

func runOntologyQuery(ctx context.Context, config Config, rawQuery string, variables map[string]any) (ontologyquery.Result, error) {
	schema, err := ontology.LoadSchema(config.VaultPath)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	prepared, errs := ontologyquery.PrepareWithVariables(execSchema, rawQuery, variables)
	if len(errs) > 0 {
		data, _ := json.Marshal(map[string]any{"errors": errs})
		return ontologyquery.Result{}, fmt.Errorf("%s", string(data))
	}
	if err := config.NoteMetadata.Validate(); err != nil {
		return ontologyquery.Result{}, fmt.Errorf("ontology query requires note metadata runtime: %w", err)
	}
	store := config.GetIntelStore()
	cleanup := func() {}
	if store == nil {
		opened, openedCleanup, err := obsidian.OpenIntelStoreBestEffort(config.VaultPath, true)
		if err != nil {
			return ontologyquery.Result{}, err
		}
		store = opened
		if openedCleanup != nil {
			cleanup = openedCleanup
		}
	}
	defer cleanup()
	if store == nil {
		return ontologyquery.Result{}, fmt.Errorf("ontology query requires an indexed vault; run `rzm index`")
	}
	noteReader := resolveNoteReader(config)
	rt, err := ontology.EnsureFreshRuntimeWithStore(ctx, config.NoteMetadata, config.VaultDef, noteReader, store)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	deps := ontologyquery.Deps{
		VaultDef:   config.VaultDef,
		NoteReader: noteReader,
		Store:      store,
		Service:    ontology.NewService(config.VaultDef, noteReader, store, rt.Schema),
	}
	if formats, err := config.NoteMetadata.FormatRuntime(); err == nil {
		deps.NoteFormats = formats
	}
	if prepared.UsesSemantic {
		noteEmbOn, _, noteProvider, _ := config.NoteEmbeddings()
		if !noteEmbOn || noteProvider == nil {
			return ontologyquery.Result{}, fmt.Errorf("semantic ontology query requires embeddings; run `rzm index`")
		}
		deps.SemanticSearcher = &semantic.Searcher{NoteProvider: noteProvider, IntelStore: store}
	}
	if prepared.UsesSearch {
		_, _, noteProvider, _ := config.NoteEmbeddings()
		_, _, codeProvider := config.CodeEmbeddingsState()
		deps.NoteSearcher = unifiedsearch.NoteSearcher{Runtime: unifiedsearch.Options{
			VaultPath:    config.VaultPath,
			VaultDef:     config.VaultDef,
			IntelStore:   store,
			NoteProvider: noteProvider,
			CodeProvider: codeProvider,
		}}
	}
	return ontologyquery.ExecutePrepared(ctx, deps, rt.Schema, execSchema, prepared), nil
}

func loadOntologyQueryExecutableSchema(vaultPath string) (*ontologyquery.ExecutableSchema, error) {
	schema, err := ontology.LoadSchema(vaultPath)
	if err != nil {
		return nil, err
	}
	return ontologyquery.BuildExecutableSchema(schema)
}

func loadQueryRecipes(config Config, path string) ([]queryrecipe.Recipe, []queryrecipe.Issue) {
	if strings.TrimSpace(path) != "" {
		return queryrecipe.LoadPath(path)
	}
	return queryrecipe.LoadDefaultSources(config.VaultPath)
}

func selectQueryRecipeMCP(recipes []queryrecipe.Recipe, id string) (queryrecipe.Recipe, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if len(recipes) == 1 {
			return recipes[0], nil
		}
		return queryrecipe.Recipe{}, fmt.Errorf("id is required when %d recipes are available", len(recipes))
	}
	for _, recipe := range recipes {
		if recipe.ID == id {
			return recipe, nil
		}
	}
	return queryrecipe.Recipe{}, fmt.Errorf("recipe %q not found", id)
}

func objectArg(args map[string]interface{}, key string) (map[string]any, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	out, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
	return map[string]any(out), nil
}

func inputStringMapArg(args map[string]interface{}, key string) (map[string]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return map[string]string{}, nil
	}
	obj, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
	out := map[string]string{}
	for name, value := range obj {
		switch typed := value.(type) {
		case string:
			out[name] = typed
		case float64, bool, json.Number:
			out[name] = fmt.Sprint(typed)
		case nil:
			out[name] = ""
		default:
			return nil, fmt.Errorf("input %q must be a string, number, boolean, or null", name)
		}
	}
	return out, nil
}

func stringArgMCP(args map[string]interface{}, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func stringArrayArgMCP(args map[string]interface{}, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
