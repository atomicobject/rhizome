package query

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"slices"
	"sort"
	"strings"
)

func writeNodeInterfacesSDL(b *strings.Builder, schema *ontoschema.Schema) {
	b.WriteString("interface Node {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
	b.WriteString("interface Note implements Node {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if schema.NoteInterfaceAuthored {
		if iface := schema.Interfaces["Note"]; iface != nil {
			writeAuthoredFields(b, iface.Fields, runtimeFieldNameSet(nodeFieldsSDL()))
		}
	} else {
		for _, line := range noteNodeFieldsSDL() {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	noteRecordFacts := noteInterfaceRecordFactSDL(schema)
	writeSDLLines(b, noteRecordFacts)
	b.WriteString("}\n\n")
	b.WriteString("interface NoteNode implements Node")
	b.WriteString(" & Note")
	b.WriteString(" {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range noteNodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if schema.NoteInterfaceAuthored {
		if iface := schema.Interfaces["Note"]; iface != nil {
			runtimeNames := runtimeFieldNameSet(append(nodeFieldsSDL(), noteNodeFieldsSDL()...))
			writeAuthoredFields(b, iface.Fields, runtimeNames)
		}
	}
	writeSDLLines(b, noteRecordFacts)
	b.WriteString("}\n\n")
	writeDescription(b, "Synthetic heading-derived section node.", 0)
	b.WriteString("interface Section implements Node {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range sectionNodeFieldsSDL(nil) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
	writeDescription(b, "Fallback concrete type for untyped synthetic sections.", 0)
	b.WriteString("type UntypedSection implements Node & Section {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range sectionNodeFieldsSDL(nil) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
	b.WriteString("type CodeFile implements Node {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("  language: String\n")
	b.WriteString("  symbols(first: Int = 50): [CodeSymbol!]!\n")
	b.WriteString("}\n\n")
	b.WriteString("type CodeSymbol implements Node {\n")
	for _, line := range nodeFieldsSDL() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("  language: String\n")
	b.WriteString("  symbol: String\n")
	b.WriteString("  fqn: String\n")
	b.WriteString("  signature: String\n")
	b.WriteString("  docComment: String\n")
	b.WriteString("}\n\n")
}

func writeInterfacesSDL(b *strings.Builder, schema *ontoschema.Schema, typeNames []string) {
	interfaceNames := make([]string, 0, len(schema.Interfaces))
	for name := range schema.Interfaces {
		interfaceNames = append(interfaceNames, name)
	}
	sort.Strings(interfaceNames)
	for _, name := range interfaceNames {
		if name == "Note" && schema.NoteInterfaceAuthored {
			continue
		}
		iface := schema.Interfaces[name]
		writeDescription(b, iface.Description, 0)
		b.WriteString("interface ")
		b.WriteString(iface.Name)
		implements := append([]string(nil), iface.Implements...)
		sectionDerived := ontoschema.TypeMatchesOrImplements(schema, iface.Name, "Section")
		if sectionDerived && !slices.Contains(implements, "Section") {
			implements = append(implements, "Section")
		}
		if sectionDerived && !slices.Contains(implements, "Node") {
			implements = append(implements, "Node")
		}
		if len(implements) > 0 {
			b.WriteString(" implements ")
			b.WriteString(strings.Join(implements, " & "))
		}
		if iface.Semantics != "" {
			b.WriteString(fmt.Sprintf(" @semantics(kind: %s)", iface.Semantics))
		}
		b.WriteString(" {\n")
		var runtimeNames map[string]struct{}
		if sectionDerived {
			builtins := append(nodeFieldsSDL(), sectionNodeFieldsSDL(nil)...)
			for _, line := range builtins {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteByte('\n')
			}
			runtimeNames = runtimeFieldNameSet(builtins)
		}
		writeAuthoredFields(b, iface.Fields, runtimeNames)
		writeRelationCountFields(b, iface.Fields, iface.ByName, nil)
		implementors := interfaceImplementors(schema, typeNames, iface.Name)
		writeSDLLines(b, recordFactSDL(func(field string) bool {
			return interfaceRecordFactFree(schema, iface.Name, implementors, field)
		}))
		b.WriteString("}\n\n")
	}

}

func writeTypesSDL(b *strings.Builder, schema *ontoschema.Schema, typeNames []string) {
	for _, name := range typeNames {
		noteType := schema.Types[name]
		writeDescription(b, noteType.Description, 0)
		b.WriteString(fmt.Sprintf("type %s implements %s", noteType.Name, strings.Join(typeInterfaces(noteType), " & ")))
		if noteType.Semantics != "" {
			b.WriteString(fmt.Sprintf(" @semantics(kind: %s)", noteType.Semantics))
		}
		b.WriteString(" {\n")
		runtimeNames := map[string]struct{}{}
		switch noteType.Role {
		case ontoschema.TypeRoleNote:
			for _, line := range nodeFieldsSDL() {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteByte('\n')
			}
			for _, line := range noteNodeFieldsSDL() {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteByte('\n')
			}
			runtimeNames = runtimeFieldNameSet(append(nodeFieldsSDL(), noteNodeFieldsSDL()...))
		case ontoschema.TypeRoleSection, ontoschema.TypeRoleEmbeddedNode:
			nodeFields := nodeFieldsSDLForType(noteType)
			for _, line := range nodeFields {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteByte('\n')
			}
			sectionFields := sectionNodeFieldsSDL(noteType)
			for _, line := range sectionFields {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteByte('\n')
			}
			runtimeNames = runtimeFieldNameSet(append(nodeFields, sectionFields...))
		}
		writeAuthoredFields(b, noteType.Fields, runtimeNames)
		writeRelationCountFields(b, noteType.Fields, noteType.ByName, runtimeNames)
		writeSDLLines(b, recordFactSDL(func(field string) bool {
			return recordFactFree(schema, noteType, field)
		}))
		b.WriteString("}\n\n")
	}
}

func noteNodeFieldsSDL() []string {
	return []string{
		"content: String!",
		"format: String!",
		"sourceRepresentation: String!",
		"evidenceRepresentation: String!",
		"sourceCapabilities: [String!]!",
		"frontmatter: JSON",
		"tags: [String!]!",
		"score: Float",
		"linked(type: String, first: Int = 20): [NoteNode!]!",
		"backlinked(type: String, first: Int = 20): [NoteNode!]!",
		"connected(type: String, first: Int = 20): [NoteNode!]!",
	}
}

func nodeFieldsSDL() []string {
	return []string{
		"ref: NodeRef!",
		"nodeId: String!",
		"nodeKind: String!",
		"path: String",
		"title: String!",
		"resolvedType: String",
		"locator: NodeLocator!",
		"workspace: NodeWorkspaceProjection",
		"neighborhood(direction: TraversalDirection = OUTBOUND, relation: String, type: String, first: Int = 20): NodeNeighborhood!",
		"localGraph(profile: GraphProfile = ONTOLOGY_NATIVE, nodeLimit: Int = 50, edgeLimit: Int = 100, includeCodeEdges: Boolean = false): LocalGraph!",
	}
}

func nodeFieldsSDLForType(noteType *ontoschema.NoteType) []string {
	fields := nodeFieldsSDL()
	if noteType == nil {
		return fields
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(strings.SplitN(field, ":", 2)[0])
		if noteType.ByName[name] != nil {
			continue
		}
		out = append(out, field)
	}
	return out
}

func runtimeFieldNameSet(fields []string) map[string]struct{} {
	names := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if name := runtimeFieldName(field); name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

func runtimeFieldName(field string) string {
	name := strings.TrimSpace(strings.SplitN(field, ":", 2)[0])
	name = strings.TrimSpace(strings.SplitN(name, "(", 2)[0])
	return name
}

func writeAuthoredFields(b *strings.Builder, fields []*ontoschema.Field, skip map[string]struct{}) {
	for _, field := range fields {
		if field == nil {
			continue
		}
		if _, ok := skip[field.Name]; ok {
			continue
		}
		writeDescription(b, field.Description, 2)
		b.WriteString("  ")
		b.WriteString(fieldSDL(field))
		b.WriteByte('\n')
	}
}

// isRelationList reports a list @neighbors or @reverse field, which takes a
// first argument and has a derived <name>Count.
func isRelationList(field *ontoschema.Field) bool {
	return field != nil && field.List && (field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse)
}

func writeRelationCountFields(b *strings.Builder, fields []*ontoschema.Field, authored map[string]*ontoschema.Field, skip map[string]struct{}) {
	for _, field := range fields {
		if !isRelationList(field) {
			continue
		}
		name := field.Name + "Count"
		if authored[name] != nil {
			continue
		}
		if _, exists := skip[name]; exists {
			continue
		}
		writeDescription(b, fmt.Sprintf("Count of the %s relation.", field.Name), 2)
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString(": Int!\n")
	}
}

func sectionNodeFieldsSDL(noteType *ontoschema.NoteType) []string {
	fields := []struct {
		name string
		sdl  string
	}{
		{name: "id", sdl: "id: ID!"},
		{name: "notePath", sdl: "notePath: String!"},
		{name: "level", sdl: "level: SectionLevel!"},
		{name: "content", sdl: "content: String!"},
		{name: "children", sdl: "children(first: Int = 20): [Section!]!"},
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if noteType != nil && noteType.ByName[field.name] != nil {
			continue
		}
		out = append(out, field.sdl)
	}
	return out
}

func fieldSDL(field *ontoschema.Field) string {
	name := field.Name
	if isRelationList(field) {
		// first caps the targets read per record; <name>Count counts them all.
		name += "(first: Int)"
	}
	out := fmt.Sprintf("%s: %s", name, gqlType(field.TypeName, field.List, field.Required))
	if field.Semantics != "" {
		out += fmt.Sprintf(" @semantics(kind: %s)", field.Semantics)
	}
	return out
}

func writeSDLLines(b *strings.Builder, lines []string) {
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func interfaceImplementors(schema *ontoschema.Schema, typeNames []string, ifaceName string) []*ontoschema.NoteType {
	var out []*ontoschema.NoteType
	for _, name := range typeNames {
		if ontoschema.TypeMatchesOrImplements(schema, name, ifaceName) {
			out = append(out, schema.Types[name])
		}
	}
	return out
}

// noteInterfaceRecordFactSDL lists the record facts every file-backed note
// type carries, which the Note and NoteNode interfaces can then declare.
func noteInterfaceRecordFactSDL(schema *ontoschema.Schema) []string {
	var noteTypes []*ontoschema.NoteType
	for _, noteType := range schema.Types {
		if noteType != nil && noteType.Role == ontoschema.TypeRoleNote {
			noteTypes = append(noteTypes, noteType)
		}
	}
	return recordFactSDL(func(field string) bool {
		return interfaceRecordFactFree(schema, "Note", noteTypes, field)
	})
}

func typeInterfaces(noteType *ontoschema.NoteType) []string {
	out := make([]string, 0, len(noteType.Implements)+1)
	seen := make(map[string]struct{}, len(noteType.Implements)+1)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	switch noteType.Role {
	case ontoschema.TypeRoleNote:
		add("NoteNode")
		add("Node")
		add("Note")
	case ontoschema.TypeRoleSection, ontoschema.TypeRoleEmbeddedNode:
		add("Section")
		add("Node")
	}
	for _, name := range noteType.Implements {
		add(name)
	}
	return out
}

func gqlType(typeName string, list bool, required bool) string {
	if !list {
		if required {
			return typeName + "!"
		}
		return typeName
	}
	out := "[" + typeName + "!]"
	if required {
		out += "!"
	}
	return out
}
