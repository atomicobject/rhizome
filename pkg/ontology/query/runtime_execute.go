package query

import (
	"context"
	"fmt"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
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
		Warnings:   []diag.Warning{{Code: code, Message: message}},
	}
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

func (e *executor) resolveOntologyTypeSelectionSet(nt *ontoschema.NoteType, set ast.SelectionSet, path []string) map[string]any {
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

func (e *executor) resolveOntologyFieldSelectionSet(f *ontoschema.Field, set ast.SelectionSet, path []string) map[string]any {
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

func (e *executor) resolveOntologyFieldCapabilitySelectionSet(f *ontoschema.Field, set ast.SelectionSet, path []string) map[string]any {
	out := map[string]any{}
	capability, ok := ontoschema.FieldQueryCapabilityForField(e.schema, f)
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

func (e *executor) resolveIdentifierFormatSelectionSet(format *ontoschema.IdentifierFormat, set ast.SelectionSet, path []string) map[string]any {
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
			if format.Strategy == ontoschema.IdentifierStrategyDateTime {
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
