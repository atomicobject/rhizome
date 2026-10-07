package query

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// BuildExecutableSchema projects the compiled ontology model into the
// read-only GraphQL SDL used by CLI, agent, and web query surfaces.
// Docs: [[ontology-graphql-query-contract]]
// defines the generated roots, built-ins, interfaces, descriptions, and
// conflict checks that must stay in sync with the ontology compiler.
func BuildExecutableSchema(schema *ontology.Schema) (*ExecutableSchema, error) {
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
	return &ExecutableSchema{SDL: sdl, Schema: parsed, RootTypes: rootTypes, RuntimeRoots: runtimeRoots}, nil
}

func buildSDL(schema *ontology.Schema) (string, map[string]string, map[string]RuntimeRootKind, error) {
	var b strings.Builder
	b.WriteString("scalar Date\n")
	b.WriteString("scalar DateTime\n")
	b.WriteString("scalar URL\n")
	b.WriteString("scalar JSON\n\n")
	b.WriteString("enum PropertySource { ANY FRONTMATTER INLINE }\n")
	b.WriteString("enum NeighborDirection { OUTBOUND INBOUND BOTH }\n\n")
	b.WriteString("enum SectionLevel { H1 H2 H3 H4 H5 H6 }\n\n")
	b.WriteString("enum NodeKind { NOTE SECTION EMBEDDED CODE_FILE CODE_SYMBOL MODULE }\n\n")
	b.WriteString("enum FieldDisplayImportance { KEY NORMAL DETAIL }\n\n")
	b.WriteString("enum TraversalDirection { OUTBOUND INBOUND BOTH }\n")
	b.WriteString("enum GraphProfile { ONTOLOGY_NATIVE NOTES_ONLY CODE_AWARE }\n\n")
	b.WriteString("enum OntologySemanticsKind { BEHAVIORAL DOCUMENTARY }\n")
	b.WriteString("directive @semantics(kind: OntologySemanticsKind!) on OBJECT | FIELD_DEFINITION\n\n")
	b.WriteString("type NodeRef {\n")
	b.WriteString("  ref: String!\n")
	b.WriteString("  notePath: String\n")
	b.WriteString("  path: String\n")
	b.WriteString("  fragment: String\n")
	b.WriteString("  kind: NodeKind!\n")
	b.WriteString("  nodeId: String\n")
	b.WriteString("  structural: String\n")
	b.WriteString("  typeName: String\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeRefResolution {\n")
	b.WriteString("  input: String!\n")
	b.WriteString("  found: Boolean!\n")
	b.WriteString("  ref: NodeRef\n")
	b.WriteString("  candidates: [NodeRef!]!\n")
	b.WriteString("  kind: NodeKind\n")
	b.WriteString("  title: String\n")
	b.WriteString("  path: String\n")
	b.WriteString("  resolvedType: String\n")
	b.WriteString("  reason: String\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeLinkTarget {\n")
	b.WriteString("  ref: NodeRef!\n")
	b.WriteString("  markdown: String\n")
	b.WriteString("  wikilink: String\n")
	b.WriteString("  displayLabel: String\n")
	b.WriteString("  exists: Boolean!\n")
	b.WriteString("  requiresFix: Boolean!\n")
	b.WriteString("  blockId: String\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeLocatorDiagnostic {\n")
	b.WriteString("  code: String!\n")
	b.WriteString("  notePath: String\n")
	b.WriteString("  ref: NodeRef\n")
	b.WriteString("  blockId: String\n")
	b.WriteString("  message: String!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeLinkFixAction {\n")
	b.WriteString("  ref: NodeRef!\n")
	b.WriteString("  blockId: String!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeLocator {\n")
	b.WriteString("  ref: NodeRef!\n")
	b.WriteString("  kind: NodeKind!\n")
	b.WriteString("  sourceLocator: String!\n")
	b.WriteString("  status: String!\n")
	b.WriteString("  linkTarget: NodeLinkTarget\n")
	b.WriteString("  diagnostics: [NodeLocatorDiagnostic!]!\n")
	b.WriteString("  fixActions: [NodeLinkFixAction!]!\n")
	b.WriteString("  wikilink: String\n")
	b.WriteString("  markdown: String\n")
	b.WriteString("  exists: Boolean!\n")
	b.WriteString("  requiresFix: Boolean!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeNeighborhood {\n")
	b.WriteString("  edges: [NodeRelationEdge!]!\n")
	b.WriteString("  nodes: [Node!]!\n")
	b.WriteString("  truncated: Boolean!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeRelationEdge {\n")
	b.WriteString("  source: NodeRef!\n")
	b.WriteString("  target: NodeRef!\n")
	b.WriteString("  direction: TraversalDirection!\n")
	b.WriteString("  relation: String\n")
	b.WriteString("  provenance: String\n")
	b.WriteString("  structural: Boolean!\n")
	b.WriteString("  targetType: String\n")
	b.WriteString("  depth: Int!\n")
	b.WriteString("}\n\n")
	b.WriteString("type LocalGraph {\n")
	b.WriteString("  nodes: [LocalGraphNode!]!\n")
	b.WriteString("  edges: [LocalGraphEdge!]!\n")
	b.WriteString("  truncated: Boolean!\n")
	b.WriteString("  diagnostics: JSON\n")
	b.WriteString("}\n\n")
	b.WriteString("type LocalGraphNode {\n")
	b.WriteString("  id: ID!\n")
	b.WriteString("  ref: NodeRef\n")
	b.WriteString("  nodeKind: String!\n")
	b.WriteString("  path: String\n")
	b.WriteString("  notePath: String\n")
	b.WriteString("  nodeId: String\n")
	b.WriteString("  typeName: String\n")
	b.WriteString("  title: String!\n")
	b.WriteString("  sourceLocator: String\n")
	b.WriteString("  parentId: ID\n")
	b.WriteString("}\n\n")
	b.WriteString("type LocalGraphEdge {\n")
	b.WriteString("  source: ID!\n")
	b.WriteString("  target: ID!\n")
	b.WriteString("  kind: String!\n")
	b.WriteString("  relation: String\n")
	b.WriteString("  relationLabel: String\n")
	b.WriteString("  provenance: String\n")
	b.WriteString("  structural: Boolean!\n")
	b.WriteString("  weight: Int\n")
	b.WriteString("  confidence: Float\n")
	b.WriteString("}\n\n")
	b.WriteString("type PageInfo {\n")
	b.WriteString("  queryShapeVersion: String!\n")
	b.WriteString("  requestedFirst: Int!\n")
	b.WriteString("  returnedCount: Int!\n")
	b.WriteString("  truncated: Boolean!\n")
	b.WriteString("  maxFirst: Int!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeBatchResult {\n")
	b.WriteString("  items: [NodeBatchItem!]!\n")
	b.WriteString("  pageInfo: PageInfo!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeBatchItem {\n")
	b.WriteString("  requestedRef: String!\n")
	b.WriteString("  ref: NodeRef\n")
	b.WriteString("  node: Node\n")
	b.WriteString("  error: RuntimeWarning\n")
	b.WriteString("}\n\n")
	b.WriteString("type NoteConnection {\n")
	b.WriteString("  nodes: [NoteNode!]!\n")
	b.WriteString("  pageInfo: PageInfo!\n")
	b.WriteString("  warnings: [RuntimeWarning!]!\n")
	b.WriteString("}\n\n")
	b.WriteString("type NodeSearchResult {\n")
	b.WriteString("  nodes: [NoteNode!]!\n")
	b.WriteString("  pageInfo: PageInfo!\n")
	b.WriteString("  query: [String!]!\n")
	b.WriteString("  warnings: [RuntimeWarning!]!\n")
	b.WriteString("}\n\n")
	b.WriteString("type ValidationState {\n")
	b.WriteString("  status: String!\n")
	b.WriteString("  generation: Int!\n")
	b.WriteString("  publishedGeneration: Int!\n")
	b.WriteString("  completion: String\n")
	b.WriteString("  staleReason: String\n")
	b.WriteString("  error: String\n")
	b.WriteString("  ok: Boolean!\n")
	b.WriteString("  issueCount: Int!\n")
	b.WriteString("  errorCount: Int!\n")
	b.WriteString("  affectedFileCount: Int!\n")
	b.WriteString("  affectedNoteCount: Int!\n")
	b.WriteString("  repairActionCount: Int!\n")
	b.WriteString("  scope: ValidationScope!\n")
	b.WriteString("  selectedChecks: [String!]!\n")
	b.WriteString("  checks: [ValidationCheck!]!\n")
	b.WriteString("}\n\n")
	b.WriteString("type ValidationScope {\n")
	b.WriteString("  kind: String!\n")
	b.WriteString("  key: String\n")
	b.WriteString("}\n\n")
	b.WriteString("type ValidationCheck {\n")
	b.WriteString("  name: String!\n")
	b.WriteString("  ok: Boolean!\n")
	b.WriteString("  skipped: Boolean!\n")
	b.WriteString("  outcome: String!\n")
	b.WriteString("  issueCount: Int!\n")
	b.WriteString("  summary: String\n")
	b.WriteString("  error: String\n")
	b.WriteString("  issues(first: Int = 100): [ValidationIssue!]!\n")
	b.WriteString("}\n\n")
	b.WriteString("type ValidationIssue {\n")
	b.WriteString("  issueKey: String!\n")
	b.WriteString("  check: String!\n")
	b.WriteString("  code: String\n")
	b.WriteString("  path: String\n")
	b.WriteString("  field: String\n")
	b.WriteString("  source: String\n")
	b.WriteString("  target: String\n")
	b.WriteString("  message: String\n")
	b.WriteString("  line: Int\n")
	b.WriteString("  data: JSON\n")
	b.WriteString("  affectedPaths: [String!]!\n")
	b.WriteString("  affectedNotePaths: [String!]!\n")
	b.WriteString("  affectedNodeIds: [String!]!\n")
	b.WriteString("  affectedTypes: [String!]!\n")
	b.WriteString("  affectedInterfaces: [String!]!\n")
	b.WriteString("  actionIds: [String!]!\n")
	b.WriteString("}\n\n")
	b.WriteString(workspaceProjectionTypesSDL())

	enumNames := make([]string, 0, len(schema.EnumTypes))
	for name := range schema.EnumTypes {
		enumNames = append(enumNames, name)
	}
	sort.Strings(enumNames)
	for _, name := range enumNames {
		enumType := schema.EnumTypes[name]
		writeDescription(&b, enumType.Description, 0)
		b.WriteString("enum ")
		b.WriteString(name)
		b.WriteString(" {\n")
		for _, value := range enumType.Values {
			if value == nil {
				continue
			}
			writeDescription(&b, value.Description, 2)
			b.WriteString("  ")
			b.WriteString(value.Name)
			b.WriteByte('\n')
		}
		b.WriteString("}\n\n")
	}

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
			writeAuthoredFields(&b, iface.Fields, runtimeFieldNameSet(nodeFieldsSDL()))
		}
	} else {
		for _, line := range noteNodeFieldsSDL() {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	noteRecordFacts := noteInterfaceRecordFactSDL(schema)
	writeSDLLines(&b, noteRecordFacts)
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
			writeAuthoredFields(&b, iface.Fields, runtimeNames)
		}
	}
	writeSDLLines(&b, noteRecordFacts)
	b.WriteString("}\n\n")
	writeDescription(&b, "Synthetic heading-derived section node.", 0)
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
	writeDescription(&b, "Fallback concrete type for untyped synthetic sections.", 0)
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
			return "", nil, nil, fmt.Errorf("ontology type %q conflicts with built-in runtime GraphQL type", name)
		}
	}
	for name := range schema.Interfaces {
		if _, ok := reservedRuntimeTypes[name]; ok {
			if name == "Note" && schema.NoteInterfaceAuthored {
				continue
			}
			return "", nil, nil, fmt.Errorf("ontology interface %q conflicts with built-in runtime GraphQL type", name)
		}
	}
	for name := range schema.EnumTypes {
		if _, ok := reservedRuntimeTypes[name]; ok {
			return "", nil, nil, fmt.Errorf("ontology enum %q conflicts with built-in runtime GraphQL type", name)
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
		if noteType == nil || (noteType.Role != ontology.TypeRoleNote && noteType.Role != ontology.TypeRoleEmbeddedNode) {
			continue
		}
		rootName := queryRootFieldName(name)
		if err := validateRuntimeRootCollision(rootName, name, usedRoots); err != nil {
			return "", nil, nil, err
		}
		usedRoots[rootName] = name
		rootTypes[rootName] = name
		if noteType.Role == ontology.TypeRoleEmbeddedNode {
			b.WriteString(fmt.Sprintf("  %s(first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [%s!]!\n", rootName, name))
			continue
		}
		b.WriteString(fmt.Sprintf("  %s(path: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [%s!]!\n", rootName, name))
	}
	b.WriteString("}\n\n")

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
		writeDescription(&b, iface.Description, 0)
		b.WriteString("interface ")
		b.WriteString(iface.Name)
		implements := append([]string(nil), iface.Implements...)
		sectionDerived := ontology.TypeMatchesOrImplements(schema, iface.Name, "Section")
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
		writeAuthoredFields(&b, iface.Fields, runtimeNames)
		writeRelationCountFields(&b, iface.Fields, iface.ByName, nil)
		implementors := interfaceImplementors(schema, typeNames, iface.Name)
		writeSDLLines(&b, recordFactSDL(func(field string) bool {
			return interfaceRecordFactFree(schema, iface.Name, implementors, field)
		}))
		b.WriteString("}\n\n")
	}

	for _, name := range typeNames {
		noteType := schema.Types[name]
		writeDescription(&b, noteType.Description, 0)
		b.WriteString(fmt.Sprintf("type %s implements %s", noteType.Name, strings.Join(typeInterfaces(schema, noteType), " & ")))
		if noteType.Semantics != "" {
			b.WriteString(fmt.Sprintf(" @semantics(kind: %s)", noteType.Semantics))
		}
		b.WriteString(" {\n")
		runtimeNames := map[string]struct{}{}
		switch noteType.Role {
		case ontology.TypeRoleNote:
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
		case ontology.TypeRoleSection, ontology.TypeRoleEmbeddedNode:
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
		writeAuthoredFields(&b, noteType.Fields, runtimeNames)
		writeRelationCountFields(&b, noteType.Fields, noteType.ByName, runtimeNames)
		writeSDLLines(&b, recordFactSDL(func(field string) bool {
			return recordFactFree(schema, noteType, field)
		}))
		b.WriteString("}\n\n")
	}
	b.WriteString(runtimeTypesSDL())
	return strings.TrimSpace(b.String()) + "\n", rootTypes, runtimeRoots, nil
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

func workspaceProjectionTypesSDL() string {
	return strings.TrimSpace(`
"Composable source-preserving metadata used by node workspace clients."
type NodeWorkspaceProjection {
  "Schema-aware semantic parent of the focused node, skipping structural wrappers."
  parentRef: NodeRef
  "Display title of parentRef, read from the index."
  parentTitle: String
  fields: [NodeFieldState!]!
  collections: [NodeCollectionState!]!
  "Source-preserving structural nodes, capped at 200 per workspace selection."
  bodies(first: Int = 200): [NodeBodyProjection!]!
  "Source-ordered authored links and embeds, capped at 100 per workspace selection."
  sourceLinks(first: Int = 100): [NodeWorkspaceSourceLink!]!
  capabilities: NodeCapabilities!
  status: NodeStatus!
  version: String!
  "Full source revision captured by the same projection as the editable values."
  sourceRevision: NodeSourceRevision!
  assessment: NodeAssessment
  structure: [NodeStructure!]!
  relationGroups: [NodeRelationGroup!]!
  loaded: NodeLoadedDomains!
}

"Source-preserving structural nodes for reconstructing a workspace body graph."
type NodeBodyProjection {
  ref: NodeRef!
  title: String!
  resolvedType: String
  locator: String!
  parentRef: NodeRef
  markdown: String!
  level: String
  blockId: String
  fragment: String
  binding: NodeBodyBinding
  fields: [NodeFieldState!]!
  collections: [NodeCollectionState!]!
  blocks: [NodeBodyBlock!]!
}

"One authored internal link or embed in source order."
type NodeWorkspaceSourceLink {
  target: String!
  authoredTarget: String!
  text: String
  kind: String!
  anchor: String
  embed: Boolean!
  targetKind: String!
  resolved: Boolean!
  resolvedRef: NodeRef
  title: String
  preview: String
}

"Schema-derived placement and display metadata for one structural body node."
type NodeBodyBinding {
  typeName: String
  fieldName: String
  fieldPath: String
  fieldList: Boolean!
  sectionDisplay: String
  properties: JSON
  identifierField: String
  previewTemplate: String
  collapsed: Boolean!
}

"One source-ordered block in a structural node body."
type NodeBodyBlock {
  kind: String!
  range: NodeRange!
  markdown: String
  fieldName: String
  rawKey: String
  childRef: NodeRef
  childRefs: [NodeRef!]!
  sectionDisplay: String
}

type NodeRange {
  start: Int!
  end: Int!
}

type NodeFieldState {
  name: String!
  kind: String!
  sourceKind: String
  present: Boolean!
  status: NodeStatus!
  range: NodeRange!
  valueRanges: [NodeRange!]!
  values: [String!]!
  "One entry per value of a relation field, in values order, with its resolved target. Empty for other fields."
  links: [NodeFieldLink!]!
  sectionNodes: [NodeRef!]!
  capability: NodeFieldCapability!
  issues: [NodeAssessmentIssue!]!
}

"One authored relation-field value. Unresolved or ambiguous values keep ref and title null."
type NodeFieldLink {
  value: String!
  resolved: Boolean!
  ref: NodeRef
  title: String
}

"Authoritative schema and authoring metadata for one field on its owning node."
type NodeFieldCapability {
  ownerRef: NodeRef!
  ownerType: String!
  typeName: String!
  valueKind: String!
  list: Boolean!
  required: Boolean!
  enumValues: [String!]!
  "Display label and tone for each enum value, in enumValues order."
  enumOptions: [NodeFieldEnumOption!]!
  targetType: String
  sourceKind: String
  valueOrigin: String!
  identifier: Boolean!
  preferredIdentifier: Boolean!
  displayImportance: FieldDisplayImportance!
  writeOperation: String
  readOnlyReason: String
}

"One admitted enum value. label is the @view(label:) or the raw value; stage is its declared or inferred lifecycle stage."
type NodeFieldEnumOption {
  value: String!
  label: String!
  tone: String
  stage: String
}

type NodeSourceRevision {
  notePath: String!
  contentFingerprint: String!
  content: String!
}

type NodeCollectionState {
  name: String!
  kind: String!
  status: NodeStatus!
  range: NodeRange!
  items: [NodeCollectionItem!]!
  orderFingerprint: String
}

type NodeCollectionItem {
  ref: NodeRef!
  range: NodeRange!
}

type NodeCapabilities {
  canEdit: Boolean!
  canEditFields: Boolean!
  canEditCollections: Boolean!
  canNavigateChildren: Boolean!
  canSubscribe: Boolean!
}

type NodeStatus {
  dirty: Boolean!
  validation: NodeValidationStatus!
  freshness: NodeFreshnessStatus!
  session: NodeSessionStatus!
  hasWarnings: Boolean!
}

type NodeValidationStatus { issueCount: Int! }
type NodeFreshnessStatus { state: String }
type NodeSessionStatus { state: String }

type NodeAssessment {
  notePath: String!
  declaredType: String
  resolvedType: String
  candidateTypes: [String!]!
  issues: [NodeAssessmentIssue!]!
  fields: [NodeFieldAssessment!]!
  relations: [NodeRelationAssessment!]!
}

type NodeAssessmentIssue {
  code: String
  notePath: String
  typeName: String
  fieldName: String
  nodeRef: String
  nodeId: String
  line: Int
  message: String!
}

type NodeFieldAssessment {
  name: String!
  description: String
  kind: String!
  typeName: String!
  required: Boolean!
  list: Boolean!
  source: String
  sourceKind: String
  present: Boolean!
  values: [String!]!
  validValues: [String!]!
  issues: [NodeAssessmentIssue!]!
}

type NodeRelationAssessment {
  name: String!
  description: String
  kind: String!
  typeName: String!
  required: Boolean!
  list: Boolean!
  source: String
  sourceKind: String
  direction: String
  present: Boolean!
  values: [String!]!
  targets: [NodeRelationTarget!]!
  issues: [NodeAssessmentIssue!]!
}

type NodeRelationTarget {
  path: String!
  typeName: String
  provenance: String
  structural: Boolean!
}

type NodeStructure {
  ref: NodeRef!
  parentRef: NodeRef
  title: String!
  level: String
  content: String
}

type NodeRelationGroup {
  key: String!
  label: String!
  ownerTitle: String
  navigation: Boolean!
  items: [NodeRelationItem!]!
}

type NodeRelationItem {
  ref: NodeRef!
  title: String!
  targetTitle: String
  resolvedType: String
  relationName: String
  provenance: String
  direction: TraversalDirection
  structural: Boolean!
  current: Boolean!
}

type NodeLoadedDomains {
  rendered: Boolean!
  assessment: Boolean!
  structure: Boolean!
  relations: Boolean!
}
`) + "\n\n"
}

func nodeFieldsSDLForType(noteType *ontology.NoteType) []string {
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

func writeAuthoredFields(b *strings.Builder, fields []*ontology.Field, skip map[string]struct{}) {
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
func isRelationList(field *ontology.Field) bool {
	return field != nil && field.List && (field.Kind == ontology.FieldKindNeighbor || field.Kind == ontology.FieldKindReverse)
}

func writeRelationCountFields(b *strings.Builder, fields []*ontology.Field, authored map[string]*ontology.Field, skip map[string]struct{}) {
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

func sectionNodeFieldsSDL(noteType *ontology.NoteType) []string {
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

func fieldSDL(field *ontology.Field) string {
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

func interfaceImplementors(schema *ontology.Schema, typeNames []string, ifaceName string) []*ontology.NoteType {
	var out []*ontology.NoteType
	for _, name := range typeNames {
		if ontology.TypeMatchesOrImplements(schema, name, ifaceName) {
			out = append(out, schema.Types[name])
		}
	}
	return out
}

// noteInterfaceRecordFactSDL lists the record facts every file-backed note
// type carries, which the Note and NoteNode interfaces can then declare.
func noteInterfaceRecordFactSDL(schema *ontology.Schema) []string {
	var noteTypes []*ontology.NoteType
	for _, noteType := range schema.Types {
		if noteType != nil && noteType.Role == ontology.TypeRoleNote {
			noteTypes = append(noteTypes, noteType)
		}
	}
	return recordFactSDL(func(field string) bool {
		return interfaceRecordFactFree(schema, "Note", noteTypes, field)
	})
}

func typeInterfaces(schema *ontology.Schema, noteType *ontology.NoteType) []string {
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
	case ontology.TypeRoleNote:
		add("NoteNode")
		add("Node")
		add("Note")
	case ontology.TypeRoleSection, ontology.TypeRoleEmbeddedNode:
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
