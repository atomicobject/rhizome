package schema

import (
	"github.com/vektah/gqlparser/v2/ast"
	"strings"
)

func isRecordDefinition(def *ast.Definition) bool {
	if def == nil {
		return false
	}
	dir := def.Directives.ForName("source")
	return dir != nil && embeddedSourceShapeArg(dir.ArgumentMap(nil)["shape"]) == EmbeddedSourceShapeRecord
}

func compileRecordSource(def *ast.Definition, dir *ast.Directive) (*RecordSourceBinding, error) {
	fail := func(code, message string) (*RecordSourceBinding, error) {
		return nil, compileError(dir.Position, code, def.Name, "", "%s", message)
	}
	args := dir.ArgumentMap(nil)
	for _, name := range []string{"select", "key", "title", "space", "logicalKind", "paths"} {
		if dir.Arguments.ForName(name) == nil {
			return fail("record_source_argument", "RECORD source requires explicit "+name)
		}
	}
	for _, name := range []string{"marker"} {
		if dir.Arguments.ForName(name) != nil {
			return fail("record_source_syntax", "RECORD source cannot declare "+name)
		}
	}

	paths, err := stringListArg(args["paths"])
	if err != nil || len(paths) == 0 {
		return fail("record_source_paths", "RECORD source requires nonempty authorized paths")
	}
	if args["select"] == nil {
		return fail("record_source_argument", "RECORD select must be a string, including explicit empty root selection")
	}
	selector, err := CompileRecordSelector(stringArg(args["select"], ""))
	if err != nil {
		return fail("record_selector_unsupported", err.Error())
	}
	titles, err := stringListArg(args["title"])
	if err != nil || len(titles) == 0 {
		return fail("record_title", "RECORD source requires a nonempty title fallback list")
	}
	for _, title := range titles {
		if title != "$key" && title != "$value" {
			if title == "" {
				return fail("record_title", "empty RECORD title path")
			}
			if _, err := CompileRecordSelector(title); err != nil {
				return fail("record_title", err.Error())
			}
		}
	}
	binding := &RecordSourceBinding{Select: selector, Title: titles, ScalarAs: stringArg(args["scalarAs"], ""), Container: stringArg(args["container"], ""), Space: stringArg(args["space"], ""), LogicalKind: stringArg(args["logicalKind"], ""), SourceOrder: intArg(args["sourceOrder"], 0)}
	for _, title := range titles {
		selector := RecordSelector{Text: title}
		if title != "$key" && title != "$value" {
			selector, _ = CompileRecordSelector(title)
		}
		binding.TitleSelectors = append(binding.TitleSelectors, selector)
	}
	if strings.TrimSpace(binding.Space) == "" || strings.TrimSpace(binding.LogicalKind) == "" {
		return fail("record_namespace", "RECORD space and logicalKind must be nonempty")
	}
	if binding.ScalarAs != "" && !recordPropertyName.MatchString(binding.ScalarAs) {
		return fail("record_scalar_as", "invalid scalarAs property")
	}
	if args["key"] == nil {
		binding.Key.Positional = true
	} else {
		key, ok := args["key"].(map[string]any)
		if !ok {
			return fail("record_key", "invalid RECORD key policy")
		}
		choices := 0
		if v, ok := key["field"]; ok {
			choices++
			binding.Key.Field = stringArg(v, "")
			if !recordPropertyName.MatchString(binding.Key.Field) {
				return fail("record_key", "key field must be a simple nonempty property")
			}
		}
		if v, ok := key["mappingKey"]; ok {
			choices++
			binding.Key.MappingKey = boolArg(v, false)
			if !binding.Key.MappingKey {
				return fail("record_key", "mappingKey must be true")
			}
		}
		if v, ok := key["template"]; ok {
			choices++
			text := stringArg(v, "")
			if text == "" {
				return fail("record_key", "key template must be nonempty")
			}
			binding.Key.Template, err = compileRecordTemplate(text)
			if err != nil {
				return fail("record_key_template", err.Error())
			}
		}
		if choices != 1 {
			return fail("record_key", "key policy must declare exactly one of field, mappingKey, template")
		}
	}
	if member, ok := args["membership"].(map[string]any); ok {
		path := stringArg(member["path"], "")
		selection, err := CompileRecordSelector(path)
		if err != nil || path == "" {
			return fail("record_membership", "membership requires an owner-local path")
		}
		for _, step := range selection.Steps {
			if step.Expansion != "" {
				return fail("record_membership", "membership cannot expand collections")
			}
		}
		includes := stringArg(member["includes"], "")
		if includes == "" {
			return fail("record_membership", "membership requires includes")
		}
		binding.Membership = &RecordMembership{Path: selection, Includes: includes, IncludeMissing: boolArg(member["includeMissing"], true)}
		if wildcard, ok := member["wildcard"].(string); ok {
			binding.Membership.Wildcard = &wildcard
		}
	}
	return binding, nil
}

// Validate record type relationships only after every concrete declaration exists.
func validateRecordBindings(schema *Schema) error {
	for _, t := range schema.Types {
		if t.Record == nil {
			for _, f := range t.Fields {
				if f.RecordContains != nil {
					if err := validateRecordContainment(schema, t, f); err != nil {
						return err
					}
				}
			}
			continue
		}
		visited := map[string]bool{t.Name: true}
		for parent := t.Record.Container; parent != ""; {
			p := schema.Types[parent]
			if p == nil || p.Record == nil {
				return compileError(nil, "record_container", t.Name, "", "record container %q must be a RECORD type", parent)
			}
			if visited[parent] {
				return compileError(nil, "record_container_cycle", t.Name, "", "cyclic RECORD containment")
			}
			visited[parent] = true
			if !recordSelectorPrefix(p.Record.Select, t.Record.Select) {
				return compileError(nil, "record_container_selector", t.Name, "", "record container selector must be an ancestor of its child selector")
			}
			parent = p.Record.Container
		}
		for _, f := range t.Fields {
			if f.RecordContains != nil {
				if err := validateRecordContainment(schema, t, f); err != nil {
					return err
				}
			}
			if f.Record != nil && f.Record.References != nil {
				ref := f.Record.References
				for _, name := range ref.To {
					if schema.Types[name] == nil && schema.Interfaces[name] == nil {
						return compileError(f.position, "record_reference_target", t.Name, f.Name, "unknown reference target %q", name)
					}
					if ref.TargetKey != "" {
						for _, target := range interfaceImplementersOrConcrete(schema, name) {
							if target.Record != nil || EffectiveTypeRole(target) != TypeRoleNote {
								continue
							}
							key := target.ByName[ref.TargetKey]
							if key == nil || key.Kind != FieldKindScalar || key.TypeName != "String" || key.List {
								return compileError(f.position, "record_reference_target_key", t.Name, f.Name, "targetKey %q must name a scalar String field on %s", ref.TargetKey, target.Name)
							}
						}
					}
					if f.Kind == FieldKindLink && !TypeMatchesOrImplements(schema, name, f.TypeName) && !TypeMatchesOrImplements(schema, f.TypeName, name) {
						return compileError(f.position, "record_reference_type", t.Name, f.Name, "reference targets disagree with declared object type")
					}
				}
				for _, target := range ref.Targets {
					found := false
					for _, name := range ref.To {
						if TypeMatchesOrImplements(schema, target.Type, name) {
							found = true
						}
					}
					if !found {
						return compileError(f.position, "record_reference_tag", t.Name, f.Name, "typed target is not admitted by to")
					}
				}
			}
		}
	}
	return nil
}
func recordSelectorPrefix(parent, child RecordSelector) bool {
	if len(parent.Steps) >= len(child.Steps) {
		return false
	}
	for i, p := range parent.Steps {
		c := child.Steps[i]
		if p.Name != c.Name {
			return false
		}
		if p.Expansion != c.Expansion && !(i == len(parent.Steps)-1 && p.Expansion == "" && c.Expansion == RecordExpandSequence) {
			return false
		}
	}
	return true
}

func validateRecordContainment(schema *Schema, parent *NoteType, field *Field) error {
	child := schema.Types[field.TypeName]
	fail := func(message string) error {
		return compileError(field.position, "record_contains_target", parent.Name, field.Name, "%s", message)
	}
	if child == nil || child.Record == nil {
		return fail("RECORD containment requires a RECORD target")
	}
	prefix := RecordSelector{}
	if parent.Record != nil {
		prefix = parent.Record.Select
		if child.Record.Container != parent.Name {
			return fail("RECORD child must declare this container")
		}
	} else if parent.Role != TypeRoleNote || child.Record.Container != "" {
		return fail("only a host note or its declared RECORD container can select this child")
	}
	suffix := field.RecordContains.Select
	combined := append(append([]RecordSelectorStep(nil), prefix.Steps...), suffix.Steps...)
	if len(combined) != len(child.Record.Select.Steps) {
		return fail("containment select disagrees with the child source selector")
	}
	for i, step := range combined {
		target := child.Record.Select.Steps[i]
		if step.Name != target.Name {
			return fail("containment select disagrees with the child source selector")
		}
		if step.Expansion != target.Expansion && !(step.Expansion == "" && target.Expansion == RecordExpandSequence) && (step.Expansion != RecordExpandSequence || target.Expansion != "") {
			return fail("containment expansion disagrees with the child source selector")
		}
	}
	return nil
}
