package ontology

import (
	"github.com/vektah/gqlparser/v2/ast"
	"strings"
)

func compileField(parent *ast.Definition, parentRole TypeRole, propertyCase PropertyCase, fieldDef *ast.FieldDefinition, doc *ast.Schema, schema *Schema) (*Field, error) {
	if fieldDef.Type == nil {
		return nil, compileError(fieldDef.Position, "field %s.%s is missing a type", parent.Name, fieldDef.Name)
	}
	list, required, typeName := unwrapType(fieldDef.Type)
	if typeName == "" {
		return nil, compileError(fieldDef.Position, "field %s.%s has an invalid type", parent.Name, fieldDef.Name)
	}

	targetDef := doc.Types[typeName]
	if targetDef == nil {
		return nil, compileError(fieldDef.Position, "field %s.%s references unknown type %q", parent.Name, fieldDef.Name, typeName)
	}

	field := &Field{
		Name:             fieldDef.Name,
		Description:      strings.TrimSpace(fieldDef.Description),
		Guidance:         guidanceFromDescriptionAndDirective(fieldDef.Description, fieldDef.Directives.ForName("guidance")),
		TypeName:         typeName,
		List:             list,
		Required:         required,
		Source:           defaultPropertyName(fieldDef.Name, propertyCase),
		SourceKind:       FieldSourceFrontmatter,
		Scope:            NeighborScopeNote,
		IncludeBodyLinks: true,
		IncludeBacklinks: true,
		Semantics:        semanticsKind(fieldDef.Directives.ForName("semantics")),
		Annotations:      directiveAnnotations(fieldDef.Directives),
		position:         fieldDef.Position,
	}
	field.Policy = policyHintFromDirective(fieldDef.Directives.ForName("policy"))
	displayDir := fieldDef.Directives.ForName("display")
	if err := validateFieldDisplayLocation(parent, fieldDef, displayDir); err != nil {
		return nil, err
	}
	field.Display = fieldDisplayFromDirective(displayDir)
	if displayDir != nil {
		field.displayRoleOwner = parent.Name
		field.displayRoleSet = displayDir.Arguments.ForName("role") != nil
		field.displayImportanceSet = displayDir.Arguments.ForName("importance") != nil
		field.displayHoverSet = displayDir.Arguments.ForName("hover") != nil
	}
	companionDocs, err := companionDocsArg(fieldDef.Directives)
	if err != nil {
		return nil, compileError(fieldDef.Position, "invalid @%s directive: %v", companionDocsDirectiveName, err)
	}
	field.CompanionDocs = companionDocs

	if dir := fieldDef.Directives.ForName("contains"); dir != nil {
		if fieldDef.Directives.ForName("reverse") != nil {
			return nil, compileError(fieldDef.Position, "contains field %s.%s cannot declare @reverse", parent.Name, fieldDef.Name)
		}
		if displayDir != nil && (field.displayRoleSet || field.displayImportanceSet || field.displayHoverSet) && field.Display.Role != FieldDisplayRoleSummary {
			return nil, compileError(displayDir.Position, "contains field %s.%s cannot declare field-level @display arguments", parent.Name, fieldDef.Name)
		}
		if fieldDef.Directives.ForName("workspaceMember") != nil {
			return nil, compileError(fieldDef.Position, "contains field %s.%s cannot declare @workspaceMember", parent.Name, fieldDef.Name)
		}
		if parentRole == TypeRoleInterface {
			return nil, compileError(fieldDef.Position, "contains field %s.%s cannot be declared on an interface", parent.Name, fieldDef.Name)
		}
		if fieldDef.Directives.ForName("link") != nil || fieldDef.Directives.ForName("field") != nil || fieldDef.Directives.ForName("neighbors") != nil || fieldDef.Directives.ForName("authoring") != nil || fieldDef.Directives.ForName("format") != nil {
			return nil, compileError(fieldDef.Position, "contains field %s.%s cannot declare @link, @field, @neighbors, @authoring, or @format", parent.Name, fieldDef.Name)
		}
		if targetDef.Kind == ast.Interface && typeName != "Section" {
			return nil, compileError(fieldDef.Position, "contains field %s.%s cannot target interface %s; use a concrete section type or the built-in Section contract", parent.Name, fieldDef.Name, typeName)
		}
		if targetDef.Kind != ast.Object && typeName != "Section" {
			return nil, compileError(fieldDef.Position, "contains field %s.%s must target Section or a concrete section type", parent.Name, fieldDef.Name)
		}
		if !implementsSchemaSection(doc, typeName) {
			return nil, compileError(fieldDef.Position, "contains field %s.%s must target Section or a type that implements Section", parent.Name, fieldDef.Name)
		}
		args := dir.ArgumentMap(nil)
		shape := embeddedSourceShapeArg(args["shape"])
		level := sectionLevelArg(args["level"])
		heading := strings.TrimSpace(stringArg(args["heading"], ""))
		marker := strings.TrimSpace(stringArg(args["marker"], ""))
		switch shape {
		case EmbeddedSourceShapeSection:
			if level == "" {
				return nil, compileError(fieldDef.Position, "contains field %s.%s must declare a valid level", parent.Name, fieldDef.Name)
			}
			// Singular section fields must pin to a specific heading; list-typed fields may
			// either pin to a heading (match all sections with that heading at the given
			// level) or omit the heading (match every direct child at the given level).
			if !list && heading == "" {
				return nil, compileError(fieldDef.Position, "singular contains field %s.%s must declare a heading", parent.Name, fieldDef.Name)
			}
		case EmbeddedSourceShapeListItem, EmbeddedSourceShapeCheckboxItem:
			if !list {
				return nil, compileError(fieldDef.Position, "item-shaped contains field %s.%s must be a list", parent.Name, fieldDef.Name)
			}
			if marker == "" && parentRole == TypeRoleNote {
				return nil, compileError(fieldDef.Position, "item-shaped contains field %s.%s must declare marker unless scoped by a containing section", parent.Name, fieldDef.Name)
			}
			if level != "" || heading != "" {
				return nil, compileError(fieldDef.Position, "item-shaped contains field %s.%s cannot declare level or heading yet; scope item matching with marker", parent.Name, fieldDef.Name)
			}
		default:
			return nil, compileError(fieldDef.Position, "contains field %s.%s declares unsupported source shape %q", parent.Name, fieldDef.Name, shape)
		}
		field.Kind = FieldKindSection
		field.Source = ""
		field.SourceAliases = nil
		field.SourceKind = ""
		field.EmbeddedSourceShape = shape
		field.EmbeddedSourceMarker = marker
		field.SectionLevel = level
		field.SectionHeading = heading
		field.SectionRequired = boolArg(args["required"], false)
		field.SectionDisplay = sectionDisplayArg(args["display"])
		minValue, hasMin := optionalIntArg(args, "min")
		maxValue, hasMax := optionalIntArg(args, "max")
		if hasMin && minValue < 0 {
			return nil, compileError(fieldDef.Position, "contains field %s.%s min must be >= 0", parent.Name, fieldDef.Name)
		}
		if hasMax && maxValue < 0 {
			return nil, compileError(fieldDef.Position, "contains field %s.%s max must be >= 0", parent.Name, fieldDef.Name)
		}
		if hasMin {
			field.ContainsMin = minValue
		}
		if hasMax {
			field.ContainsMax = maxValue
			if hasMin && maxValue < minValue {
				return nil, compileError(fieldDef.Position, "contains field %s.%s max must be >= min", parent.Name, fieldDef.Name)
			}
		} else {
			field.ContainsMax = -1
		}
		field.Required = field.Required || field.SectionRequired
		return field, nil
	}

	if dir := fieldDef.Directives.ForName("reverse"); dir != nil {
		if parentRole != TypeRoleNote && parentRole != TypeRoleInterface {
			return nil, compileError(fieldDef.Position, "reverse field %s.%s must belong to a note type or interface", parent.Name, fieldDef.Name)
		}
		if !list || typeName == "Note" || (targetDef.Kind != ast.Object && targetDef.Kind != ast.Interface) {
			return nil, compileError(fieldDef.Position, "reverse field %s.%s must be a list of a concrete node type or node interface", parent.Name, fieldDef.Name)
		}
		if fieldDef.Directives.ForName("link") != nil || fieldDef.Directives.ForName("neighbors") != nil || fieldDef.Directives.ForName("field") != nil || fieldDef.Directives.ForName("contains") != nil || fieldDef.Directives.ForName("workspaceMember") != nil || fieldDef.Directives.ForName("authoring") != nil || fieldDef.Directives.ForName("format") != nil || fieldDef.Directives.ForName("identifier") != nil {
			return nil, compileError(fieldDef.Position, "reverse field %s.%s cannot declare an authored source or another relationship directive", parent.Name, fieldDef.Name)
		}
		field.ReverseField = strings.TrimSpace(stringArg(dir.ArgumentMap(nil)["field"], ""))
		if field.ReverseField == "" {
			return nil, compileError(fieldDef.Position, "reverse field %s.%s must name an authored @link field", parent.Name, fieldDef.Name)
		}
		field.Kind = FieldKindReverse
		field.Source = ""
		field.SourceKind = ""
		return field, nil
	}

	if dir := fieldDef.Directives.ForName("neighbors"); dir != nil {
		if fieldDef.Directives.ForName("workspaceMember") != nil {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s cannot declare @workspaceMember", parent.Name, fieldDef.Name)
		}
		if fieldDef.Directives.ForName("link") != nil || fieldDef.Directives.ForName("field") != nil || fieldDef.Directives.ForName("authoring") != nil || fieldDef.Directives.ForName("format") != nil {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s cannot declare @link, @field, @authoring, or @format", parent.Name, fieldDef.Name)
		}
		if targetDef.Kind != ast.Object && targetDef.Kind != ast.Interface {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s must target an ontology object or interface", parent.Name, fieldDef.Name)
		}
		if typeName == "Note" || implementsSchemaSection(doc, typeName) {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s must target note types, not Section", parent.Name, fieldDef.Name)
		}
		if !list {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s must be a list", parent.Name, fieldDef.Name)
		}
		args := dir.ArgumentMap(nil)
		targetType := stringArg(args["type"], "")
		if targetType == "" {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s must declare a target type", parent.Name, fieldDef.Name)
		}
		if targetType != typeName {
			return nil, compileError(fieldDef.Position, "neighbor field %s.%s target %q must match field type %q", parent.Name, fieldDef.Name, targetType, typeName)
		}
		field.Kind = FieldKindNeighbor
		field.Direction = neighborDirectionArg(args["direction"])
		field.Scope = neighborScopeArg(args["scope"])
		if parentRole == TypeRoleSection || parentRole == TypeRoleEmbeddedNode {
			field.Scope = NeighborScopeSubtree
		}
		field.ContextInclude = boolArg(args["contextInclude"], false)
		return field, nil
	}

	if targetDef.Kind == ast.Object || targetDef.Kind == ast.Interface {
		if parentRole == TypeRoleSection {
			return nil, compileError(fieldDef.Position, "section type field %s.%s must use @contains or @neighbors", parent.Name, fieldDef.Name)
		}
		if typeName != "Note" && implementsSchemaSection(doc, typeName) {
			return nil, compileError(fieldDef.Position, "link field %s.%s cannot target Section types", parent.Name, fieldDef.Name)
		}
		field.Kind = FieldKindLink
		link := fieldDef.Directives.ForName("link")
		if link == nil {
			return nil, compileError(fieldDef.Position, "link field %s.%s must declare @link", parent.Name, fieldDef.Name)
		}
		if fieldDef.Directives.ForName("field") != nil || fieldDef.Directives.ForName("neighbors") != nil {
			return nil, compileError(fieldDef.Position, "link field %s.%s cannot declare @field or @neighbors", parent.Name, fieldDef.Name)
		}
		if fieldDef.Directives.ForName("format") != nil {
			return nil, compileError(fieldDef.Position, "link field %s.%s cannot declare @format", parent.Name, fieldDef.Name)
		}
		args := link.ArgumentMap(nil)
		if parentRole == TypeRoleEmbeddedNode {
			if link.Arguments.ForName("source") != nil || link.Arguments.ForName("sources") != nil {
				return nil, compileError(fieldDef.Position, "embedded node link field %s.%s cannot override @link source; the inline key is derived from the field name", parent.Name, fieldDef.Name)
			}
			if link.Arguments.ForName("sourceKind") != nil {
				return nil, compileError(fieldDef.Position, "embedded node link field %s.%s cannot declare sourceKind; embedded-node link fields are always inline", parent.Name, fieldDef.Name)
			}
			field.Source = ""
			field.SourceAliases = nil
			field.SourceKind = FieldSourceInline
		} else {
			field.Source, field.SourceAliases, err = propertyNamesFromArgs(args["source"], args["sources"], defaultPropertyName(field.Name, propertyCase))
			if err != nil {
				return nil, compileError(fieldDef.Position, "invalid @link sources: %v", err)
			}
			field.SourceKind = fieldSourceArg(args["sourceKind"])
		}
		field.Inverse = strings.TrimSpace(stringArg(args["inverse"], ""))
		field.IncludeBodyLinks = boolArg(args["includeBodyLinks"], true)
		field.IncludeBacklinks = boolArg(args["includeBacklinks"], true)
		field.ContextInclude = boolArg(args["contextInclude"], false)
		if member := fieldDef.Directives.ForName("workspaceMember"); member != nil {
			if parent.Kind == ast.Interface {
				return nil, compileError(fieldDef.Position, "interface field %s.%s cannot declare @workspaceMember; declare it on each concrete workspace type", parent.Name, fieldDef.Name)
			}
			if parentRole == TypeRoleSection || parentRole == TypeRoleEmbeddedNode {
				return nil, compileError(fieldDef.Position, "embedded field %s.%s cannot declare @workspaceMember", parent.Name, fieldDef.Name)
			}
			field.WorkspaceMember = true
			field.WorkspaceMemberLabel = strings.TrimSpace(stringArg(member.ArgumentMap(nil)["label"], ""))
		}
		if err := applyAuthoringDirective(field, parent, parentRole, fieldDef); err != nil {
			return nil, err
		}
		return field, nil
	}

	if schema.EnumTypes[typeName] != nil {
		field.Kind = FieldKindEnum
	} else {
		field.Kind = FieldKindScalar
		if _, ok := schema.Scalars[typeName]; !ok {
			switch typeName {
			case "String", "Int", "Float", "Boolean", "ID":
			default:
				return nil, compileError(fieldDef.Position, "field %s.%s uses unsupported scalar type %q", parent.Name, fieldDef.Name, typeName)
			}
		}
	}
	if fieldDef.Directives.ForName("link") != nil || fieldDef.Directives.ForName("neighbors") != nil || fieldDef.Directives.ForName("workspaceMember") != nil {
		return nil, compileError(fieldDef.Position, "scalar field %s.%s cannot declare @link, @neighbors, or @workspaceMember", parent.Name, fieldDef.Name)
	}
	if parentRole == TypeRoleSection || parentRole == TypeRoleEmbeddedNode {
		// Section-role scalar/enum fields are authored as inline `Key:: Value` properties inside the
		// section body. Frontmatter has no meaning at the section level. The inline key is derived
		// lazily from the enclosing note type's propertyCase at resolve/assessment time, so we leave
		// Source empty at compile.
		dir := fieldDef.Directives.ForName("field")
		if dir == nil {
			return nil, compileError(fieldDef.Position, "section type field %s.%s must declare @field, @contains, or @neighbors", parent.Name, fieldDef.Name)
		}
		if dir.Arguments.ForName("source") != nil || dir.Arguments.ForName("sources") != nil {
			return nil, compileError(fieldDef.Position, "section type field %s.%s cannot override @field source; the inline key is derived from the field name", parent.Name, fieldDef.Name)
		}
		sourceKind := FieldSourceInline
		if dir.Arguments.ForName("sourceKind") != nil {
			sourceKind = fieldSourceArg(dir.ArgumentMap(nil)["sourceKind"])
			switch sourceKind {
			case FieldSourceCheckbox:
				if field.TypeName != "Boolean" || field.List {
					return nil, compileError(fieldDef.Position, "section type checkbox field %s.%s must be a singular Boolean", parent.Name, fieldDef.Name)
				}
			case FieldSourceItemTitle, FieldSourceItemSummary, FieldSourceItemDetail:
				if field.TypeName != "String" || field.List {
					return nil, compileError(fieldDef.Position, "section type item-source field %s.%s must be a singular String", parent.Name, fieldDef.Name)
				}
			case FieldSourceInline:
				// Explicitly restating the section default is allowed.
			default:
				return nil, compileError(fieldDef.Position, "section type field %s.%s cannot declare sourceKind %s", parent.Name, fieldDef.Name, sourceKind)
			}
		}
		field.Source = ""
		field.SourceAliases = nil
		field.SourceKind = sourceKind
	} else if dir := fieldDef.Directives.ForName("field"); dir != nil {
		args := dir.ArgumentMap(nil)
		field.Source, field.SourceAliases, err = propertyNamesFromArgs(args["source"], args["sources"], defaultPropertyName(field.Name, propertyCase))
		if err != nil {
			return nil, compileError(fieldDef.Position, "invalid @field sources: %v", err)
		}
		field.SourceKind = fieldSourceArg(args["sourceKind"])
	}

	if dir := fieldDef.Directives.ForName("identifier"); dir != nil {
		if err := applyIdentifierDirective(field, parent, fieldDef, parentRole, dir); err != nil {
			return nil, err
		}
	}
	if err := applyAuthoringDirective(field, parent, parentRole, fieldDef); err != nil {
		return nil, err
	}
	if err := applyFormatDirective(field, parent, fieldDef); err != nil {
		return nil, err
	}

	return field, nil
}

func validateFieldDisplayLocation(parent *ast.Definition, fieldDef *ast.FieldDefinition, dir *ast.Directive) error {
	if dir == nil {
		return nil
	}
	for _, name := range []string{"singular", "plural", "group", "parent"} {
		if dir.Arguments.ForName(name) != nil {
			return compileError(dir.Position, "field %s.%s cannot declare type-level @display argument %s", parent.Name, fieldDef.Name, name)
		}
	}
	return nil
}

func applyAuthoringDirective(field *Field, parent *ast.Definition, parentRole TypeRole, fieldDef *ast.FieldDefinition) error {
	dir := fieldDef.Directives.ForName("authoring")
	if dir == nil {
		return nil
	}
	args := dir.ArgumentMap(nil)
	style := fieldAuthoringStyleArg(args["style"])
	if style == "" {
		return compileError(dir.Position, "@authoring on %s.%s declares an unsupported style", parent.Name, fieldDef.Name)
	}
	if field.Kind == FieldKindNeighbor || field.Kind == FieldKindSection {
		return compileError(dir.Position, "@authoring on %s.%s is only valid for scalar, enum, or link fields", parent.Name, fieldDef.Name)
	}
	switch style {
	case FieldAuthoringStyleAny:
		// Explicit escape hatch for generated or mixed-source fields.
	case FieldAuthoringStyleFrontmatter:
		if parentRole != TypeRoleNote || field.SourceKind != FieldSourceFrontmatter {
			return compileError(dir.Position, "@authoring(style: FRONTMATTER) on %s.%s requires a file-backed note field with sourceKind FRONTMATTER", parent.Name, fieldDef.Name)
		}
	case FieldAuthoringStyleInlineProperty:
		if field.SourceKind != FieldSourceInline {
			return compileError(dir.Position, "@authoring(style: INLINE_PROPERTY) on %s.%s requires sourceKind INLINE", parent.Name, fieldDef.Name)
		}
	case FieldAuthoringStyleListMetadata:
		if parentRole != TypeRoleSection && parentRole != TypeRoleEmbeddedNode {
			return compileError(dir.Position, "@authoring(style: LIST_METADATA) on %s.%s requires a section or embedded-node field", parent.Name, fieldDef.Name)
		}
		if field.SourceKind != FieldSourceInline {
			return compileError(dir.Position, "@authoring(style: LIST_METADATA) on %s.%s requires sourceKind INLINE", parent.Name, fieldDef.Name)
		}
	case FieldAuthoringStyleItemText:
		if field.SourceKind != FieldSourceItemTitle && field.SourceKind != FieldSourceItemSummary && field.SourceKind != FieldSourceItemDetail {
			return compileError(dir.Position, "@authoring(style: ITEM_TEXT) on %s.%s requires an ITEM_* sourceKind", parent.Name, fieldDef.Name)
		}
	case FieldAuthoringStyleCheckbox:
		if field.SourceKind != FieldSourceCheckbox {
			return compileError(dir.Position, "@authoring(style: CHECKBOX) on %s.%s requires sourceKind CHECKBOX", parent.Name, fieldDef.Name)
		}
	}
	field.AuthoringStyle = style
	return nil
}

func applyFormatDirective(field *Field, parent *ast.Definition, fieldDef *ast.FieldDefinition) error {
	dir := fieldDef.Directives.ForName("format")
	if dir == nil {
		return nil
	}
	if field.Kind != FieldKindScalar && field.Kind != FieldKindEnum {
		return compileError(dir.Position, "@format on %s.%s is only valid for scalar or enum fields", parent.Name, fieldDef.Name)
	}
	args := dir.ArgumentMap(nil)
	format, err := fieldFormatConstraintFromArgs(args)
	if err != nil {
		return compileError(dir.Position, "invalid @format directive on %s.%s: %v", parent.Name, fieldDef.Name, err)
	}
	field.Format = format
	return nil
}
