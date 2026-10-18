package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/vektah/gqlparser/v2/ast"
	"strings"
)

func (e *executor) ontologyQueryPlan(ctx context.Context, args map[string]any) OntologyQueryPlan {
	typeName := strings.TrimSpace(stringValue(args["type"]))
	first := boundedFirst(args["first"], 20, publicRootFirstMax)
	root := ""
	if typeName != "" {
		root = queryRootFieldName(typeName)
	}
	plan := OntologyQueryPlan{
		Type:       typeName,
		Root:       root,
		Execution:  "projection_fallback",
		Projection: "required",
		First:      first,
	}
	noteType := e.schema.Types[typeName]
	if noteType == nil {
		plan.Warnings = append(plan.Warnings, diag.Warning{Code: "unknown_type", Message: fmt.Sprintf("ontology type %q is not defined", typeName)})
		return plan
	}
	if noteType.Role != ontoschema.TypeRoleEmbeddedNode && noteType.Role != ontoschema.TypeRoleNote {
		plan.Warnings = append(plan.Warnings, diag.Warning{Code: "unsupported_root_role", Message: "indexed root planning applies to note and embedded-node roots"})
		return plan
	}
	queryArgs := map[string]any{"first": first}
	if filters, ok := args["filters"].([]any); ok {
		queryArgs["filters"] = filters
	}
	if sortSpec, ok := args["sort"].([]any); ok {
		queryArgs["sort"] = sortSpec
	}
	rootPlan := e.indexedOntologyNodeRootPlan(ctx, typeName, queryArgs)
	residualFilters, residualSort, planWarnings := graphqlFiltersFromInputs(rootPlan.residual), graphqlSortsFromInputs(rootPlan.sort), rootPlan.warnings
	plan.Warnings = append(plan.Warnings, planWarnings...)
	plan.PushedFilters, plan.ResidualFilters = explainFilterPlan(args["filters"], residualFilters)
	plan.PushedSort, plan.ResidualSort = explainSortPlan(args["sort"], residualSort)
	if len(plan.ResidualFilters) > 0 || len(plan.ResidualSort) > 0 {
		plan.Execution = "indexed_with_residual"
		plan.Warnings = append(plan.Warnings, diag.Warning{Code: "residual_constraints", Message: "some filters or sort terms cannot be pushed into indexed field rows"})
	} else {
		plan.Execution = "indexed"
	}
	if e.deps.Store == nil {
		plan.Execution = "index_unavailable"
		plan.Warnings = append(plan.Warnings, diag.Warning{Code: "index_unavailable", Message: "indexed store is unavailable"})
	}
	selectionSet := selectionSetFromNames(stringListValue(args["select"]))
	if len(selectionSet) > 0 && !e.ontologyNodeRootSelectionNeedsProjection(typeName, selectionSet) {
		plan.Projection = "not_required"
	}
	if e.deps.Store != nil {
		rows, err := e.deps.Store.OntologyNodesByTypePlan(ctx, readmodel.NodeQueryPlan{TypeNames: []string{typeName}, Limit: 1})
		if err != nil {
			plan.Warnings = append(plan.Warnings, diag.Warning{Code: "index_probe_failed", Message: err.Error()})
		} else if len(rows) == 0 {
			plan.Warnings = append(plan.Warnings, diag.Warning{Code: "indexed_root_empty", Message: "indexed root returned no rows; this may be legitimate or may indicate stale/missing index rows"})
		}
	}
	return plan
}

// selectionSetFromNames synthesizes a minimal AST selection set from a list of
// field names so the queryPlan explain path can share the same projection
// decider as the real executor. Each entry becomes a leaf field with no
// sub-selection, which is enough for ontologyNodeRootSelectionNeedsProjection.
func selectionSetFromNames(names []string) ast.SelectionSet {
	if len(names) == 0 {
		return nil
	}
	out := make(ast.SelectionSet, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out = append(out, &ast.Field{Name: trimmed, Alias: trimmed})
	}
	return out
}

func explainFilterPlan(raw any, residual any) ([]string, []string) {
	all := explainFilters(raw)
	residualItems := explainFilters(residual)
	if len(residualItems) == 0 {
		return all, nil
	}
	residualSet := map[string]int{}
	for _, item := range residualItems {
		residualSet[item]++
	}
	pushed := make([]string, 0, len(all))
	for _, item := range all {
		if residualSet[item] > 0 {
			residualSet[item]--
			continue
		}
		pushed = append(pushed, item)
	}
	return pushed, residualItems
}

func explainSortPlan(raw any, residual any) ([]string, []string) {
	all := explainSort(raw)
	residualItems := explainSort(residual)
	if len(residualItems) == 0 {
		return all, nil
	}
	residualSet := map[string]int{}
	for _, item := range residualItems {
		residualSet[item]++
	}
	pushed := make([]string, 0, len(all))
	for _, item := range all {
		if residualSet[item] > 0 {
			residualSet[item]--
			continue
		}
		pushed = append(pushed, item)
	}
	return pushed, residualItems
}

func explainFilters(raw any) []string {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		filter, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fieldName := strings.TrimSpace(stringValue(filter["field"]))
		op := strings.TrimSpace(stringValue(filter["op"]))
		if op == "" {
			op = "eq"
		}
		out = append(out, fieldName+" "+op)
	}
	return out
}

func explainSort(raw any) []string {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		sortSpec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fieldName := strings.TrimSpace(stringValue(sortSpec["field"]))
		direction := strings.TrimSpace(stringValue(sortSpec["direction"]))
		if direction == "" {
			direction = "asc"
		}
		out = append(out, fieldName+" "+strings.ToLower(direction))
	}
	return out
}

func (e *executor) resolveOntologyQueryPlanSelectionSet(value OntologyQueryPlan, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		switch field.Name {
		case "type":
			out[key] = value.Type
		case "root":
			out[key] = value.Root
		case "execution":
			out[key] = value.Execution
		case "projection":
			out[key] = value.Projection
		case "first":
			out[key] = value.First
		case "pushedFilters":
			out[key] = stringsSliceAny(value.PushedFilters)
		case "residualFilters":
			out[key] = stringsSliceAny(value.ResidualFilters)
		case "pushedSort":
			out[key] = stringsSliceAny(value.PushedSort)
		case "residualSort":
			out[key] = stringsSliceAny(value.ResidualSort)
		case "warnings":
			out[key] = e.resolveWarnings(value.Warnings, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("unsupported queryPlan field %q", field.Name))
		}
	}
	return out
}
