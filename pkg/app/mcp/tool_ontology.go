package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/app/queryengine/runtimeproviders"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"

	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/mark3labs/mcp-go/mcp"
)

func OntologyQuerySchemaTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx
		typeName, typeOK := request.GetArguments()["type"].(string)
		if _, present := request.GetArguments()["type"]; present && !typeOK {
			return mcp.NewToolResultError("ontology_query_schema: type must be a string"), nil
		}
		schema, err := ontoschema.LoadSchema(config.VaultPath)
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
	registry := queryrecipe.NewRegistry(queryrecipe.DefaultSourceRoots(config.VaultPath))
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		op, _ := args["op"].(string)
		op = strings.TrimSpace(op)
		if op == "" {
			return mcp.NewToolResultError("op is required"), nil
		}
		recipes, issues := registry.Catalog()
		if path := strings.TrimSpace(stringArgMCP(args, "path")); path != "" {
			recipes, issues = queryrecipe.LoadPath(path)
		}
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
			inputs, err := inputStringMapArg(args, "inputs")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			schema, err := ontoschema.LoadSchema(config.VaultPath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query_recipe: %v", err)), nil
			}
			execSchema, err := ontologyquery.BuildExecutableSchema(schema)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query_recipe: %v", err)), nil
			}
			run, issues, err := queryrecipe.Run(ctx, recipes, issues, stringArgMCP(args, "id"), inputs, stringArrayArgMCP(args, "anchor"), execSchema,
				func(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
					return runPreparedOntologyQuery(ctx, config, schema, execSchema, prepared)
				})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("query_recipe: %v", err)), nil
			}
			if len(issues) > 0 {
				return respondJSON(map[string]any{"ok": false, "issueCount": len(issues), "recipes": []queryrecipe.Recipe{run.Recipe}, "issues": issues}, "marshal query recipe validation")
			}
			return respondJSON(run, "marshal query recipe run")
		default:
			return mcp.NewToolResultError("op must be one of list, validate, run"), nil
		}
	}
}

func runOntologyQuery(ctx context.Context, config Config, rawQuery string, variables map[string]any) (ontologyquery.Result, error) {
	schema, err := ontoschema.LoadSchema(config.VaultPath)
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
	return runPreparedOntologyQuery(ctx, config, schema, execSchema, prepared)
}

func runPreparedOntologyQuery(ctx context.Context, config Config, schema *ontoschema.Schema, execSchema *ontologyquery.ExecutableSchema, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {

	if prepared.Introspection {
		engine, err := queryengine.New(schema, execSchema, ontologyquery.Deps{})
		if err != nil {
			return ontologyquery.Result{}, err
		}
		return engine.Run(ctx, prepared)
	}
	if provider, ok := config.Runtime.(interface{ Configuration() repoconfig.Resolved }); ok {
		resolved := provider.Configuration()
		config.RepositoryConfig = &resolved
	}
	if config.RepositoryConfig == nil {
		resolved, err := repoconfig.Load(config.VaultPath)
		if err != nil {
			return ontologyquery.Result{}, err
		}
		config.RepositoryConfig = &resolved
	} else if err := config.RepositoryConfig.ValidateFor(config.VaultPath); err != nil {
		return ontologyquery.Result{}, err
	}

	if err := config.NoteMetadata.Validate(); err != nil {
		return ontologyquery.Result{}, fmt.Errorf("ontology query requires note metadata runtime: %w", err)
	}
	store := config.GetIntelStore()
	cleanup := func() {}
	if store == nil {
		opened, openedCleanup, err := indexstore.Open(config.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, Config: &config.RepositoryConfig.Code, RequireEnabled: true})
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
	rt, err := ontology.PublishedRuntimeWithStore(ctx, config.NoteMetadata, config.VaultDef, store)

	if err != nil {
		return ontologyquery.Result{}, err
	}
	if !rt.Ready {
		if owner, ok := config.Runtime.(interface{ RequestOntologyRefresh() bool }); ok {
			owner.RequestOntologyRefresh()
		}
		return ontologyquery.Result{}, fmt.Errorf("ontology index is not ready; run `rzm index`")
	}
	deps := ontologyquery.Deps{
		VaultDef:   config.VaultDef,
		NoteReader: noteReader,
		Store:      store,
	}
	if formats, err := config.NoteMetadata.FormatRuntime(); err == nil {
		deps.NoteFormats = formats
	}
	_, _, noteProvider, _ := config.NoteEmbeddings()
	_, _, codeProvider := config.CodeEmbeddingsState()
	deps, err = queryengine.Dependencies(deps, unifiedsearch.Options{Config: config.RepositoryConfig, VaultPath: config.VaultPath, VaultDef: config.VaultDef, IntelStore: store, NoteProvider: noteProvider, CodeProvider: codeProvider})
	if err != nil {
		return ontologyquery.Result{}, err
	}
	_, anchors := config.CodeAnchorState()
	providers := runtimeproviders.New(schema, execSchema, deps, anchors)
	deps.OntologyRuntime, deps.CodeRuntime = providers, providers
	if rt.Schema.Hash != schema.Hash {
		return ontologyquery.Result{}, fmt.Errorf("ontology schema changed during query preparation")
	}
	engine, err := queryengine.New(schema, execSchema, deps)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	return engine.Run(ctx, prepared)
}

func loadOntologyQueryExecutableSchema(vaultPath string) (*ontologyquery.ExecutableSchema, error) {
	schema, err := ontoschema.LoadSchema(vaultPath)
	if err != nil {
		return nil, err
	}
	return ontologyquery.BuildExecutableSchema(schema)
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
