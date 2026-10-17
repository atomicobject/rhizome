package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/vektah/gqlparser/v2/ast"
)

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

func (e *executor) resolveWarnings(items []diag.Warning, set ast.SelectionSet, path []string) []any {
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
		Warnings:  []diag.Warning{{Code: code, Message: message}},
	}
}

func unavailableNextID(typeName, code, message string) OntologyNextID {
	return OntologyNextID{
		Type:      typeName,
		Available: false,
		ErrorCode: code,
		Error:     message,
		Warnings:  []diag.Warning{{Code: code, Message: message}},
	}
}

func unavailableCodePack(path, code, message string) CodeContextPack {
	return CodeContextPack{
		InputPath: path,
		Available: false,
		Warnings:  []diag.Warning{{Code: code, Message: message, Path: path}},
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
