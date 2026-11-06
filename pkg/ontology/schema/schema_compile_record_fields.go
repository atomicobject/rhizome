package schema

import (
	"github.com/vektah/gqlparser/v2/ast"
	"strings"
)

func isRecordContains(def *ast.FieldDefinition) bool {
	dir := def.Directives.ForName("contains")
	return dir != nil && embeddedSourceShapeArg(dir.ArgumentMap(nil)["shape"]) == EmbeddedSourceShapeRecord
}

func compileRecordField(parent *ast.Definition, def *ast.FieldDefinition, target *ast.Definition, field *Field, schema *Schema) (*Field, error) {
	fail := func(code, message string) (*Field, error) {
		return nil, compileError(def.Position, code, parent.Name, def.Name, "%s", message)
	}
	for _, name := range []string{"link", "neighbors", "reverse", "authoring", "identifier", "workspaceMember"} {
		if def.Directives.ForName(name) != nil {
			return fail("record_field_directive", "RECORD field cannot declare @"+name)
		}
	}
	if contains := def.Directives.ForName("contains"); contains != nil {
		if !isRecordContains(def) || !isRecordDefinition(target) {
			return fail("record_contains_target", "RECORD containment requires shape RECORD and a RECORD target")
		}
		for _, name := range []string{"field", "references", "format"} {
			if def.Directives.ForName(name) != nil {
				return fail("record_contains_directive", "RECORD containment cannot declare @"+name)
			}
		}
		for _, name := range []string{"level", "heading", "marker"} {
			if contains.Arguments.ForName(name) != nil {
				return fail("record_contains_syntax", "RECORD containment cannot declare "+name)
			}
		}
		if contains.Arguments.ForName("select") == nil {
			return fail("record_contains_select", "RECORD containment requires explicit select")
		}
		args := contains.ArgumentMap(nil)
		if args["select"] == nil {
			return fail("record_contains_select", "RECORD select must be a string")
		}
		selector, err := CompileRecordSelector(stringArg(args["select"], ""))
		if err != nil {
			return fail("record_selector_unsupported", err.Error())
		}
		edge, err := compileRecordEdge(args["edge"])
		if err != nil {
			return fail("record_edge", err.Error())
		}
		field.Kind = FieldKindSection
		field.Source = ""
		field.SourceKind = ""
		field.EmbeddedSourceShape = EmbeddedSourceShapeRecord
		field.RecordContains = &RecordContainmentBinding{Select: selector, Edge: edge}
		field.SectionDisplay = sectionDisplayArg(args["display"])
		field.SectionRequired = boolArg(args["required"], false)
		field.Required = field.Required || field.SectionRequired
		field.ContainsMin = intArg(args["min"], 0)
		field.ContainsMax = intArg(args["max"], -1)
		if field.ContainsMin < 0 || field.ContainsMax < -1 || (field.ContainsMax >= 0 && field.ContainsMax < field.ContainsMin) {
			return fail("record_contains_bounds", "invalid RECORD containment bounds")
		}
		return field, nil
	}
	dir := def.Directives.ForName("field")
	if dir == nil || dir.Arguments.ForName("path") == nil {
		return fail("record_field_path", "RECORD fields require @field(path:)")
	}
	for _, name := range []string{"source", "sources", "sourceKind"} {
		if dir.Arguments.ForName(name) != nil {
			return fail("record_field_source", "RECORD fields use only owner-local path, without "+name)
		}
	}
	args := dir.ArgumentMap(nil)
	path := stringArg(args["path"], "")
	if path == "" {
		return fail("record_field_path", "RECORD field path must be nonempty")
	}
	selector, err := CompileRecordSelector(path)
	if err != nil {
		return fail("record_selector_unsupported", err.Error())
	}
	field.Source = ""
	field.SourcePath = path
	field.SourceKind = FieldSourceProperty
	field.Record = &RecordFieldBinding{Path: selector, MappingKeyFallback: stringArg(args["fallback"], "") == "MAPPING_KEY"}
	if schema.EnumTypes[field.TypeName] != nil {
		field.Kind = FieldKindEnum
	} else if target.Kind == ast.Scalar {
		field.Kind = FieldKindScalar
	} else {
		field.Kind = FieldKindLink
	}
	if ref := def.Directives.ForName("references"); ref != nil {
		binding, err := compileRecordReferences(ref)
		if err != nil {
			return fail("record_references", err.Error())
		}
		field.Record.References = binding
	} else if field.Kind == FieldKindLink {
		return fail("record_reference_binding", "object RECORD fields require @references")
	}
	if err := applyFormatDirective(field, parent, def); err != nil {
		return nil, err
	}
	return field, nil
}

func compileRecordEdge(input any) (*RecordEdge, error) {
	if input == nil {
		return nil, nil
	}
	args, ok := input.(map[string]any)
	if !ok {
		return nil, recordBindingError("invalid edge")
	}
	name := strings.TrimSpace(stringArg(args["name"], ""))
	if name == "" {
		return nil, recordBindingError("edge name must be nonempty")
	}
	direction := NeighborDirection(stringArg(args["direction"], "OUTBOUND"))
	if direction != NeighborDirectionOutbound && direction != NeighborDirectionInbound {
		return nil, recordBindingError("edge direction must be OUTBOUND or INBOUND")
	}
	return &RecordEdge{Name: name, Direction: direction}, nil
}
func compileRecordReferences(dir *ast.Directive) (*RecordReferenceBinding, error) {
	args := dir.ArgumentMap(nil)
	to, err := stringListArg(args["to"])
	if err != nil || len(to) == 0 {
		return nil, recordBindingError("references requires nonempty to")
	}
	ref := &RecordReferenceBinding{To: to, Mode: stringArg(args["mode"], ""), Encoding: stringArg(args["encoding"], "ID"), Values: stringArg(args["values"], "VALUE"), MatchName: stringArg(args["matchName"], ""), AllowWildcard: boolArg(args["allowWildcard"], false)}
	if ref.Mode != "STRICT" && ref.Mode != "TOLERANT" {
		return nil, recordBindingError("reference mode must be explicit")
	}
	ref.Edge, err = compileRecordEdge(args["edge"])
	if err != nil {
		return nil, err
	}
	if ref.MatchName != "" {
		if !recordPropertyName.MatchString(ref.MatchName) {
			return nil, recordBindingError("matchName must name one property")
		}
	}
	if ref.MatchName != "" {
		ref.MatchPath = RecordSelector{Text: ref.MatchName, Steps: []RecordSelectorStep{{Name: ref.MatchName}}}
	}
	tags := map[string]bool{}
	if targets, ok := args["targets"].([]any); ok {
		for _, value := range targets {
			target, ok := value.(map[string]any)
			if !ok {
				return nil, recordBindingError("invalid typed target")
			}
			tag := stringArg(target["tag"], "")
			name := stringArg(target["type"], "")
			if tag == "" || name == "" || tags[tag] || strings.ContainsAny(tag, ":.") {
				return nil, recordBindingError("typed target tags must be nonempty and unique")
			}
			tags[tag] = true
			member, err := CompileRecordSelector(stringArg(target["memberPath"], ""))
			if err != nil {
				return nil, err
			}
			ref.Targets = append(ref.Targets, RecordTarget{Tag: tag, Type: name, MemberPath: member})
		}
	}
	if ref.Encoding == "TYPED" && len(ref.Targets) == 0 {
		return nil, recordBindingError("TYPED references require targets")
	}
	if ref.Encoding != "TYPED" && len(ref.Targets) > 0 {
		return nil, recordBindingError("targets require TYPED encoding")
	}
	return ref, nil
}

type recordBindingError string

func (e recordBindingError) Error() string { return string(e) }
