package ontology

import (
	"fmt"
	"github.com/vektah/gqlparser/v2/ast"
	"regexp"
	"strings"
)

func compileNoteType(def *ast.Definition, doc *ast.Schema, schema *Schema) (*NoteType, error) {
	if err := validateTypeDisplayLocation(def); err != nil {
		return nil, err
	}
	dir := def.Directives.ForName("node")
	role := TypeRoleNote
	implements := interfaceNames(def)
	sectionType := implementsSchemaSection(doc, def.Name)
	if dir == nil {
		if !sectionType {
			return nil, compileError(def.Position, "object type %q must declare @node or implement Section", def.Name)
		}
		role = TypeRoleSection
	}

	var (
		args      map[string]any
		paths     []string
		matches   []string
		matchers  []*NoteMatcher
		isDefault bool
		err       error
	)
	if dir != nil {
		args = dir.ArgumentMap(nil)
		isDefault = boolArg(args["default"], false)
		locator := strings.ToUpper(strings.TrimSpace(stringArg(args["locator"], "FILE")))
		switch locator {
		case "", "FILE":
			if sectionType {
				return nil, compileError(def.Position, "file-backed node type %q cannot implement Section; use @node(locator: EMBEDDED) for embedded nodes", def.Name)
			}
			role = TypeRoleNote
		case "EMBEDDED":
			if !sectionType {
				return nil, compileError(def.Position, "embedded node type %q must implement Section", def.Name)
			}
			role = TypeRoleEmbeddedNode
		default:
			return nil, compileError(def.Position, "invalid @node locator %q on %s; expected FILE or EMBEDDED", locator, def.Name)
		}
		paths, err = stringListArg(args["paths"])
		if err != nil {
			return nil, compileError(def.Position, "invalid @node paths: %v", err)
		}
		matches, err = stringListArg(args["matches"])
		if err != nil {
			return nil, compileError(def.Position, "invalid @node matches: %v", err)
		}
		matchers, err = compileNoteMatchers(paths, matches)
		if err != nil {
			return nil, compileError(def.Position, "invalid @node matcher: %v", err)
		}
		if isDefault && role != TypeRoleNote {
			return nil, compileError(def.Position, "@node(default: true) is only valid on file-backed note types")
		}
		if isDefault && (len(paths) > 0 || len(matches) > 0) {
			return nil, compileError(def.Position, "@node(default: true) on %q cannot declare paths or matches", def.Name)
		}
		if len(matchers) == 0 {
			if role == TypeRoleNote && !isDefault {
				return nil, compileError(def.Position, "@node must declare paths and/or matches for file-backed node %q", def.Name)
			}
		}
	}

	displaySingular, displayPlural, displayGroup, displayParent := displayPresentationFromDirective(def.Directives.ForName("display"))
	noteType := &NoteType{
		Name:          def.Name,
		Description:   strings.TrimSpace(def.Description),
		Guidance:      guidanceFromDescriptionAndDirective(def.Description, def.Directives.ForName("guidance")),
		Role:          role,
		Label:         firstNonEmptyString(displaySingular, stringArg(args["label"], def.Name)),
		LabelAuthored: firstNonEmptyString(displaySingular, stringArg(args["label"], "")) != "",
		PluralLabel:   displayPlural,
		DisplayGroup:  displayGroup,
		DisplayParent: displayParent,
		Color:         stringArg(args["color"], ""),
		KeyField:      stringArg(args["keyField"], ""),
		PropertyCase:  propertyCaseArg(args["propertyCase"], PropertyCaseKebab),
		Default:       isDefault,
		Paths:         paths,
		Matches:       matches,
		Matchers:      matchers,
		Implements:    append([]string(nil), implements...),
		Fields:        make([]*Field, 0, len(def.Fields)),
		ByName:        make(map[string]*Field, len(def.Fields)),
		Semantics:     semanticsKind(def.Directives.ForName("semantics")),
		Annotations:   directiveAnnotations(def.Directives),
	}
	if titleDir := def.Directives.ForName("title"); titleDir != nil {
		title, titleErr := titleConstraintFromDirective(titleDir)
		if titleErr != nil {
			return nil, compileError(titleDir.Position, "invalid @title directive on %s: %v", def.Name, titleErr)
		}
		noteType.Title = title
	}
	if sourceDir := def.Directives.ForName("source"); sourceDir != nil {
		sourceArgs := sourceDir.ArgumentMap(nil)
		shape := embeddedSourceShapeArg(sourceArgs["shape"])
		marker := strings.TrimSpace(stringArg(sourceArgs["marker"], ""))
		paths, err := stringListArg(sourceArgs["paths"])
		if err != nil {
			return nil, compileError(def.Position, "invalid @source paths: %v", err)
		}
		sourceMatchers, err := compileNoteMatchers(paths, nil)
		if err != nil {
			return nil, compileError(def.Position, "invalid @source matcher: %v", err)
		}
		if role != TypeRoleEmbeddedNode {
			return nil, compileError(def.Position, "@source on %s is only supported for embedded node types", def.Name)
		}
		switch shape {
		case EmbeddedSourceShapeListItem, EmbeddedSourceShapeCheckboxItem:
			if marker == "" {
				return nil, compileError(def.Position, "@source on item-shaped embedded node %s must declare marker", def.Name)
			}
		default:
			return nil, compileError(def.Position, "@source on %s declares unsupported source shape %q", def.Name, shape)
		}
		noteType.SourceShape = shape
		noteType.SourceMarker = marker
		noteType.SourcePaths = paths
		noteType.SourceMatchers = sourceMatchers
	}
	companionDocs, err := companionDocsArg(def.Directives)
	if err != nil {
		return nil, compileError(def.Position, "invalid @%s directive: %v", companionDocsDirectiveName, err)
	}
	noteType.CompanionDocs = companionDocs

	for _, fieldDef := range def.Fields {
		if (role == TypeRoleSection || role == TypeRoleEmbeddedNode) && isSyntheticSectionBuiltinField(fieldDef) {
			continue
		}
		field, compileErr := compileField(def, noteType.Role, noteType.PropertyCase, fieldDef, doc, schema)
		if compileErr != nil {
			return nil, compileErr
		}
		noteType.Fields = append(noteType.Fields, field)
		noteType.ByName[field.Name] = field
	}
	requiresWhen, requiresErr := conditionalRequirementsFromDirectives(def.Directives, noteType, schema)
	if requiresErr != nil {
		return nil, compileError(def.Position, "invalid @requiresWhen directive on %s: %v", def.Name, requiresErr)
	}
	noteType.RequiresWhen = requiresWhen

	// At most one field per note type may be marked @identifier(preferred: true).
	// The validator also enforces vault-wide uniqueness of the value.
	var preferredIdentifier string
	for _, f := range noteType.Fields {
		if !f.IsPreferredIdentifier {
			continue
		}
		if preferredIdentifier != "" {
			return nil, compileError(def.Position, "note type %q declares more than one @identifier(preferred: true) field (%s and %s)", def.Name, preferredIdentifier, f.Name)
		}
		preferredIdentifier = f.Name
	}

	if previewDir := def.Directives.ForName("preview"); previewDir != nil {
		preview, previewErr := compilePreview(def, noteType, previewDir)
		if previewErr != nil {
			return nil, previewErr
		}
		noteType.Preview = preview
	}

	return noteType, nil
}

// previewPlaceholderPattern matches `{{name}}` tokens in a preview template.
// Placeholder names are restricted to ASCII letters, digits, and underscores
// (the same character class valid GraphQL field names use), so wrapping the
// placeholder name in simple bounded content avoids surprising matches.
var previewPlaceholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// compilePreview validates an @preview directive on a section-role type. It
// resolves placeholders against the type's declared scalar/enum fields plus
// the built-in `{{title}}`, and rejects collisions where a user-declared
// `title` field would shadow the built-in.
func compilePreview(def *ast.Definition, noteType *NoteType, dir *ast.Directive) (*PreviewSpec, error) {
	if noteType.Role != TypeRoleSection && noteType.Role != TypeRoleEmbeddedNode {
		return nil, compileError(dir.Position, "@preview is only valid on section-role types (type %q is not a section)", def.Name)
	}
	args := dir.ArgumentMap(nil)
	template := strings.TrimSpace(stringArg(args["template"], ""))
	if template == "" {
		return nil, compileError(dir.Position, "@preview on %q requires a non-empty template", def.Name)
	}
	collapsed := true
	if raw, ok := args["collapsed"]; ok {
		if b, ok := raw.(bool); ok {
			collapsed = b
		}
	}
	// `{{title}}` is always resolvable from the enclosing section heading —
	// the Section interface guarantees it as a built-in, and
	// isSyntheticSectionBuiltinField prevents user redeclaration.
	allowed := make(map[string]struct{}, len(noteType.Fields)+1)
	allowed["title"] = struct{}{}
	for _, field := range noteType.Fields {
		if field == nil {
			continue
		}
		if field.Kind != FieldKindScalar && field.Kind != FieldKindEnum {
			continue
		}
		allowed[field.Name] = struct{}{}
	}
	matches := previewPlaceholderPattern.FindAllStringSubmatch(template, -1)
	placeholders := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := match[1]
		if _, ok := allowed[name]; !ok {
			return nil, compileError(dir.Position, "@preview on %q references unknown placeholder {{%s}}; must be {{title}} or a declared scalar/enum field", def.Name, name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		placeholders = append(placeholders, name)
	}
	return &PreviewSpec{
		Template:     template,
		Placeholders: placeholders,
		Collapsed:    collapsed,
	}, nil
}

func compileInterfaceType(def *ast.Definition, doc *ast.Schema, schema *Schema) (*InterfaceType, error) {
	if def.Directives.ForName("node") != nil {
		return nil, compileError(def.Position, "interface %q cannot declare @node", def.Name)
	}
	if err := validateTypeDisplayLocation(def); err != nil {
		return nil, err
	}
	displaySingular, displayPlural, displayGroup, displayParent := displayPresentationFromDirective(def.Directives.ForName("display"))
	iface := &InterfaceType{
		Name:          def.Name,
		Description:   strings.TrimSpace(def.Description),
		Guidance:      guidanceFromDescriptionAndDirective(def.Description, def.Directives.ForName("guidance")),
		Role:          TypeRoleInterface,
		Label:         displaySingular,
		PluralLabel:   displayPlural,
		DisplayGroup:  displayGroup,
		DisplayParent: displayParent,
		Implements:    append([]string(nil), interfaceNames(def)...),
		Fields:        make([]*Field, 0, len(def.Fields)),
		ByName:        make(map[string]*Field, len(def.Fields)),
		Semantics:     semanticsKind(def.Directives.ForName("semantics")),
		Annotations:   directiveAnnotations(def.Directives),
		CompanionDocs: nil,
	}
	companionDocs, err := companionDocsArg(def.Directives)
	if err != nil {
		return nil, compileError(def.Position, "invalid @%s directive: %v", companionDocsDirectiveName, err)
	}
	iface.CompanionDocs = companionDocs
	sectionType := implementsSchemaSection(doc, def.Name)
	for _, fieldDef := range def.Fields {
		if sectionType && isSyntheticSectionBuiltinField(fieldDef) {
			continue
		}
		field, compileErr := compileField(def, TypeRoleInterface, PropertyCaseKebab, fieldDef, doc, schema)
		if compileErr != nil {
			return nil, compileErr
		}
		if def.Name == "Note" && field.Required {
			return nil, compileError(fieldDef.Position, "interface Note field %s must be nullable; shared Note fields cannot use !", field.Name)
		}
		iface.Fields = append(iface.Fields, field)
		iface.ByName[field.Name] = field
	}
	return iface, nil
}

func validateTypeDisplayLocation(def *ast.Definition) error {
	dir := def.Directives.ForName("display")
	if dir == nil {
		return nil
	}
	for _, name := range []string{"role", "importance", "hover"} {
		if dir.Arguments.ForName(name) != nil {
			return compileError(dir.Position, "%s %s cannot declare field-level @display argument %s", strings.ToLower(string(def.Kind)), def.Name, name)
		}
	}
	return nil
}

func interfaceNames(def *ast.Definition) []string {
	if def == nil || len(def.Interfaces) == 0 {
		return nil
	}
	out := make([]string, 0, len(def.Interfaces))
	seen := make(map[string]struct{}, len(def.Interfaces))
	for _, name := range def.Interfaces {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// Section admission must use the complete SDL, since the compiled Schema is
// populated one type at a time. The visited set bounds invalid interface cycles.
func implementsSchemaSection(doc *ast.Schema, typeName string) bool {
	seen := make(map[string]struct{})
	var visit func(string) bool
	visit = func(name string) bool {
		name = strings.TrimSpace(name)
		if name == "Section" {
			return true
		}
		if doc == nil {
			return false
		}
		if _, ok := seen[name]; ok {
			return false
		}
		seen[name] = struct{}{}
		def := doc.Types[name]
		if def == nil {
			return false
		}
		for _, iface := range def.Interfaces {
			if visit(iface) {
				return true
			}
		}
		return false
	}
	return visit(typeName)
}

func injectNoteInterfaceFields(schema *Schema) {
	if schema == nil || !schema.NoteInterfaceAuthored {
		return
	}
	iface := schema.Interfaces["Note"]
	if iface == nil {
		return
	}
	for _, noteType := range schema.Types {
		if noteType == nil || noteType.Role != TypeRoleNote {
			continue
		}
		for _, ifaceField := range iface.Fields {
			if ifaceField == nil {
				continue
			}
			if _, ok := noteType.ByName[ifaceField.Name]; ok {
				continue
			}
			field := cloneField(ifaceField)
			noteType.Fields = append(noteType.Fields, field)
			noteType.ByName[field.Name] = field
		}
	}
}

func cloneField(field *Field) *Field {
	if field == nil {
		return nil
	}
	clone := *field
	clone.SourceAliases = append([]string(nil), field.SourceAliases...)
	clone.CompanionDocs = append([]CompanionDocRef(nil), field.CompanionDocs...)
	if field.IdentifierFormat != nil {
		format := *field.IdentifierFormat
		clone.IdentifierFormat = &format
	}
	if field.Format != nil {
		format := *field.Format
		clone.Format = &format
	}
	if field.Guidance != nil {
		guidance := *field.Guidance
		clone.Guidance = &guidance
	}
	if field.Policy != nil {
		policy := *field.Policy
		clone.Policy = &policy
	}
	if field.Annotations != nil {
		clone.Annotations = make(map[string]map[string]any, len(field.Annotations))
		for name, values := range field.Annotations {
			clonedValues := make(map[string]any, len(values))
			for key, value := range values {
				clonedValues[key] = value
			}
			clone.Annotations[name] = clonedValues
		}
	}
	return &clone
}

func validateImplements(schema *Schema, typeName string, role TypeRole, fields []*Field, implements []string) error {
	if containsString(implements, "Section") && role == TypeRoleNote {
		return fmt.Errorf("ontology type %s cannot implement Section", typeName)
	}
	for _, ifaceName := range implements {
		if ifaceName == "Section" {
			continue
		}
		iface := schema.Interfaces[ifaceName]
		if iface == nil {
			return fmt.Errorf("ontology type %s implements unknown interface %s", typeName, ifaceName)
		}
		for _, ifaceField := range iface.Fields {
			if err := validateImplementedField(schema, typeName, fields, ifaceField); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateImplementedField(schema *Schema, typeName string, fields []*Field, expected *Field) error {
	for _, field := range fields {
		if field.Name != expected.Name {
			continue
		}
		if field.TypeName != expected.TypeName && !typeMatchesOrImplements(schema, field.TypeName, expected.TypeName) {
			return fmt.Errorf("ontology type %s field %s must target %s to satisfy interface, got %s", typeName, field.Name, expected.TypeName, field.TypeName)
		}
		if field.List != expected.List {
			return fmt.Errorf("ontology type %s field %s list shape does not satisfy implemented interface", typeName, field.Name)
		}
		if expected.Required && !field.Required {
			return fmt.Errorf("ontology type %s field %s must be required to satisfy implemented interface", typeName, field.Name)
		}
		if field.Kind != expected.Kind {
			return fmt.Errorf("ontology type %s field %s kind %s does not satisfy interface kind %s", typeName, field.Name, field.Kind, expected.Kind)
		}
		return nil
	}
	return fmt.Errorf("ontology type %s is missing interface field %s", typeName, expected.Name)
}

func builtinSectionFields() []*Field {
	return []*Field{
		{Name: "id", Kind: FieldKindScalar, TypeName: "ID", Required: true},
		{Name: "notePath", Kind: FieldKindScalar, TypeName: "String", Required: true},
		{Name: "title", Kind: FieldKindScalar, TypeName: "String", Required: true},
		{Name: "level", Kind: FieldKindEnum, TypeName: "SectionLevel", Required: true},
		{Name: "content", Kind: FieldKindScalar, TypeName: "String", Required: true},
		{Name: "children", Kind: FieldKindLink, TypeName: "Section", List: true, Required: true},
	}
}
