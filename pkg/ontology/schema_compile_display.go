package ontology

import (
	"fmt"
	"slices"
)

func applyInheritedFieldDisplay(schema *Schema) error {
	if schema == nil {
		return nil
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var applyInterface func(string) error
	applyInterface = func(name string) error {
		if done[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("ontology interface inheritance cycle involving %s", name)
		}
		iface := schema.Interfaces[name]
		if iface == nil {
			return nil
		}
		visiting[name] = true
		for _, parentName := range iface.Implements {
			if err := applyInterface(parentName); err != nil {
				return err
			}
			parent := schema.Interfaces[parentName]
			if parent != nil {
				inheritFieldDisplay(iface.ByName, parent.Fields)
			}
		}
		visiting[name] = false
		done[name] = true
		return validateDisplayFields(schema, iface.Name, iface.Fields)
	}
	for name := range schema.Interfaces {
		if err := applyInterface(name); err != nil {
			return err
		}
	}
	for _, noteType := range schema.Types {
		if noteType == nil {
			continue
		}
		for _, ifaceName := range noteType.Implements {
			if err := applyInterface(ifaceName); err != nil {
				return err
			}
			if iface := schema.Interfaces[ifaceName]; iface != nil {
				inheritFieldDisplay(noteType.ByName, iface.Fields)
			}
		}
		if err := validateDisplayFields(schema, noteType.Name, noteType.Fields); err != nil {
			return err
		}
	}
	return nil
}

func inheritFieldDisplay(byName map[string]*Field, inherited []*Field) {
	for _, parentField := range inherited {
		if parentField == nil {
			continue
		}
		field := byName[parentField.Name]
		if field == nil {
			continue
		}
		if !field.displayRoleSet && parentField.Display.Role != FieldDisplayRoleNone {
			field.Display.Role = parentField.Display.Role
			field.displayRoleOwner = parentField.displayRoleOwner
		}
		if !field.displayImportanceSet && parentField.Display.Importance != "" {
			field.Display.Importance = parentField.Display.Importance
		}
		if !field.displayHoverSet && parentField.Display.HideHover {
			field.Display.HideHover = true
		}
	}
}

func validateDisplayFields(schema *Schema, typeName string, fields []*Field) error {
	var summary, parent, rank *Field
	for _, field := range fields {
		if field == nil {
			continue
		}
		switch field.Display.Role {
		case FieldDisplayRoleSummary:
			if !IsSectionSummary(field) && (field.Kind != FieldKindScalar || field.List || (field.TypeName != "String" && field.TypeName != "ID" && field.TypeName != "URL")) {
				return compileError(field.position, "field %s.%s declares role SUMMARY but must be a singular String, ID, or URL scalar or a singular section @contains field", typeName, field.Name)
			}
			if summary != nil && summary.Name != field.Name {
				return duplicateDisplayRoleError(typeName, FieldDisplayRoleSummary, summary, field)
			}
			summary = field
		case FieldDisplayRoleParent:
			if err := validateParentRoleField(schema, typeName, field); err != nil {
				return err
			}
			if parent != nil && parent.Name != field.Name {
				return duplicateDisplayRoleError(typeName, FieldDisplayRoleParent, parent, field)
			}
			parent = field
		case FieldDisplayRoleRank:
			if field.Kind != FieldKindScalar || field.List || (field.TypeName != "Int" && field.TypeName != "Float") {
				return compileError(field.position, "field %s.%s declares role RANK but must be a singular Int or Float field", typeName, field.Name)
			}
			if rank != nil && rank.Name != field.Name {
				return duplicateDisplayRoleError(typeName, FieldDisplayRoleRank, rank, field)
			}
			rank = field
		}
	}
	return nil
}

// validateParentRoleField keeps PARENT on one single-valued @link whose target
// is the declaring type itself or an interface it implements. The same rule on
// an interface guarantees every implementor inherits a valid parent link. The
// hierarchy is a tree of note records, so every type the target admits must be
// a file-backed note type; that also excludes section and embedded declarers.
func validateParentRoleField(schema *Schema, typeName string, field *Field) error {
	if field.Kind != FieldKindLink || field.List {
		return compileError(field.position, "field %s.%s declares role PARENT but must be a single-valued @link field", typeName, field.Name)
	}
	// Every file-backed type implements Note, but a parent of any note is not a tree.
	if field.TypeName == "Note" || !typeMatchesOrImplements(schema, typeName, field.TypeName) {
		return compileError(field.position, "field %s.%s declares role PARENT but targets %s; it must target %s or an interface %s implements", typeName, field.Name, field.TypeName, typeName, typeName)
	}
	if noteType := schema.Types[typeName]; noteType != nil && effectiveTypeRole(noteType) != TypeRoleNote {
		return compileError(field.position, "field %s.%s declares role PARENT but %s is not a file-backed note type; parent hierarchies link note records", typeName, field.Name, typeName)
	}
	var notNote []string
	for _, target := range interfaceImplementersOrConcrete(schema, field.TypeName) {
		if effectiveTypeRole(target) != TypeRoleNote {
			notNote = append(notNote, target.Name)
		}
	}
	if len(notNote) > 0 {
		return compileError(field.position, "field %s.%s declares role PARENT but its target %s includes %s, which is not a file-backed note type; parent hierarchies link note records", typeName, field.Name, field.TypeName, slices.Min(notNote))
	}
	return nil
}

func duplicateDisplayRoleError(typeName string, role FieldDisplayRole, first, second *Field) error {
	firstOwner := firstNonEmptyString(first.displayRoleOwner, typeName)
	secondOwner := firstNonEmptyString(second.displayRoleOwner, typeName)
	return compileError(second.position, "type %s declares role %s on more than one field (%s.%s and %s.%s)", typeName, role, firstOwner, first.Name, secondOwner, second.Name)
}
