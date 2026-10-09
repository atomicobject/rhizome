package ontology

import (
	"fmt"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"regexp"
	"strings"
)

type schemaSourceInput struct {
	name  string
	input string
}

func ontologyPreludeInput(includeNoteInterface bool) string {
	prelude := `
scalar Date
scalar DateTime
scalar URL
enum FieldSource { FRONTMATTER INLINE CHECKBOX ITEM_TITLE ITEM_SUMMARY ITEM_DETAIL }
enum FieldAuthoringStyle { ANY FRONTMATTER INLINE_PROPERTY LIST_METADATA ITEM_TEXT CHECKBOX }
enum OntologyPropertyCase { KEBAB CAMEL SNAKE AS_DEFINED }
enum NeighborDirection { OUTBOUND INBOUND BOTH }
enum NeighborScope { NOTE SUBTREE }
enum SectionLevel { H1 H2 H3 H4 H5 H6 }
enum SectionDisplay { PANE INLINE }
enum EmbeddedSourceShape { SECTION LIST_ITEM CHECKBOX_ITEM }
enum NodeLocator { FILE EMBEDDED }
enum IdentifierPopulate { MANUAL ON_CREATE ON_LINK }
enum IdentifierStrategy { SEQUENTIAL DATETIME }
input RequiredFieldCondition { field: String!, equals: String }
directive @node(matches: [String!], paths: [String!], label: String, color: String, keyField: String, propertyCase: OntologyPropertyCase = KEBAB, locator: NodeLocator = FILE, default: Boolean = false) on OBJECT
directive @field(source: String, sources: [String!], sourceKind: FieldSource = FRONTMATTER) on FIELD_DEFINITION
directive @authoring(style: FieldAuthoringStyle!) on FIELD_DEFINITION
directive @format(pattern: String, notPattern: String) on FIELD_DEFINITION
directive @identifier(preferred: Boolean = false, strategy: IdentifierStrategy = SEQUENTIAL, prefix: String, pad: Int = 4, separator: String = "-", derivable: Boolean, derivedSuffix: String, populate: IdentifierPopulate) on FIELD_DEFINITION
directive @link(source: String, sources: [String!], sourceKind: FieldSource = FRONTMATTER, inverse: String, includeBodyLinks: Boolean = true, includeBacklinks: Boolean = true, contextInclude: Boolean = false) on FIELD_DEFINITION
directive @reverse(field: String!) on FIELD_DEFINITION
directive @workspaceMember(label: String) on FIELD_DEFINITION
directive @neighbors(direction: NeighborDirection!, type: String!, scope: NeighborScope = NOTE, contextInclude: Boolean = false) on FIELD_DEFINITION
directive @contains(level: SectionLevel, heading: String, shape: EmbeddedSourceShape = SECTION, marker: String, required: Boolean = false, display: SectionDisplay = PANE, min: Int, max: Int) on FIELD_DEFINITION
directive @source(shape: EmbeddedSourceShape!, marker: String, paths: [String!]) on OBJECT
directive @companionDocs(paths: [String!], purpose: String) on OBJECT | FIELD_DEFINITION
enum OntologySemanticsKind { BEHAVIORAL DOCUMENTARY }
directive @semantics(kind: OntologySemanticsKind!) on OBJECT | FIELD_DEFINITION
directive @guidance(meaning: String, authoring: String, agentImplications: String) on OBJECT | INTERFACE | FIELD_DEFINITION | ENUM | ENUM_VALUE
enum FieldDisplayRole { SUMMARY PARENT RANK }
enum FieldDisplayImportance { KEY NORMAL DETAIL }
directive @display(singular: String, plural: String, group: String, parent: String, role: FieldDisplayRole, importance: FieldDisplayImportance = NORMAL, hover: Boolean = true) on OBJECT | INTERFACE | FIELD_DEFINITION
directive @title(pattern: String, notPattern: String) on OBJECT
directive @requiresWhen(field: String!, equals: String!, require: [RequiredFieldCondition!]!) repeatable on OBJECT
directive @policy(requiresUserConfirmation: Boolean = false, forbidAutonomousSemanticEdits: Boolean = false, editScope: String, reason: String) on FIELD_DEFINITION | ENUM_VALUE
directive @view(label: String, order: Int, collapsed: Boolean, tone: String, stage: String) on ENUM_VALUE
directive @retrieval(intents: [String!], boost: Float, relationBoost: Float, propertyBoost: Float) on OBJECT | FIELD_DEFINITION
directive @traversal(intents: [String!], includeAmbient: Boolean, maxDepth: Int, minStructuralHits: Int) on OBJECT | FIELD_DEFINITION
directive @preview(template: String!, collapsed: Boolean = true) on OBJECT
interface Section {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
}
`
	if includeNoteInterface {
		prelude += "interface Note { id: ID }\n"
	}
	return prelude
}

type noteDeclarationScan struct {
	concrete          bool
	interfaceDefined  bool
	interfaceExtended bool
}

func detectAuthoredNoteDeclarations(name string, input string) (noteDeclarationScan, error) {
	doc, err := parser.ParseSchema(&ast.Source{Name: name, Input: input})
	if err != nil {
		return noteDeclarationScan{}, err
	}
	var out noteDeclarationScan
	for _, def := range doc.Definitions {
		if def == nil || def.Name != "Note" {
			continue
		}
		switch def.Kind {
		case ast.Object:
			out.concrete = true
		case ast.Interface:
			out.interfaceDefined = true
		}
	}
	for _, def := range doc.Extensions {
		if def == nil || def.Name != "Note" {
			continue
		}
		switch def.Kind {
		case ast.Object:
			out.concrete = true
		case ast.Interface:
			out.interfaceExtended = true
		}
	}
	return out, nil
}

func noteInterfaceImplementerExtensions(sources []schemaSourceInput) (string, error) {
	astSources := make([]*ast.Source, 0, len(sources))
	for _, source := range sources {
		astSources = append(astSources, &ast.Source{Name: source.name, Input: source.input})
	}
	doc, err := parser.ParseSchemas(astSources...)
	if err != nil {
		return "", err
	}
	noteFields := make([]*ast.FieldDefinition, 0)
	collectNoteFields := func(defs ast.DefinitionList) {
		for _, def := range defs {
			if def == nil || def.Name != "Note" || def.Kind != ast.Interface {
				continue
			}
			noteFields = append(noteFields, def.Fields...)
		}
	}
	collectNoteFields(doc.Definitions)
	collectNoteFields(doc.Extensions)
	if len(noteFields) == 0 {
		return "", nil
	}

	var b strings.Builder
	emitExtensions := func(defs ast.DefinitionList) {
		for _, def := range defs {
			if def == nil || def.Kind != ast.Object || !isFileBackedNoteObjectDefinition(def) || def.Name == "Note" {
				continue
			}
			missing := make([]*ast.FieldDefinition, 0, len(noteFields))
			for _, field := range noteFields {
				if field == nil || def.Fields.ForName(field.Name) != nil {
					continue
				}
				missing = append(missing, field)
			}
			if len(missing) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\nextend type %s {\n", def.Name)
			for _, field := range missing {
				fmt.Fprintf(&b, "  %s: %s%s\n", field.Name, field.Type.String(), renderDirectiveList(field.Directives))
			}
			b.WriteString("}\n")
		}
	}
	emitExtensions(doc.Definitions)
	emitExtensions(doc.Extensions)
	return b.String(), nil
}

func isFileBackedNoteObjectDefinition(def *ast.Definition) bool {
	if def == nil || def.Kind != ast.Object || containsString(def.Interfaces, "Section") {
		return false
	}
	dir := def.Directives.ForName("node")
	if dir == nil {
		return false
	}
	args := dir.ArgumentMap(nil)
	locator := strings.ToUpper(strings.TrimSpace(stringArg(args["locator"], "FILE")))
	return locator == "" || locator == "FILE"
}

func renderDirectiveList(dirs ast.DirectiveList) string {
	if len(dirs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == nil {
			continue
		}
		args := make([]string, 0, len(dir.Arguments))
		for _, arg := range dir.Arguments {
			if arg == nil || arg.Value == nil {
				continue
			}
			args = append(args, fmt.Sprintf("%s: %s", arg.Name, arg.Value.String()))
		}
		if len(args) == 0 {
			parts = append(parts, "@"+dir.Name)
			continue
		}
		parts = append(parts, fmt.Sprintf("@%s(%s)", dir.Name, strings.Join(args, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

var (
	sectionDeclarationStartPattern = regexp.MustCompile(`(?m)^[ \t]*(type|interface)\s+[A-Za-z_][A-Za-z0-9_]*\b`)
	sectionFieldNamePattern        = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)(?:\s*\([^)]*\))?\s*:`)
	implementsWordPattern          = regexp.MustCompile(`\bimplements\b`)
	sectionWordPattern             = regexp.MustCompile(`\bSection\b`)
)

func injectSectionPreludeFields(input string) string {
	if strings.TrimSpace(input) == "" {
		return input
	}
	var out strings.Builder
	last := 0
	for _, match := range sectionDeclarationStartPattern.FindAllStringIndex(input, -1) {
		start := match[0]
		braceIdx := findSectionBodyBrace(input, match[1])
		if braceIdx < 0 {
			continue
		}
		header := input[start:braceIdx]
		if !implementsSectionHeader(header) {
			continue
		}
		bodyStart := braceIdx + 1
		bodyEnd := findGraphQLBodyEnd(input, braceIdx)
		if bodyEnd < 0 {
			continue
		}
		missing := missingSectionPreludeFields(input[bodyStart:bodyEnd])
		if missing == "" {
			continue
		}
		out.WriteString(input[last:bodyStart])
		out.WriteString(missing)
		last = bodyStart
	}
	if last == 0 {
		return input
	}
	out.WriteString(input[last:])
	return out.String()
}

func implementsSectionHeader(header string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if !implementsWordPattern.MatchString(header) {
		return false
	}
	return sectionWordPattern.MatchString(header)
}

func findGraphQLBodyEnd(input string, open int) int {
	depth := 0
	i := open
	n := len(input)
	for i < n {
		c := input[i]
		switch c {
		case '"':
			i = skipGraphQLString(input, i)
			if i < 0 {
				return -1
			}
			continue
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// findSectionBodyBrace returns the index of the first `{` at or after `from`
// in `input` that is not inside a GraphQL string literal. Returns -1 if no
// such brace exists. Handles both `"..."` and `"""..."""` literals so
// directive arguments containing `{` (e.g. `@preview(template: "{{title}}")`)
// do not confuse the scan.
func findSectionBodyBrace(input string, from int) int {
	i := from
	n := len(input)
	parenDepth := 0
	bracketDepth := 0
	for i < n {
		c := input[i]
		switch c {
		case '"':
			i = skipGraphQLString(input, i)
			if i < 0 {
				return -1
			}
			continue
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			if parenDepth == 0 && bracketDepth == 0 {
				return i
			}
		default:
			i++
			continue
		}
		i++
	}
	return -1
}

// skipGraphQLString returns the first position after the quoted literal at start.
// GraphQL single-line strings reject raw newlines; block strings end at the next
// triple quote. Malformed block strings or raw newlines return -1.
func skipGraphQLString(input string, start int) int {
	if strings.HasPrefix(input[start:], `"""`) {
		end := strings.Index(input[start+3:], `"""`)
		if end < 0 {
			return -1
		}
		return start + 3 + end + 3
	}
	i := start + 1
	for i < len(input) {
		switch input[i] {
		case '\\':
			if i+1 < len(input) {
				i += 2
				continue
			}
		case '"':
			return i + 1
		case '\n':
			return -1
		}
		i++
	}
	return i
}

func missingSectionPreludeFields(body string) string {
	existing := make(map[string]struct{})
	for _, match := range sectionFieldNamePattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		existing[match[1]] = struct{}{}
	}
	lines := make([]string, 0, len(builtinSectionFields()))
	for _, field := range builtinSectionFields() {
		if field == nil {
			continue
		}
		if _, ok := existing[field.Name]; ok {
			continue
		}
		switch field.Name {
		case "children":
			lines = append(lines, "  children(first: Int = 20): [Section!]!")
		default:
			lines = append(lines, fmt.Sprintf("  %s: %s", field.Name, schemaSDLType(field.TypeName, field.List, field.Required)))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

func isSyntheticSectionBuiltinField(fieldDef *ast.FieldDefinition) bool {
	if fieldDef == nil {
		return false
	}
	if len(fieldDef.Directives) > 0 {
		return false
	}
	switch fieldDef.Name {
	case "id", "notePath", "title", "level", "content", "children":
		return true
	default:
		return false
	}
}

func schemaSDLType(typeName string, list bool, required bool) string {
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
