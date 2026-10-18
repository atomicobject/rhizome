package query

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// BuildExecutableSchema projects the compiled ontology model into the
// read-only GraphQL SDL used by CLI, agent, and web query surfaces.
// Docs: [[ontology-graphql-query-contract]]
// defines the generated roots, built-ins, interfaces, descriptions, and
// conflict checks that must stay in sync with the ontology compiler.
func BuildExecutableSchema(schema *ontoschema.Schema) (*ExecutableSchema, error) {
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	sdl, rootTypes, runtimeRoots, err := buildSDL(schema)
	if err != nil {
		return nil, err
	}
	parsed, err := gqlparser.LoadSchema(&ast.Source{Name: "rhizome-ontology-query.graphql", Input: sdl})
	if err != nil {
		return nil, err
	}
	return &ExecutableSchema{SDL: sdl, Schema: parsed, RootTypes: rootTypes, RuntimeRoots: runtimeRoots, sourceSchema: schema, sourceHash: schema.Hash}, nil
}

func buildSDL(schema *ontoschema.Schema) (string, map[string]string, map[string]RuntimeRootKind, error) {
	var b strings.Builder
	writeBuiltinSDL(&b)
	writeEnumSDL(&b, schema)
	writeFilterSDL(&b)
	writeNodeInterfacesSDL(&b, schema)
	typeNames, rootTypes, runtimeRoots, err := writeQueryRootsSDL(&b, schema)
	if err != nil {
		return "", nil, nil, err
	}
	writeInterfacesSDL(&b, schema, typeNames)
	writeTypesSDL(&b, schema, typeNames)
	b.WriteString(runtimeTypesSDL())
	return strings.TrimSpace(b.String()) + "\n", rootTypes, runtimeRoots, nil
}

func writeEnumSDL(b *strings.Builder, schema *ontoschema.Schema) {
	enumNames := make([]string, 0, len(schema.EnumTypes))
	for name := range schema.EnumTypes {
		enumNames = append(enumNames, name)
	}
	sort.Strings(enumNames)
	for _, name := range enumNames {
		enumType := schema.EnumTypes[name]
		writeDescription(b, enumType.Description, 0)
		b.WriteString("enum ")
		b.WriteString(name)
		b.WriteString(" {\n")
		for _, value := range enumType.Values {
			if value == nil {
				continue
			}
			writeDescription(b, value.Description, 2)
			b.WriteString("  ")
			b.WriteString(value.Name)
			b.WriteByte('\n')
		}
		b.WriteString("}\n\n")
	}

}

func writeFilterSDL(b *strings.Builder) {
	b.WriteString("input PropertyFilterInput {\n")
	b.WriteString("  name: String!\n")
	b.WriteString("  value: String!\n")
	b.WriteString("  source: PropertySource = ANY\n")
	b.WriteString("}\n\n")
	b.WriteString("enum FieldFilterOperator {\n")
	b.WriteString("  eq\n")
	b.WriteString("  in\n")
	b.WriteString("  exists\n")
	b.WriteString("  gt\n")
	b.WriteString("  gte\n")
	b.WriteString("  lt\n")
	b.WriteString("  lte\n")
	b.WriteString("  contains\n")
	b.WriteString("  neq\n")
	b.WriteString("}\n\n")
	b.WriteString("enum SortDirection {\n")
	b.WriteString("  asc\n")
	b.WriteString("  desc\n")
	b.WriteString("}\n\n")
	b.WriteString("\"\"\"For @link fields, eq/in test uniquely resolved target membership and exists tests presence. contains is for scalar text fields.\"\"\"\n")
	b.WriteString("input FieldFilterInput {\n")
	b.WriteString("  field: String!\n")
	b.WriteString("  op: FieldFilterOperator = eq\n")
	b.WriteString("  value: String\n")
	b.WriteString("  values: [String!]\n")
	b.WriteString("}\n\n")
	b.WriteString("input SortInput {\n")
	b.WriteString("  field: String!\n")
	b.WriteString("  direction: SortDirection = asc\n")
	b.WriteString("}\n\n")
}

func writeQueryRootsSDL(b *strings.Builder, schema *ontoschema.Schema) ([]string, map[string]string, map[string]RuntimeRootKind, error) {
	b.WriteString("type Query {\n")
	b.WriteString("  node(ref: String!): Node\n")
	b.WriteString("  nodes(refs: [String!]!, first: Int = 50): NodeBatchResult!\n")
	b.WriteString("  note(path: String, ref: String): NoteNode\n")
	b.WriteString("  resolve(ref: String!): NodeRefResolution!\n")
	b.WriteString("  notes(type: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20): NoteConnection!\n")
	b.WriteString("  search(query: [String!]!, type: String, first: Int = 20): NodeSearchResult!\n")
	b.WriteString("  validation(check: String, scopeKind: String = \"global\", scopeKey: String, firstIssues: Int = 100): ValidationState!\n")
	for _, line := range runtimeRootFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}

	typeNames := make([]string, 0, len(schema.Types))
	for name := range schema.Types {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)
	reservedRuntimeTypes := runtimeTypeNames()
	for _, name := range typeNames {
		if _, ok := reservedRuntimeTypes[name]; ok {
			return nil, nil, nil, fmt.Errorf("ontology type %q conflicts with built-in runtime GraphQL type", name)
		}
	}
	for name := range schema.Interfaces {
		if _, ok := reservedRuntimeTypes[name]; ok {
			if name == "Note" && schema.NoteInterfaceAuthored {
				continue
			}
			return nil, nil, nil, fmt.Errorf("ontology interface %q conflicts with built-in runtime GraphQL type", name)
		}
	}
	for name := range schema.EnumTypes {
		if _, ok := reservedRuntimeTypes[name]; ok {
			return nil, nil, nil, fmt.Errorf("ontology enum %q conflicts with built-in runtime GraphQL type", name)
		}
	}
	rootTypes := make(map[string]string, len(typeNames))
	usedRoots := map[string]string{
		"note":  "built-in",
		"notes": "built-in",
	}
	runtimeRoots := builtinRuntimeRoots()
	for root := range runtimeRoots {
		usedRoots[root] = "built-in runtime root"
	}
	for _, name := range typeNames {
		noteType := schema.Types[name]
		if noteType == nil || (noteType.Role != ontoschema.TypeRoleNote && noteType.Role != ontoschema.TypeRoleEmbeddedNode) {
			continue
		}
		rootName := queryRootFieldName(name)
		if err := validateRuntimeRootCollision(rootName, name, usedRoots); err != nil {
			return nil, nil, nil, err
		}
		usedRoots[rootName] = name
		rootTypes[rootName] = name
		if noteType.Role == ontoschema.TypeRoleEmbeddedNode {
			b.WriteString(fmt.Sprintf("  %s(first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [%s!]!\n", rootName, name))
			continue
		}
		b.WriteString(fmt.Sprintf("  %s(path: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [%s!]!\n", rootName, name))
	}
	b.WriteString("}\n\n")

	return typeNames, rootTypes, runtimeRoots, nil
}

func queryRootFieldName(typeName string) string {
	if typeName == "" {
		return ""
	}
	return strings.ToLower(typeName[:1]) + typeName[1:]
}

func writeDescription(b *strings.Builder, desc string, indent int) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return
	}
	prefix := strings.Repeat(" ", indent)
	b.WriteString(prefix)
	b.WriteString("\"\"\"\n")
	for _, line := range strings.Split(desc, "\n") {
		b.WriteString(prefix)
		b.WriteString(strings.TrimRight(line, " \t"))
		b.WriteByte('\n')
	}
	b.WriteString(prefix)
	b.WriteString("\"\"\"\n")
}
