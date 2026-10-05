package query

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/vektah/gqlparser/v2/validator"
)

// Prepare validates Rhizome's intentionally small query subset before
// execution.
// Docs: [[ontology-graphql-query-contract]] is the
// contract for rejected forms, root bounds, and semantic-use detection.
func Prepare(execSchema *ExecutableSchema, raw string) (*PreparedQuery, []Error) {
	return PrepareWithVariables(execSchema, raw, nil)
}

type PrepareOptions struct {
	Variables     map[string]any
	OperationName string
}

// PrepareWithVariables validates a read-only query plus its JSON-supplied
// variables before execution.
// Docs: [[ontology-graphql-query-contract#^spec-0042-variables]] requires
// variable coercion before root-bound validation so reusable recipes can bind
// inputs without rewriting query text.
func PrepareWithVariables(execSchema *ExecutableSchema, raw string, variables map[string]any) (*PreparedQuery, []Error) {
	return PrepareWithOptions(execSchema, raw, PrepareOptions{Variables: variables})
}

func PrepareWithOptions(execSchema *ExecutableSchema, raw string, opts PrepareOptions) (*PreparedQuery, []Error) {
	raw = strings.TrimSpace(raw)
	if execSchema == nil || execSchema.Schema == nil {
		return nil, []Error{{Message: "executable schema is required"}}
	}
	if raw == "" {
		return nil, []Error{{Message: "query is required"}}
	}

	doc, err := parser.ParseQuery(&ast.Source{Input: raw})
	if err != nil {
		gqlErr := &gqlerror.Error{}
		if errors.As(err, &gqlErr) {
			return nil, gqlErrors(gqlerror.List{gqlErr})
		}
		return nil, gqlErrors(gqlerror.List{gqlerror.Wrap(err)})
	}
	wrapStringSemanticVariables(doc)
	errs := validator.Validate(execSchema.Schema, doc)
	if len(errs) > 0 {
		return nil, gqlErrors(errs)
	}
	if strings.TrimSpace(opts.OperationName) == "" && len(doc.Operations) != 1 {
		return nil, []Error{{Message: "exactly one query operation is required"}}
	}
	op := doc.Operations.ForName(opts.OperationName)
	if op == nil {
		if strings.TrimSpace(opts.OperationName) != "" {
			return nil, []Error{{Message: fmt.Sprintf("operation %q not found", opts.OperationName)}}
		}
		return nil, []Error{{Message: "exactly one query operation is required"}}
	}
	if op.Operation != ast.Query {
		return nil, []Error{{Message: "only query operations are supported"}}
	}
	if len(op.Directives) > 0 {
		return nil, []Error{{Message: "query directives are not supported"}}
	}
	coercedVariables, err := validator.VariableValues(execSchema.Schema, op, opts.Variables)
	if err != nil {
		return nil, []Error{{Message: err.Error()}}
	}

	if isIntrospectionOperation(op, doc.Fragments) {
		if errs := validateIntrospectionRootOnly(op.SelectionSet, doc.Fragments); len(errs) > 0 {
			return nil, errs
		}
		return &PreparedQuery{
			Raw:           raw,
			Document:      doc,
			Operation:     op,
			Variables:     coercedVariables,
			Introspection: true,
			schema:        execSchema.Schema,
		}, nil
	}
	usesSemantic := false
	usesSearch := false
	if errs := validateSelectionSet(execSchema, op.SelectionSet, doc.Fragments, true, &usesSemantic, &usesSearch, coercedVariables); len(errs) > 0 {
		return nil, errs
	}

	return &PreparedQuery{
		Raw:          raw,
		Document:     doc,
		Operation:    op,
		Variables:    coercedVariables,
		UsesSemantic: usesSemantic,
		UsesSearch:   usesSearch,
		schema:       execSchema.Schema,
	}, nil
}

func validateSelectionSet(execSchema *ExecutableSchema, set ast.SelectionSet, fragments ast.FragmentDefinitionList, root bool, usesSemantic *bool, usesSearch *bool, variables map[string]any) []Error {
	var errs []Error
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if len(current.Directives) > 0 {
				errs = append(errs, Error{Message: fmt.Sprintf("directives are not supported on field %q", current.Name)})
				continue
			}
			if root {
				if current.Name != "node" && current.Name != "nodes" && current.Name != "note" && current.Name != "notes" && current.Name != "search" && current.Name != "validation" && current.Name != "resolve" && execSchema.RootTypes[current.Name] == "" && execSchema.RuntimeRoots[current.Name] == "" {
					errs = append(errs, Error{Message: fmt.Sprintf("unsupported root field %q", current.Name)})
					continue
				}
				switch current.Name {
				case "node":
					args := current.ArgumentMap(variables)
					if args["ref"] == nil {
						errs = append(errs, Error{Message: "node(...) requires ref"})
					}
				case "nodes":
					args := current.ArgumentMap(variables)
					if args["refs"] == nil {
						errs = append(errs, Error{Message: "nodes(...) requires refs"})
					}
					errs = append(errs, validateFirstLimit("nodes", args, nestedFieldFirstMax)...)
				case "resolve":
					args := current.ArgumentMap(variables)
					if strings.TrimSpace(stringArg(args["ref"])) == "" {
						errs = append(errs, Error{Message: "resolve(...) requires ref"})
					}
				case "note":
					args := current.ArgumentMap(variables)
					pathArg := strings.TrimSpace(stringArg(args["path"]))
					refArg := args["ref"]
					modeCount := 0
					if pathArg != "" {
						modeCount++
					}
					if refArg != nil {
						modeCount++
					}
					if modeCount == 0 {
						errs = append(errs, Error{Message: "note(...) requires path or ref"})
					}
					if modeCount > 1 {
						errs = append(errs, Error{Message: "note(...) accepts at most one of path or ref"})
					}
				case "notes":
					args := current.ArgumentMap(variables)
					errs = append(errs, validateNotesRootArgs("notes", args, usesSemantic)...)
					errs = append(errs, validateFirstLimit("notes", args, publicRootFirstMax)...)
				case "search":
					args := current.ArgumentMap(variables)
					if len(semanticArgList(args["query"])) == 0 {
						errs = append(errs, Error{Message: "search(...) requires query"})
					}
					*usesSearch = true
					errs = append(errs, validateFirstLimit("search", args, nestedFieldFirstMax)...)
				case "validation":
					break
				default:
					if execSchema.RuntimeRoots[current.Name] != "" {
						break
					}
					args := current.ArgumentMap(variables)
					errs = append(errs, validateFirstLimit(current.Name, args, publicRootFirstMax)...)
					errs = append(errs, validateTypedRootOffset(current.Name, args)...)
					if !fieldAcceptsAnyArg(current, "path", "find", "property", "semantic") {
						break
					}
					pathArg := strings.TrimSpace(stringArg(args["path"]))
					findArg := strings.TrimSpace(stringArg(args["find"]))
					semanticArgs := semanticArgList(args["semantic"])
					propertyArg := args["property"]
					modeCount := 0
					if pathArg != "" {
						modeCount++
					}
					if findArg != "" {
						modeCount++
					}
					if propertyArg != nil {
						modeCount++
					}
					if len(semanticArgs) > 0 {
						modeCount++
						*usesSemantic = true
					}
					if modeCount > 1 {
						errs = append(errs, Error{Message: fmt.Sprintf("%s(...) accepts at most one of path, find, property, or semantic", current.Name)})
					}
					if modeCount == 0 {
						if execSchema.RootTypes[current.Name] != "" {
							break
						}
						errs = append(errs, Error{Message: fmt.Sprintf("%s(...) requires at least one of path, find, property, or semantic", current.Name)})
					}
				}
			}
			if childErrs := validateSelectionSet(execSchema, current.SelectionSet, fragments, false, usesSemantic, usesSearch, variables); len(childErrs) > 0 {
				errs = append(errs, childErrs...)
			}
		case *ast.InlineFragment:
			if len(current.Directives) > 0 {
				errs = append(errs, Error{Message: "inline fragment directives are not supported"})
				continue
			}
			if childErrs := validateSelectionSet(execSchema, current.SelectionSet, fragments, root, usesSemantic, usesSearch, variables); len(childErrs) > 0 {
				errs = append(errs, childErrs...)
			}
		case *ast.FragmentSpread:
			if len(current.Directives) > 0 {
				errs = append(errs, Error{Message: "fragment spread directives are not supported"})
				continue
			}
			fragment := fragments.ForName(current.Name)
			if fragment == nil {
				errs = append(errs, Error{Message: fmt.Sprintf("fragment %q not found", current.Name)})
				continue
			}
			if childErrs := validateSelectionSet(execSchema, fragment.SelectionSet, fragments, root, usesSemantic, usesSearch, variables); len(childErrs) > 0 {
				errs = append(errs, childErrs...)
			}
		default:
			errs = append(errs, Error{Message: "unsupported selection in query"})
		}
	}
	return errs
}

func fieldAcceptsAnyArg(field *ast.Field, names ...string) bool {
	if field == nil || field.Definition == nil {
		return false
	}
	for _, name := range names {
		if field.Definition.Arguments.ForName(name) != nil {
			return true
		}
	}
	return false
}

func validateNotesRootArgs(rootName string, args map[string]any, usesSemantic *bool) []Error {
	typeArg := strings.TrimSpace(stringArg(args["type"]))
	findArg := strings.TrimSpace(stringArg(args["find"]))
	semanticArgs := semanticArgList(args["semantic"])
	propertyArg := args["property"]
	modeCount := 0
	if findArg != "" {
		modeCount++
	}
	if propertyArg != nil {
		modeCount++
	}
	if len(semanticArgs) > 0 {
		modeCount++
		*usesSemantic = true
	}
	var errs []Error
	if modeCount > 1 {
		errs = append(errs, Error{Message: fmt.Sprintf("%s(...) accepts at most one of find, property, or semantic", rootName)})
	}
	if modeCount == 0 && typeArg == "" {
		errs = append(errs, Error{Message: fmt.Sprintf("%s(...) requires at least one of type, find, property, or semantic", rootName)})
	}
	return errs
}

func validateFirstLimit(rootName string, args map[string]any, max int) []Error {
	raw, ok := args["first"]
	if !ok || raw == nil {
		return nil
	}
	value, ok := intArg(raw)
	if !ok {
		return nil
	}
	if value > max {
		return []Error{{Message: fmt.Sprintf("%s(...) first must be <= %d", rootName, max)}}
	}
	if value <= 0 {
		return []Error{{Message: fmt.Sprintf("%s(...) first must be > 0", rootName)}}
	}
	return nil
}

func validateTypedRootOffset(rootName string, args map[string]any) []Error {
	raw, ok := args["offset"]
	if !ok || raw == nil {
		return nil
	}
	value, ok := intArg(raw)
	if !ok || value < 0 {
		return []Error{{Message: fmt.Sprintf("%s(...) offset must be >= 0", rootName)}}
	}
	return nil
}

func intArg(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func isIntrospectionOperation(op *ast.OperationDefinition, fragments ast.FragmentDefinitionList) bool {
	if op == nil {
		return false
	}
	return selectionSetUsesIntrospection(op.SelectionSet, fragments)
}

func selectionSetUsesIntrospection(set ast.SelectionSet, fragments ast.FragmentDefinitionList) bool {
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if current.Name == "__schema" || current.Name == "__type" {
				return true
			}
		case *ast.InlineFragment:
			if selectionSetUsesIntrospection(current.SelectionSet, fragments) {
				return true
			}
		case *ast.FragmentSpread:
			if fragment := fragments.ForName(current.Name); fragment != nil && selectionSetUsesIntrospection(fragment.SelectionSet, fragments) {
				return true
			}
		}
	}
	return false
}

func validateIntrospectionRootOnly(set ast.SelectionSet, fragments ast.FragmentDefinitionList) []Error {
	var errs []Error
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if current.Name != "__schema" && current.Name != "__type" && current.Name != "__typename" {
				errs = append(errs, Error{Message: fmt.Sprintf("introspection queries cannot include root field %q", current.Name)})
			}
		case *ast.InlineFragment:
			errs = append(errs, validateIntrospectionRootOnly(current.SelectionSet, fragments)...)
		case *ast.FragmentSpread:
			fragment := fragments.ForName(current.Name)
			if fragment == nil {
				errs = append(errs, Error{Message: fmt.Sprintf("fragment %q not found", current.Name)})
				continue
			}
			errs = append(errs, validateIntrospectionRootOnly(fragment.SelectionSet, fragments)...)
		default:
			errs = append(errs, Error{Message: "unsupported selection in introspection query"})
		}
	}
	return errs
}

func gqlErrors(errs gqlerror.List) []Error {
	out := make([]Error, 0, len(errs))
	for _, err := range errs {
		out = append(out, Error{Message: err.Error()})
	}
	return out
}

func wrapStringSemanticVariables(doc *ast.QueryDocument) {
	if doc == nil {
		return
	}
	for _, op := range doc.Operations {
		if op == nil {
			continue
		}
		wrapStringSemanticVariablesInSelectionSet(op.SelectionSet, op.VariableDefinitions)
	}
}

func wrapStringSemanticVariablesInSelectionSet(set ast.SelectionSet, defs ast.VariableDefinitionList) {
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			for _, arg := range current.Arguments {
				if arg == nil || arg.Name != "semantic" {
					continue
				}
				if isStringVariableValue(arg.Value, defs) {
					arg.Value = &ast.Value{
						Kind:     ast.ListValue,
						Children: ast.ChildValueList{{Value: arg.Value}},
						Position: arg.Value.Position,
					}
				}
			}
			wrapStringSemanticVariablesInSelectionSet(current.SelectionSet, defs)
		case *ast.InlineFragment:
			wrapStringSemanticVariablesInSelectionSet(current.SelectionSet, defs)
		}
	}
}

func isStringVariableValue(value *ast.Value, defs ast.VariableDefinitionList) bool {
	if value == nil || value.Kind != ast.Variable {
		return false
	}
	def := defs.ForName(value.Raw)
	return def != nil && def.Type != nil && def.Type.NamedType == "String" && def.Type.Elem == nil && def.Type.NonNull
}

func stringArg(v any) string {
	s, _ := v.(string)
	return s
}

func semanticArgList(v any) []string {
	switch value := v.(type) {
	case nil:
		return nil
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(stringArg(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
