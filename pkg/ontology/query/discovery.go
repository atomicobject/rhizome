package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/vektah/gqlparser/v2/ast"
)

// SchemaDiscovery preserves the full-schema response by default. Selected
// fragments include whole definitions and identify unexpanded output types.
type SchemaDiscovery struct {
	Schema          string                 `json:"schema"`
	Scope           string                 `json:"scope,omitempty"`
	Type            string                 `json:"type,omitempty"`
	SchemaHash      string                 `json:"schemaHash,omitempty"`
	ReferencedTypes []string               `json:"referencedTypes,omitempty"`
	Roots           []DiscoveredRoot       `json:"roots,omitempty"`
	Types           []string               `json:"types,omitempty"`
	IncludedTypes   []string               `json:"includedTypes,omitempty"`
	Limits          *SchemaDiscoveryLimits `json:"limits,omitempty"`
	Truncated       bool                   `json:"truncated,omitempty"`
	LimitReasons    []string               `json:"limitReasons,omitempty"`
}

const (
	SchemaDiscoveryMaxDepth        = 2
	SchemaDiscoveryDefaultMaxBytes = 32768
	SchemaDiscoveryMaxBytes        = 131072
	SchemaDiscoveryMaxTypes        = 32
)

type SchemaDiscoveryOptions struct {
	Type     string   `json:"type,omitempty"`
	Types    []string `json:"types,omitempty"`
	Depth    int      `json:"depth,omitempty"`
	MaxBytes int      `json:"maxBytes,omitempty"`
}

type SchemaDiscoveryLimits struct {
	Depth    int `json:"depth"`
	MaxBytes int `json:"maxBytes"`
	MaxTypes int `json:"maxTypes"`
}

func DecodeSchemaDiscoveryOptions(input map[string]any) (SchemaDiscoveryOptions, error) {
	var options SchemaDiscoveryOptions
	if value, present := input["type"]; present {
		if _, ok := value.(string); !ok {
			return options, fmt.Errorf("type must be a string")
		}
	}
	data, err := json.Marshal(input)
	if err == nil {
		err = json.Unmarshal(data, &options)
	}
	return options, err
}

type DiscoveredRoot struct {
	Name               string   `json:"name"`
	MaxFirst           int      `json:"maxFirst"`
	ExclusiveSelectors []string `json:"exclusiveSelectors,omitempty"`
}

func DiscoverSchema(schema *ontoschema.Schema, exec *ExecutableSchema, options SchemaDiscoveryOptions) (SchemaDiscovery, error) {
	if schema == nil || exec == nil || exec.Schema == nil {
		return SchemaDiscovery{}, fmt.Errorf("compiled ontology and executable schemas are required")
	}
	names, maxBytes, err := options.selection()
	if err != nil {
		return SchemaDiscovery{}, err
	}
	if len(names) == 0 {
		return SchemaDiscovery{Schema: exec.SDL}, nil
	}
	included := map[string]bool{}
	for _, name := range names {
		if exec.Schema.Types[name] == nil || !publicDiscoveryType(name) {
			return SchemaDiscovery{}, fmt.Errorf("public query type %q not found", name)
		}
		included[name] = true
	}
	fragment := discoverFragment(exec, included)
	if len(fragment.Schema) > maxBytes {
		return SchemaDiscovery{}, fmt.Errorf("selected types require %d schema bytes; maxBytes is %d: select fewer types or raise maxBytes (maximum %d)", len(fragment.Schema), maxBytes, SchemaDiscoveryMaxBytes)
	}
	reasons, blocked := map[string]bool{}, map[string]bool{}
	for depth := 0; depth < options.Depth; depth++ {
		changed := false
		// Capture this frontier before adding types so each pass is one hop.
		for _, name := range fragment.ReferencedTypes {
			if blocked[name] {
				continue
			}
			if len(included) == SchemaDiscoveryMaxTypes {
				reasons["maxTypes"], blocked[name] = true, true
				continue
			}
			included[name] = true
			expanded := discoverFragment(exec, included)
			if len(expanded.Schema) > maxBytes {
				delete(included, name)
				reasons["maxBytes"], blocked[name] = true, true
				continue
			}
			fragment = expanded
			changed = true
		}
		if !changed {
			break
		}
	}
	fragment.Scope = "type-fragment"
	fragment.Type = strings.TrimSpace(options.Type)
	if len(options.Types) > 0 {
		fragment.Types = names
	}
	fragment.SchemaHash = schema.Hash
	fragment.IncludedTypes = sortedDiscoveryNames(included)
	fragment.Limits = &SchemaDiscoveryLimits{Depth: options.Depth, MaxBytes: maxBytes, MaxTypes: SchemaDiscoveryMaxTypes}
	fragment.Truncated = len(reasons) > 0
	fragment.LimitReasons = sortedDiscoveryNames(reasons)
	return fragment, nil
}

func (options SchemaDiscoveryOptions) selection() ([]string, int, error) {
	if options.Depth < 0 || options.Depth > SchemaDiscoveryMaxDepth {
		return nil, 0, fmt.Errorf("depth must be between 0 and %d", SchemaDiscoveryMaxDepth)
	}
	if options.MaxBytes < 0 || options.MaxBytes > SchemaDiscoveryMaxBytes {
		return nil, 0, fmt.Errorf("maxBytes must be between 1 and %d, or 0 for the default", SchemaDiscoveryMaxBytes)
	}
	names := map[string]bool{}
	if name := strings.TrimSpace(options.Type); name != "" {
		if len(options.Types) > 0 {
			return nil, 0, fmt.Errorf("type and types cannot be combined")
		}
		names[name] = true
	}
	for _, raw := range options.Types {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, 0, fmt.Errorf("types must contain nonempty type names")
		}
		names[name] = true
	}
	if len(names) > SchemaDiscoveryMaxTypes {
		return nil, 0, fmt.Errorf("select at most %d types", SchemaDiscoveryMaxTypes)
	}
	if len(names) == 0 && (options.Depth != 0 || options.MaxBytes != 0) {
		return nil, 0, fmt.Errorf("type or types is required with depth or maxBytes")
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = SchemaDiscoveryDefaultMaxBytes
	}
	return sortedDiscoveryNames(names), maxBytes, nil
}

func publicDiscoveryType(name string) bool {
	return !strings.HasPrefix(name, "__") && ontoschema.OntologyTypeVisibility(name) != ontoschema.TypeVisibilityInternal
}

func sortedDiscoveryNames(set map[string]bool) []string {
	var names []string
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func discoverFragment(exec *ExecutableSchema, included map[string]bool) SchemaDiscovery {
	definitions := map[string]*ast.Definition{}
	for name := range included {
		definitions[name] = exec.Schema.Types[name]
	}
	references := map[string]bool{}
	var collect func(*ast.Definition)
	collect = func(def *ast.Definition) {
		add := func(name string) {
			if definitions[name] != nil || name == "" || !publicDiscoveryType(name) {
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
		if def.Kind == ast.Interface {
			for _, implementation := range exec.Schema.PossibleTypes[def.Name] {
				add(implementation.Name)
			}
		}
		for _, field := range def.Fields {
			add(field.Type.Name())
			for _, arg := range field.Arguments {
				add(arg.Type.Name())
			}
		}
	}
	for _, name := range sortedDiscoveryNames(included) {
		collect(definitions[name])
	}
	resp := SchemaDiscovery{}
	roots := &ast.Definition{Kind: ast.Object, Name: exec.Schema.Query.Name}
	for _, field := range exec.Schema.Query.Fields {
		if !included[exec.RootTypes[field.Name]] {
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
	if len(roots.Fields) > 0 && !included[roots.Name] {
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
	return resp
}
