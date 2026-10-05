package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// ExecuteIntrospection resolves GraphQL schema introspection from the generated
// executable schema. It deliberately stays schema-only: no vault index, note
// projection, semantic search, or runtime providers are touched.
func ExecuteIntrospection(execSchema *ExecutableSchema, prepared *PreparedQuery) Result {
	if execSchema == nil || execSchema.Schema == nil {
		return Result{Errors: []Error{{Message: "executable schema is required"}}}
	}
	if prepared == nil || prepared.Operation == nil {
		return Result{Errors: []Error{{Message: "prepared query is required"}}}
	}
	exec := introspectionExecutor{
		schema:    execSchema.Schema,
		variables: prepared.Variables,
		fragments: prepared.Document.Fragments,
	}
	data := exec.resolveSelectionSet(introspectionValue{kind: "Query"}, prepared.Operation.SelectionSet, nil)
	return Result{
		Data:        data,
		Errors:      exec.errors,
		orderedData: orderResponseValue(data, prepared.Operation.SelectionSet, prepared.Document.Fragments),
	}
}

type introspectionValue struct {
	kind      string
	schema    *ast.Schema
	def       *ast.Definition
	typ       *ast.Type
	field     *ast.FieldDefinition
	arg       *ast.ArgumentDefinition
	enumValue *ast.EnumValueDefinition
	directive *ast.DirectiveDefinition
}

type introspectionExecutor struct {
	schema    *ast.Schema
	variables map[string]any
	fragments ast.FragmentDefinitionList
	errors    []Error
}

func (e *introspectionExecutor) resolveSelectionSet(value introspectionValue, set ast.SelectionSet, path []string) map[string]any {
	out := make(map[string]any)
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			key := responseKey(current)
			out[key] = e.resolveField(value, current, append(path, key))
		case *ast.InlineFragment:
			if fragmentApplies(value.kind, current.TypeCondition) {
				for key, item := range e.resolveSelectionSet(value, current.SelectionSet, path) {
					out[key] = item
				}
			}
		case *ast.FragmentSpread:
			fragment := e.fragments.ForName(current.Name)
			if fragment == nil {
				e.addError(path, fmt.Sprintf("fragment %q not found", current.Name))
				continue
			}
			if fragmentApplies(value.kind, fragment.TypeCondition) {
				for key, item := range e.resolveSelectionSet(value, fragment.SelectionSet, path) {
					out[key] = item
				}
			}
		default:
			e.addError(path, "unsupported introspection selection")
		}
	}
	return out
}

func fragmentApplies(kind, condition string) bool {
	return condition == "" || condition == kind
}

func (e *introspectionExecutor) resolveField(value introspectionValue, field *ast.Field, path []string) any {
	if field.Name == "__typename" {
		return value.kind
	}
	switch value.kind {
	case "Query":
		return e.resolveQueryField(field, path)
	case "__Schema":
		return e.resolveSchemaField(field, path)
	case "__Type":
		return e.resolveTypeField(value, field, path)
	case "__Field":
		return e.resolveFieldField(value, field, path)
	case "__InputValue":
		return e.resolveInputValueField(value, field, path)
	case "__EnumValue":
		return e.resolveEnumValueField(value, field, path)
	case "__Directive":
		return e.resolveDirectiveField(value, field, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported introspection object %q", value.kind))
		return nil
	}
}

func (e *introspectionExecutor) resolveQueryField(field *ast.Field, path []string) any {
	switch field.Name {
	case "__schema":
		return e.resolveSelectionSet(introspectionValue{kind: "__Schema", schema: e.schema}, field.SelectionSet, path)
	case "__type":
		name := stringValue(field.ArgumentMap(e.variables)["name"])
		if name == "" {
			return nil
		}
		def := e.schema.Types[name]
		if def == nil {
			return nil
		}
		return e.resolveSelectionSet(introspectionValue{kind: "__Type", def: def}, field.SelectionSet, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported introspection root field %q", field.Name))
		return nil
	}
}

func (e *introspectionExecutor) resolveSchemaField(field *ast.Field, path []string) any {
	switch field.Name {
	case "description":
		return nullableString(e.schema.Description)
	case "types":
		values := make([]introspectionValue, 0, len(e.schema.Types))
		for _, def := range e.schema.Types {
			values = append(values, introspectionValue{kind: "__Type", def: def})
		}
		sort.Slice(values, func(i, j int) bool { return values[i].def.Name < values[j].def.Name })
		return e.resolveList(values, field.SelectionSet, path)
	case "queryType":
		return e.resolveSelectionSet(introspectionValue{kind: "__Type", def: e.schema.Query}, field.SelectionSet, path)
	case "mutationType", "subscriptionType":
		return nil
	case "directives":
		values := make([]introspectionValue, 0, len(e.schema.Directives))
		for _, directive := range e.schema.Directives {
			values = append(values, introspectionValue{kind: "__Directive", directive: directive})
		}
		sort.Slice(values, func(i, j int) bool { return values[i].directive.Name < values[j].directive.Name })
		return e.resolveList(values, field.SelectionSet, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported __Schema field %q", field.Name))
		return nil
	}
}

func (e *introspectionExecutor) resolveTypeField(value introspectionValue, field *ast.Field, path []string) any {
	typ := value.typ
	def := value.def
	switch field.Name {
	case "kind":
		return introspectionTypeKind(typ, def)
	case "name":
		if typ != nil && (typ.NonNull || typ.Elem != nil) {
			return nil
		}
		if def == nil {
			return nil
		}
		return def.Name
	case "description":
		if def == nil {
			return nil
		}
		return nullableString(def.Description)
	case "specifiedByURL", "ofType", "isOneOf":
		if field.Name == "ofType" && typ != nil {
			if typ.NonNull {
				nullable := *typ
				nullable.NonNull = false
				return e.resolveSelectionSet(e.typeValue(&nullable), field.SelectionSet, path)
			}
			if typ.Elem != nil {
				return e.resolveSelectionSet(e.typeValue(typ.Elem), field.SelectionSet, path)
			}
		}
		if field.Name == "isOneOf" {
			return false
		}
		return nil
	case "fields":
		if def == nil || (def.Kind != ast.Object && def.Kind != ast.Interface) {
			return nil
		}
		values := make([]introspectionValue, 0, len(def.Fields))
		for _, item := range def.Fields {
			// GraphQL meta-fields are queried through the introspection protocol,
			// but are not ordinary fields in the schema's type listing.
			if strings.HasPrefix(item.Name, "__") {
				continue
			}
			values = append(values, introspectionValue{kind: "__Field", field: item})
		}
		return e.resolveList(values, field.SelectionSet, path)
	case "interfaces":
		if def == nil || (def.Kind != ast.Object && def.Kind != ast.Interface) {
			return nil
		}
		values := make([]introspectionValue, 0, len(def.Interfaces))
		for _, name := range def.Interfaces {
			if iface := e.schema.Types[name]; iface != nil {
				values = append(values, introspectionValue{kind: "__Type", def: iface})
			}
		}
		return e.resolveList(values, field.SelectionSet, path)
	case "possibleTypes":
		if def == nil || (def.Kind != ast.Interface && def.Kind != ast.Union) {
			return nil
		}
		values := make([]introspectionValue, 0)
		for _, item := range e.schema.GetPossibleTypes(def) {
			values = append(values, introspectionValue{kind: "__Type", def: item})
		}
		return e.resolveList(values, field.SelectionSet, path)
	case "enumValues":
		if def == nil || def.Kind != ast.Enum {
			return nil
		}
		values := make([]introspectionValue, 0, len(def.EnumValues))
		for _, item := range def.EnumValues {
			values = append(values, introspectionValue{kind: "__EnumValue", enumValue: item})
		}
		return e.resolveList(values, field.SelectionSet, path)
	case "inputFields":
		if def == nil || def.Kind != ast.InputObject {
			return nil
		}
		values := make([]introspectionValue, 0, len(def.Fields))
		for _, item := range def.Fields {
			values = append(values, introspectionValue{kind: "__InputValue", field: item})
		}
		return e.resolveList(values, field.SelectionSet, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported __Type field %q", field.Name))
		return nil
	}
}

func (e *introspectionExecutor) resolveFieldField(value introspectionValue, field *ast.Field, path []string) any {
	item := value.field
	if item == nil {
		return nil
	}
	switch field.Name {
	case "name":
		return item.Name
	case "description":
		return nullableString(item.Description)
	case "args":
		values := make([]introspectionValue, 0, len(item.Arguments))
		for _, arg := range item.Arguments {
			values = append(values, introspectionValue{kind: "__InputValue", arg: arg})
		}
		return e.resolveList(values, field.SelectionSet, path)
	case "type":
		return e.resolveSelectionSet(e.typeValue(item.Type), field.SelectionSet, path)
	case "isDeprecated":
		return item.Directives.ForName("deprecated") != nil
	case "deprecationReason":
		return deprecationReason(item.Directives)
	default:
		e.addError(path, fmt.Sprintf("unsupported __Field field %q", field.Name))
		return nil
	}
}

func (e *introspectionExecutor) resolveInputValueField(value introspectionValue, field *ast.Field, path []string) any {
	name, description, typ, defaultValue, directives := inputValueParts(value)
	switch field.Name {
	case "name":
		return name
	case "description":
		return nullableString(description)
	case "type":
		return e.resolveSelectionSet(e.typeValue(typ), field.SelectionSet, path)
	case "defaultValue":
		if defaultValue == nil {
			return nil
		}
		return defaultValue.String()
	case "isDeprecated":
		return directives.ForName("deprecated") != nil
	case "deprecationReason":
		return deprecationReason(directives)
	default:
		e.addError(path, fmt.Sprintf("unsupported __InputValue field %q", field.Name))
		return nil
	}
}

func inputValueParts(value introspectionValue) (string, string, *ast.Type, *ast.Value, ast.DirectiveList) {
	if value.arg != nil {
		return value.arg.Name, value.arg.Description, value.arg.Type, value.arg.DefaultValue, value.arg.Directives
	}
	if value.field != nil {
		return value.field.Name, value.field.Description, value.field.Type, value.field.DefaultValue, value.field.Directives
	}
	return "", "", nil, nil, nil
}

func (e *introspectionExecutor) resolveEnumValueField(value introspectionValue, field *ast.Field, path []string) any {
	item := value.enumValue
	if item == nil {
		return nil
	}
	switch field.Name {
	case "name":
		return item.Name
	case "description":
		return nullableString(item.Description)
	case "isDeprecated":
		return item.Directives.ForName("deprecated") != nil
	case "deprecationReason":
		return deprecationReason(item.Directives)
	default:
		e.addError(path, fmt.Sprintf("unsupported __EnumValue field %q", field.Name))
		return nil
	}
}

func (e *introspectionExecutor) resolveDirectiveField(value introspectionValue, field *ast.Field, path []string) any {
	item := value.directive
	if item == nil {
		return nil
	}
	switch field.Name {
	case "name":
		return item.Name
	case "description":
		return nullableString(item.Description)
	case "isRepeatable":
		return item.IsRepeatable
	case "locations":
		out := make([]any, 0, len(item.Locations))
		for _, location := range item.Locations {
			out = append(out, string(location))
		}
		return out
	case "args":
		values := make([]introspectionValue, 0, len(item.Arguments))
		for _, arg := range item.Arguments {
			values = append(values, introspectionValue{kind: "__InputValue", arg: arg})
		}
		return e.resolveList(values, field.SelectionSet, path)
	default:
		e.addError(path, fmt.Sprintf("unsupported __Directive field %q", field.Name))
		return nil
	}
}

func (e *introspectionExecutor) resolveList(values []introspectionValue, set ast.SelectionSet, path []string) []any {
	out := make([]any, 0, len(values))
	for i, value := range values {
		out = append(out, e.resolveSelectionSet(value, set, append(path, fmt.Sprintf("%d", i))))
	}
	return out
}

func (e *introspectionExecutor) typeValue(typ *ast.Type) introspectionValue {
	if typ == nil {
		return introspectionValue{kind: "__Type"}
	}
	return introspectionValue{kind: "__Type", typ: typ, def: e.schema.Types[typ.Name()]}
}

func introspectionTypeKind(typ *ast.Type, def *ast.Definition) string {
	if typ != nil {
		if typ.NonNull {
			return "NON_NULL"
		}
		if typ.Elem != nil {
			return "LIST"
		}
	}
	if def == nil {
		return ""
	}
	return string(def.Kind)
}

func deprecationReason(directives ast.DirectiveList) any {
	directive := directives.ForName("deprecated")
	if directive == nil {
		return nil
	}
	reason := stringValue(directive.ArgumentMap(nil)["reason"])
	if reason == "" {
		return nil
	}
	return reason
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (e *introspectionExecutor) addError(path []string, message string) {
	e.errors = append(e.errors, Error{Message: message, Path: path})
}
