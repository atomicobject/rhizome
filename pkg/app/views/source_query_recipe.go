package views

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"strings"
)

func (r defaultSourceResolver) resolveQueryRecipeSourceWithScope(ctx context.Context, scope *noderead.Scope, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	if r.opts.ExecSchema == nil {
		return SourceResult{Warnings: []diag.Warning{{Code: "view_source_unavailable", Message: "ontology query schema is unavailable"}}}, nil
	}
	registry := r.opts.QueryRecipes
	if registry == nil {
		registry = queryrecipe.NewRegistry(queryrecipe.DefaultSourceRoots(r.opts.VaultPath))
	}
	recipes, issues := registry.Catalog()
	inputs := map[string]string{}
	for key, value := range view.SourceSpec.Inputs {
		inputs[key] = value
	}
	for key, value := range req.Inputs {
		inputs[key] = value
	}
	run, issues, err := queryrecipe.Run(ctx, recipes, issues, view.SourceSpec.QueryRecipe, inputs, nil, r.opts.ExecSchema, func(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
		engine, err := queryengine.New(r.opts.Schema, r.opts.ExecSchema, r.opts.QueryDeps)
		if err != nil {
			return ontologyquery.Result{}, err
		}
		return engine.Run(ctx, prepared)
	})
	if errors.Is(err, queryrecipe.ErrNotFound) {
		return SourceResult{Warnings: []diag.Warning{{Code: "unknown_query_recipe", Message: err.Error()}}}, nil
	}
	if err != nil {
		return SourceResult{}, fmt.Errorf("%w: query recipe %q failed: %s", ErrInvalidRequest, view.SourceSpec.QueryRecipe, err)
	}
	if len(issues) > 0 {
		return SourceResult{Warnings: queryRecipeWarnings(issues)}, nil
	}
	result := run.Result
	var warnings []diag.Warning
	if len(result.Errors) > 0 {
		return SourceResult{}, fmt.Errorf("%w: query recipe %q failed: %s", ErrInvalidRequest, view.SourceSpec.QueryRecipe, queryErrorSummary(result.Errors))
	}
	items, rowWarnings := extractQueryRecipeRowsWithRecipePath(result, view.SourceSpec.ResultPath, run.OutputContract.RowPath)
	warnings = append(warnings, rowWarnings...)
	items, hydrateWarnings := r.hydrateRowsWithScope(ctx, scope, items)
	warnings = append(warnings, hydrateWarnings...)
	return SourceResult{Rows: items, Capabilities: r.ontologyEditCapabilitiesForTypes(ctx, scope, rowResolvedTypeNames(items)), Warnings: warnings}, nil
}
func queryErrorSummary(errors []ontologyquery.Error) string {
	parts := make([]string, 0, len(errors))
	for _, err := range errors {
		message := strings.TrimSpace(err.Message)
		if message == "" {
			message = "unknown query error"
		}
		if len(err.Path) > 0 {
			message = fmt.Sprintf("%s: %s", strings.Join(err.Path, "."), message)
		}
		parts = append(parts, message)
	}
	return strings.Join(parts, "; ")
}
func queryRecipeWarnings(issues []queryrecipe.Issue) []diag.Warning {
	out := make([]diag.Warning, 0, len(issues))
	for _, issue := range issues {
		out = append(out, diag.Warning{Code: issue.Code, Message: issue.Message, Path: issue.Path})
	}
	return out
}
func extractQueryRecipeRowsWithRecipePath(result ontologyquery.Result, sourcePath, recipeRowPath string) ([]TableRow, []diag.Warning) {
	path := strings.TrimSpace(sourcePath)
	if path == "" {
		path = strings.TrimSpace(recipeRowPath)
	}
	if path == "" {
		return nil, []diag.Warning{{Code: "view_result_path_required", Message: "query_recipe views must set source.resultPath or recipe outputContract.rowPath to the array of node-like rows"}}
	}
	values, found, isArray := extractResultArrayStrict(result.Data, path)
	if !found {
		return nil, []diag.Warning{{Code: "view_result_path_not_found", Message: resultPathNotFoundMessage(path)}}
	}
	if !isArray {
		return nil, []diag.Warning{{Code: "view_result_path_not_array", Message: fmt.Sprintf("query recipe result path %q did not resolve to an array", normalizeResultPath(path))}}
	}
	var rows []TableRow
	unresolved := 0
	for _, value := range values {
		obj, ok := value.(map[string]any)
		if !ok {
			unresolved++
			continue
		}
		row, ok := rowFromMap(obj)
		if !ok {
			unresolved++
			continue
		}
		rows = append(rows, row)
	}
	var warnings []diag.Warning
	if unresolved > 0 {
		warnings = append(warnings, diag.Warning{Code: "view_source_items_unresolved", Message: fmt.Sprintf("query recipe skipped %d row item(s) without object identity", unresolved)})
	}
	return rows, warnings
}
func rowResolvedTypeNames(rows []TableRow) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, row := range rows {
		typeName := firstNonEmpty(row.Ref.TypeName, row.ResolvedType)
		if strings.TrimSpace(typeName) == "" {
			continue
		}
		if _, ok := seen[typeName]; ok {
			continue
		}
		seen[typeName] = struct{}{}
		out = append(out, typeName)
	}
	return out
}
func resultPathNotFoundMessage(path string) string {
	return fmt.Sprintf("query recipe result path %q was not found in query result", normalizeResultPath(path))
}
func normalizeResultPath(path string) string {
	path = strings.TrimPrefix(strings.TrimSpace(path), "result.data.")
	path = strings.TrimPrefix(path, "data.")
	return path
}
func extractResultArrayStrict(data map[string]any, path string) ([]any, bool, bool) {
	path = normalizeResultPath(path)
	if path == "" {
		return nil, false, false
	}
	var current any = data
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, true, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false, false
		}
	}
	if arr, ok := current.([]any); ok {
		return arr, true, true
	}
	return nil, true, false
}
func rowFromMap(obj map[string]any) (TableRow, bool) {
	path := firstString(obj["path"], obj["notePath"])
	ref, hasRef := nodeRefFromMapValue(obj["ref"])
	if !hasRef {
		ref, hasRef = nodeRefFromMapValue(obj["nodeRef"])
	}
	if hasRef {
		path = firstNonEmpty(path, ref.NotePath)
	}
	if path == "" {
		return TableRow{}, false
	}
	if !hasRef {
		ref = ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKindNote
	}
	title := firstString(obj["title"], obj["name"])
	resolvedType := firstString(obj["resolvedType"], obj["type"])
	if ref.TypeName != "" {
		resolvedType = firstNonEmpty(resolvedType, ref.TypeName)
	}
	return TableRow{Ref: ref, Path: path, Title: title, ResolvedType: resolvedType, Fields: obj}, true
}
func nodeRefFromMapValue(value any) (ontology.NodeRef, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return ontology.NodeRef{}, false
	}
	ref := ontology.NodeRef{NotePath: firstString(obj["notePath"], obj["path"]), Fragment: firstString(obj["fragment"]), NodeID: firstString(obj["nodeId"], obj["nodeID"], obj["node_id"]), TypeName: firstString(obj["typeName"], obj["type"]), Kind: ontology.NodeKind(firstString(obj["kind"])), StartByte: intValue(obj["startByte"]), EndByte: intValue(obj["endByte"]), ParentID: firstString(obj["parentId"], obj["parentID"], obj["parent_id"]), Structural: firstString(obj["structuralFingerprint"], obj["structural"])}
	if ref.NotePath == "" {
		return ontology.NodeRef{}, false
	}
	return ref, true
}
func firstString(values ...any) string {
	for _, value := range values {
		if str, ok := value.(string); ok && strings.TrimSpace(str) != "" {
			return str
		}
	}
	return ""
}
func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		var parsed int
		_, _ = fmt.Sscan(strings.TrimSpace(typed), &parsed)
		return parsed
	default:
		return 0
	}
}
