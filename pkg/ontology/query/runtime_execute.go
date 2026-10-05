package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveRuntimeRoot(ctx context.Context, field *ast.Field, path []string) (any, bool) {
	switch field.Name {
	case "ontology":
		return e.resolveOntologyRuntimeSelectionSet(ctx, field.SelectionSet, path), true
	case "code":
		return e.resolveCodeRuntimeSelectionSet(ctx, field.SelectionSet, path), true
	default:
		return nil, false
	}
}

func (e *executor) resolveOntologyRuntimeSelectionSet(ctx context.Context, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			if frag, ok := selection.(*ast.InlineFragment); ok {
				for k, v := range e.resolveOntologyRuntimeSelectionSet(ctx, frag.SelectionSet, path) {
					out[k] = v
				}
				continue
			}
			e.addError(path, "unsupported selection on OntologyRuntime")
			continue
		}
		key := responseKey(field)
		childPath := append(path, key)
		args := e.fieldArgs(field)
		switch field.Name {
		case "schemaHash":
			out[key] = e.schema.Hash
		case "type":
			typeName := strings.TrimSpace(stringValue(args["name"]))
			nt := e.schema.Types[typeName]
			if nt == nil {
				out[key] = nil
				continue
			}
			out[key] = e.resolveOntologyTypeSelectionSet(nt, field.SelectionSet, childPath)
		case "types":
			first := boundedFirst(args["first"], 50, 200)
			typeNames := make([]string, 0, len(e.schema.Types))
			for name, nt := range e.schema.Types {
				if nt != nil {
					typeNames = append(typeNames, name)
				}
			}
			sort.Strings(typeNames)
			if first > 0 && len(typeNames) > first {
				typeNames = typeNames[:first]
			}
			rows := make([]any, 0, len(typeNames))
			for _, name := range typeNames {
				rows = append(rows, e.resolveOntologyTypeSelectionSet(e.schema.Types[name], field.SelectionSet, childPath))
			}
			out[key] = rows
		case "authoringGuide":
			typeName := strings.TrimSpace(stringValue(args["type"]))
			guide := e.unavailableAuthoringGuide(typeName, "ontology_runtime_unavailable", "ontology runtime provider is unavailable")
			if e.deps.OntologyRuntime != nil {
				value, err := e.deps.OntologyRuntime.AuthoringGuide(ctx, OntologyAuthoringGuideRequest{Type: typeName})
				if err != nil {
					e.addError(childPath, err.Error())
				} else {
					guide = value
				}
			}
			out[key] = e.resolveAuthoringGuideSelectionSet(guide, field.SelectionSet, childPath)
		case "nextId":
			typeName := strings.TrimSpace(stringValue(args["type"]))
			count := boundedFirst(args["count"], 1, idalloc.MaxBatchCount)
			paths := runtimeStringList(args["paths"])
			next := unavailableNextID(typeName, "ontology_runtime_unavailable", "ontology runtime provider is unavailable")
			if e.deps.OntologyRuntime != nil {
				value, err := e.deps.OntologyRuntime.NextID(ctx, OntologyNextIDRequest{Type: typeName, Count: count, Paths: paths})
				if err != nil {
					e.addError(childPath, err.Error())
				} else {
					next = value
				}
			}
			out[key] = e.resolveNextIDSelectionSet(next, field.SelectionSet, childPath)
		case "currentUser":
			currentUser := unavailableCurrentUser("ontology_runtime_unavailable", "ontology runtime provider is unavailable")
			if e.deps.OntologyRuntime != nil {
				value, err := e.deps.OntologyRuntime.CurrentUser(ctx)
				if err != nil {
					e.addError(childPath, err.Error())
				} else {
					currentUser = value
				}
			}
			out[key] = e.resolveCurrentUserSelectionSet(currentUser, field.SelectionSet, childPath)
		case "queryPlan":
			out[key] = e.resolveOntologyQueryPlanSelectionSet(e.ontologyQueryPlan(ctx, args), field.SelectionSet, childPath)
		default:
			e.addError(childPath, fmt.Sprintf("unsupported ontology runtime field %q", field.Name))
		}
	}
	return out
}

func unavailableCurrentUser(code, message string) OntologyCurrentUser {
	return OntologyCurrentUser{
		Configured: false,
		Found:      false,
		ErrorCode:  code,
		Error:      message,
		Warnings:   []RuntimeWarning{{Code: code, Message: message}},
	}
}

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
		plan.Warnings = append(plan.Warnings, RuntimeWarning{Code: "unknown_type", Message: fmt.Sprintf("ontology type %q is not defined", typeName)})
		return plan
	}
	if noteType.Role != ontology.TypeRoleEmbeddedNode && noteType.Role != ontology.TypeRoleNote {
		plan.Warnings = append(plan.Warnings, RuntimeWarning{Code: "unsupported_root_role", Message: "indexed root planning applies to note and embedded-node roots"})
		return plan
	}
	queryArgs := map[string]any{"first": first}
	if filters, ok := args["filters"].([]any); ok {
		queryArgs["filters"] = filters
	}
	if sortSpec, ok := args["sort"].([]any); ok {
		queryArgs["sort"] = sortSpec
	}
	_, residualFilters, residualSort, planWarnings := e.indexedOntologyNodeRootPlan(ctx, typeName, queryArgs)
	plan.Warnings = append(plan.Warnings, planWarnings...)
	plan.PushedFilters, plan.ResidualFilters = explainFilterPlan(args["filters"], residualFilters)
	plan.PushedSort, plan.ResidualSort = explainSortPlan(args["sort"], residualSort)
	if len(plan.ResidualFilters) > 0 || len(plan.ResidualSort) > 0 {
		plan.Execution = "indexed_with_residual"
		plan.Warnings = append(plan.Warnings, RuntimeWarning{Code: "residual_constraints", Message: "some filters or sort terms cannot be pushed into indexed field rows"})
	} else {
		plan.Execution = "indexed"
	}
	if e.deps.Store == nil {
		plan.Execution = "index_unavailable"
		plan.Warnings = append(plan.Warnings, RuntimeWarning{Code: "index_unavailable", Message: "indexed store is unavailable"})
	}
	selectionSet := selectionSetFromNames(stringListValue(args["select"]))
	if len(selectionSet) > 0 && !e.ontologyNodeRootSelectionNeedsProjection(typeName, selectionSet) {
		plan.Projection = "not_required"
	}
	if e.deps.Store != nil {
		rows, err := e.deps.Store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{TypeNames: []string{typeName}, Limit: 1})
		if err != nil {
			plan.Warnings = append(plan.Warnings, RuntimeWarning{Code: "index_probe_failed", Message: err.Error()})
		} else if len(rows) == 0 {
			plan.Warnings = append(plan.Warnings, RuntimeWarning{Code: "indexed_root_empty", Message: "indexed root returned no rows; this may be legitimate or may indicate stale/missing index rows"})
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

func (e *executor) resolveCurrentUserSelectionSet(value OntologyCurrentUser, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		switch field.Name {
		case "configured":
			out[key] = value.Configured
		case "ref":
			out[key] = emptyNil(value.Ref)
		case "found":
			out[key] = value.Found
		case "resolvedRef":
			out[key] = emptyNil(value.ResolvedRef)
		case "path":
			out[key] = emptyNil(value.Path)
		case "title":
			out[key] = emptyNil(value.Title)
		case "typeName":
			out[key] = emptyNil(value.TypeName)
		case "errorCode":
			out[key] = emptyNil(value.ErrorCode)
		case "error":
			out[key] = emptyNil(value.Error)
		case "warnings":
			out[key] = e.resolveWarnings(value.Warnings, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("unsupported currentUser field %q", field.Name))
		}
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

func (e *executor) resolveOntologyTypeSelectionSet(nt *ontology.NoteType, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		childPath := append(path, key)
		switch field.Name {
		case "name":
			out[key] = nt.Name
		case "role":
			out[key] = string(nt.Role)
		case "description":
			out[key] = nt.Description
		case "paths":
			out[key] = stringsSliceAny(nt.Paths)
		case "interfaces":
			out[key] = stringsSliceAny(nt.Implements)
		case "fields":
			rows := make([]any, 0, len(nt.Fields))
			for _, f := range nt.Fields {
				if f != nil {
					rows = append(rows, e.resolveOntologyFieldSelectionSet(f, field.SelectionSet, childPath))
				}
			}
			out[key] = rows
		default:
			e.addError(childPath, fmt.Sprintf("unsupported ontology type field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveOntologyFieldSelectionSet(f *ontology.Field, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		childPath := append(path, key)
		switch field.Name {
		case "name":
			out[key] = f.Name
		case "type":
			out[key] = f.TypeName
		case "kind":
			out[key] = string(f.Kind)
		case "required":
			out[key] = f.Required
		case "list":
			out[key] = f.List
		case "source":
			out[key] = f.Source
		case "description":
			out[key] = f.Description
		case "identifierFormat":
			if f.IdentifierFormat == nil {
				out[key] = nil
				continue
			}
			out[key] = e.resolveIdentifierFormatSelectionSet(f.IdentifierFormat, field.SelectionSet, childPath)
		case "capability":
			out[key] = e.resolveOntologyFieldCapabilitySelectionSet(f, field.SelectionSet, childPath)
		default:
			e.addError(childPath, fmt.Sprintf("unsupported ontology field field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveOntologyFieldCapabilitySelectionSet(f *ontology.Field, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	capability, ok := ontology.FieldQueryCapabilityForField(e.schema, f)
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		if !ok {
			switch field.Name {
			case "filterOps", "indexedFilterOps", "residualFilterOps":
				out[key] = []any{}
			case "sortable", "groupable":
				out[key] = false
			case "valueKind", "targetType", "enumName":
				out[key] = nil
			default:
				e.addError(append(path, key), fmt.Sprintf("unsupported ontology field capability field %q", field.Name))
			}
			continue
		}
		switch field.Name {
		case "valueKind":
			out[key] = capability.ValueKind
		case "filterOps":
			out[key] = stringsSliceAny(capability.FilterOps)
		case "indexedFilterOps":
			out[key] = stringsSliceAny(capability.IndexedFilterOps)
		case "residualFilterOps":
			out[key] = stringsSliceAny(capability.ResidualFilterOps)
		case "sortable":
			out[key] = capability.Sortable
		case "groupable":
			out[key] = capability.Groupable
		case "targetType":
			out[key] = nullableString(capability.TargetType)
		case "enumName":
			out[key] = nullableString(capability.EnumName)
		default:
			e.addError(append(path, key), fmt.Sprintf("unsupported ontology field capability field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveIdentifierFormatSelectionSet(format *ontology.IdentifierFormat, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		switch field.Name {
		case "strategy":
			out[key] = string(format.Strategy)
		case "prefix":
			out[key] = format.Prefix
		case "separator":
			out[key] = format.Separator
		case "pad":
			if format.Strategy == ontology.IdentifierStrategyDateTime {
				out[key] = nil
			} else {
				out[key] = format.Pad
			}
		default:
			e.addError(append(path, key), fmt.Sprintf("unsupported identifier format field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveCodeRuntimeSelectionSet(ctx context.Context, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		childPath := append(path, key)
		args := e.fieldArgs(field)
		req := CodeRuntimeRequest{Path: stringValue(args["path"]), First: boundedFirst(args["first"], 20, 100)}
		pack := unavailableCodePack(req.Path, "code_runtime_unavailable", "code runtime provider is unavailable")
		if e.deps.CodeRuntime != nil {
			var value CodeContextPack
			var err error
			switch field.Name {
			case "docsForCode":
				value, err = e.deps.CodeRuntime.DocsForCode(ctx, req)
			case "codeForNote":
				value, err = e.deps.CodeRuntime.CodeForNote(ctx, req)
			case "testsForCode":
				value, err = e.deps.CodeRuntime.TestsForCode(ctx, req)
			default:
				e.addError(childPath, fmt.Sprintf("unsupported code runtime field %q", field.Name))
				continue
			}
			if err != nil {
				e.addError(childPath, err.Error())
				if value.InputPath != "" || value.NormalizedPath != "" || len(value.Warnings) > 0 {
					pack = value
				}
			} else {
				pack = value
			}
		}
		out[key] = e.resolveCodePackSelectionSet(pack, field.SelectionSet, childPath)
	}
	return out
}

func (e *executor) resolveAuthoringGuideSelectionSet(guide OntologyAuthoringGuide, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		switch field.Name {
		case "type":
			out[key] = guide.Type
		case "markdown":
			out[key] = guide.Markdown
		case "available":
			out[key] = guide.Available
		case "warnings":
			out[key] = e.resolveWarnings(guide.Warnings, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("unsupported authoring guide field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveNextIDSelectionSet(next OntologyNextID, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		switch field.Name {
		case "type":
			out[key] = next.Type
		case "identifierField":
			out[key] = next.IdentifierField
		case "strategy":
			out[key] = string(next.Strategy)
		case "source":
			out[key] = next.Source
		case "prefix":
			out[key] = next.Prefix
		case "separator":
			out[key] = next.Separator
		case "pad":
			out[key] = nullableInt(next.Pad)
		case "next":
			out[key] = next.Next
		case "ids":
			out[key] = stringsSliceAny(next.IDs)
		case "count":
			out[key] = next.Count
		case "last":
			out[key] = next.Last
		case "currentMax":
			out[key] = nullableInt(next.CurrentMax)
		case "currentMaxValue":
			out[key] = next.CurrentMaxValue
		case "currentMaxOwner":
			out[key] = next.CurrentMaxOwner
		case "ownersScanned":
			out[key] = next.OwnersScanned
		case "sharedWith":
			out[key] = stringsSliceAny(next.SharedWith)
		case "ownersMatched":
			out[key] = stringsSliceAny(next.OwnersMatched)
		case "ownersSkipped":
			out[key] = stringsSliceAny(next.OwnersSkipped)
		case "notes":
			out[key] = stringsSliceAny(next.Notes)
		case "paths":
			out[key] = stringsSliceAny(next.Paths)
		case "allocations":
			out[key] = e.resolveIDAllocations(next.Allocations, field.SelectionSet, append(path, key))
		case "available":
			out[key] = next.Available
		case "errorCode":
			out[key] = next.ErrorCode
		case "error":
			out[key] = next.Error
		case "warnings":
			out[key] = e.resolveWarnings(next.Warnings, field.SelectionSet, append(path, key))
		default:
			e.addError(append(path, key), fmt.Sprintf("unsupported next id field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveIDAllocations(items []OntologyIDAllocation, set ast.SelectionSet, path []string) []any {
	out := make([]any, 0, len(items))
	for i, item := range items {
		row := map[string]any{}
		for _, field := range runtimeFields(set) {
			key := responseKey(field)
			switch field.Name {
			case "path":
				row[key] = item.Path
			case "id":
				row[key] = item.ID
			case "base":
				row[key] = item.Base
			case "disambiguator":
				row[key] = nullableInt(item.Disambiguator)
			default:
				e.addError(append(path, fmt.Sprintf("%d", i), key), fmt.Sprintf("unsupported id allocation field %q", field.Name))
			}
		}
		out = append(out, row)
	}
	return out
}

func (e *executor) resolveCodePackSelectionSet(pack CodeContextPack, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	for _, field := range runtimeFields(set) {
		key := responseKey(field)
		childPath := append(path, key)
		switch field.Name {
		case "inputPath":
			out[key] = pack.InputPath
		case "normalizedPath":
			out[key] = pack.NormalizedPath
		case "available":
			out[key] = pack.Available
		case "warnings":
			out[key] = e.resolveWarnings(pack.Warnings, field.SelectionSet, childPath)
		case "docs":
			out[key] = e.resolveRuntimePaths(pack.Docs, field.SelectionSet, childPath)
		case "notes":
			out[key] = e.resolveRuntimePaths(pack.Notes, field.SelectionSet, childPath)
		case "code":
			out[key] = e.resolveRuntimePaths(pack.Code, field.SelectionSet, childPath)
		case "tests":
			out[key] = e.resolveRuntimePaths(pack.Tests, field.SelectionSet, childPath)
		default:
			e.addError(childPath, fmt.Sprintf("unsupported code context field %q", field.Name))
		}
	}
	return out
}

func (e *executor) resolveRuntimePaths(items []RuntimePath, set ast.SelectionSet, path []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{}
		for _, field := range runtimeFields(set) {
			key := responseKey(field)
			switch field.Name {
			case "path":
				row[key] = item.Path
			case "title":
				row[key] = item.Title
			case "kind":
				row[key] = item.Kind
			case "reason":
				row[key] = item.Reason
			case "snippet":
				row[key] = item.Snippet
			case "content":
				row[key] = item.Content
			case "line":
				row[key] = item.Line
			case "truncated":
				row[key] = item.Truncated
			default:
				e.addError(append(path, key), fmt.Sprintf("unsupported runtime path field %q", field.Name))
			}
		}
		out = append(out, row)
	}
	return out
}

func (e *executor) resolveWarnings(items []RuntimeWarning, set ast.SelectionSet, path []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{}
		for _, field := range runtimeFields(set) {
			key := responseKey(field)
			switch field.Name {
			case "code":
				row[key] = item.Code
			case "message":
				row[key] = item.Message
			case "path":
				row[key] = item.Path
			default:
				e.addError(append(path, key), fmt.Sprintf("unsupported runtime warning field %q", field.Name))
			}
		}
		out = append(out, row)
	}
	return out
}

func (e *executor) unavailableAuthoringGuide(typeName, code, message string) OntologyAuthoringGuide {
	return OntologyAuthoringGuide{
		Type:      typeName,
		Available: false,
		Warnings:  []RuntimeWarning{{Code: code, Message: message}},
	}
}

func unavailableNextID(typeName, code, message string) OntologyNextID {
	return OntologyNextID{
		Type:      typeName,
		Available: false,
		ErrorCode: code,
		Error:     message,
		Warnings:  []RuntimeWarning{{Code: code, Message: message}},
	}
}

func unavailableCodePack(path, code, message string) CodeContextPack {
	return CodeContextPack{
		InputPath: path,
		Available: false,
		Warnings:  []RuntimeWarning{{Code: code, Message: message, Path: path}},
	}
}

func boundedFirst(v any, fallback, max int) int {
	first := intValue(v, fallback)
	if first <= 0 {
		return fallback
	}
	if first > max {
		return max
	}
	return first
}

func stringsSliceAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func runtimeStringList(value any) []string {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			out = append(out, stringValue(item))
		}
		return out
	default:
		return nil
	}
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func runtimeFields(set ast.SelectionSet) []*ast.Field {
	var out []*ast.Field
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			out = append(out, current)
		case *ast.InlineFragment:
			out = append(out, runtimeFields(current.SelectionSet)...)
		}
	}
	return out
}
