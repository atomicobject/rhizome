package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2/ast"
)

// SchemaDiscovery preserves the full-schema response by default. A selected
// type is explicitly a fragment; referenced object types are expanded only by
// another selected request, while input/enum/scalar dependencies are included.
type SchemaDiscovery struct {
	Schema          string           `json:"schema"`
	Scope           string           `json:"scope,omitempty"`
	Type            string           `json:"type,omitempty"`
	SchemaHash      string           `json:"schemaHash,omitempty"`
	ReferencedTypes []string         `json:"referencedTypes,omitempty"`
	Roots           []DiscoveredRoot `json:"roots,omitempty"`
}

type DiscoveredRoot struct {
	Name               string   `json:"name"`
	MaxFirst           int      `json:"maxFirst"`
	ExclusiveSelectors []string `json:"exclusiveSelectors,omitempty"`
}

func DiscoverSchema(schema *ontology.Schema, exec *ExecutableSchema, typeName string) (SchemaDiscovery, error) {
	if schema == nil || exec == nil || exec.Schema == nil {
		return SchemaDiscovery{}, fmt.Errorf("compiled ontology and executable schemas are required")
	}
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return SchemaDiscovery{Schema: exec.SDL}, nil
	}
	selected := exec.Schema.Types[typeName]
	if selected == nil || strings.HasPrefix(typeName, "__") || ontology.OntologyTypeVisibility(typeName) == ontology.TypeVisibilityInternal {
		return SchemaDiscovery{}, fmt.Errorf("public query type %q not found", typeName)
	}
	definitions := map[string]*ast.Definition{typeName: selected}
	references := map[string]bool{}
	var collect func(*ast.Definition)
	collect = func(def *ast.Definition) {
		add := func(name string) {
			if definitions[name] != nil || name == "" {
				return
			}
			dependency := exec.Schema.Types[name]
			if dependency == nil || dependency.BuiltIn {
				return
			}
			if dependency.IsInputType() {
				definitions[name] = dependency
				collect(dependency)
			} else {
				references[name] = true
			}
		}
		for _, name := range def.Interfaces {
			add(name)
		}
		for _, name := range def.Types {
			add(name)
		}
		for _, field := range def.Fields {
			add(field.Type.Name())
			for _, arg := range field.Arguments {
				add(arg.Type.Name())
			}
		}
	}
	collect(selected)
	resp := SchemaDiscovery{Scope: "type-fragment", Type: typeName, SchemaHash: schema.Hash}
	roots := &ast.Definition{Kind: ast.Object, Name: exec.Schema.Query.Name}
	for _, field := range exec.Schema.Query.Fields {
		if exec.RootTypes[field.Name] != typeName {
			continue
		}
		roots.Fields = append(roots.Fields, field)
		root := DiscoveredRoot{Name: field.Name, MaxFirst: publicRootFirstMax}
		for _, name := range []string{"path", "find", "property", "semantic"} {
			if field.Arguments.ForName(name) != nil {
				root.ExclusiveSelectors = append(root.ExclusiveSelectors, name)
			}
		}
		resp.Roots = append(resp.Roots, root)
	}
	if len(roots.Fields) > 0 && roots.Name != typeName {
		definitions[roots.Name] = roots
		collect(roots)
	}
	var out strings.Builder
	out.WriteString("# Selected query-schema fragment. Expand referencedTypes explicitly; this is not a complete executable schema.\n")
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeDiscoveryDefinition(&out, definitions[name])
	}
	for name := range references {
		if definitions[name] == nil {
			resp.ReferencedTypes = append(resp.ReferencedTypes, name)
		}
	}
	sort.Strings(resp.ReferencedTypes)
	resp.Schema = out.String()
	return resp, nil
}
