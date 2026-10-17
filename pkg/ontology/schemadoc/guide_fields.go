package schemadoc

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
)

type fieldBuckets struct {
	required []*ontoschema.Field
	optional []*ontoschema.Field
}

func authoredGuideFields(fields []*ontoschema.Field) fieldBuckets {
	out := fieldBuckets{}
	for _, field := range fields {
		if field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse {
			continue
		}
		required := authoredFieldRequired(field)
		if required {
			out.required = append(out.required, field)
			continue
		}
		out.optional = append(out.optional, field)
	}
	return out
}

func authoredFieldRequired(field *ontoschema.Field) bool {
	if field == nil {
		return false
	}
	if isOptionalAuthoredIdentifier(field) {
		return false
	}
	return field.Required || field.SectionRequired
}

func isOptionalAuthoredIdentifier(field *ontoschema.Field) bool {
	return field != nil && field.IsPreferredIdentifier && field.IsDerivableIdentifier && field.IdentifierPopulate != ontoschema.IdentifierPopulateOnCreate
}

func authoredGuideTypeFields(noteType *guideType) fieldBuckets {
	if noteType == nil {
		return fieldBuckets{}
	}
	return authoredGuideFields(noteType.Fields)
}

func derivedFields(noteType *guideType) []*ontoschema.Field {
	if noteType == nil {
		return nil
	}
	out := make([]*ontoschema.Field, 0)
	for _, field := range noteType.Fields {
		if field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse {
			out = append(out, field)
		}
	}
	return out
}

func renderFieldGuide(b *strings.Builder, schema *ontoschema.Schema, noteType *guideType, field *ontoschema.Field, required bool, resolve CompanionDocResolver) {
	if field == nil {
		return
	}
	b.WriteString("\n- `")
	b.WriteString(field.Name)
	b.WriteString("`: ")
	if field.Kind == ontoschema.FieldKindSection {
		required = required || field.SectionRequired
	}
	if isOptionalAuthoredIdentifier(field) {
		required = false
	}
	if required {
		b.WriteString("required; ")
	} else {
		b.WriteString("optional; ")
	}
	b.WriteString(locationLabel(field))
	if field.Kind == ontoschema.FieldKindSection || field.SourceKind == ontoschema.FieldSourceCheckbox {
		b.WriteString("; ")
	} else {
		source := authoredSource(field, noteType)
		b.WriteString(" `")
		b.WriteString(source)
		b.WriteString("`; ")
	}
	if len(field.SourceAliases) > 0 {
		b.WriteString("also accepts `")
		b.WriteString(strings.Join(field.SourceAliases, "`, `"))
		b.WriteString("`; ")
	}
	b.WriteString(valueShape(field))
	if field.Kind == ontoschema.FieldKindSection {
		b.WriteString("; heading `")
		b.WriteString(renderHeading(field.SectionLevel, field.SectionHeading))
		b.WriteString("`")
	}
	if ontoschema.IsSectionSummary(field) {
		b.WriteString("; author and edit summary prose in this section; compact views use its body; query with `" + field.Name + " { content }`")
	}
	if field.Description != "" {
		b.WriteString("; ")
		b.WriteString(field.Description)
	}
	if field.Kind == ontoschema.FieldKindLink {
		b.WriteString("; target `")
		b.WriteString(field.TypeName)
		if field.List {
			b.WriteString("[]")
		}
		b.WriteString("`")
		b.WriteString("; query with `eq` or `in` for resolved target membership, or `exists`; `contains` applies to scalar text, not links")
		if field.Inverse != "" {
			b.WriteString("; inverse `")
			b.WriteString(field.Inverse)
			b.WriteString("` should also be true on the target note")
		}
		if field.IncludeBodyLinks || field.IncludeBacklinks {
			b.WriteString("; ambient discovery ")
			b.WriteString(ambientLinkPolicy(field))
		}
	}
	if field.Kind == ontoschema.FieldKindNeighbor && field.Scope == ontoschema.NeighborScopeSubtree {
		b.WriteString("; scoped to links found inside the matched section subtree")
	}
	if field.IsPreferredIdentifier {
		b.WriteString("; preferred identifier")
		if field.IsDerivableIdentifier {
			b.WriteString("; derivable from parent structure")
		}
		switch field.IdentifierPopulate {
		case ontoschema.IdentifierPopulateOnCreate:
			b.WriteString("; author and populate this field when creating the node")
		case ontoschema.IdentifierPopulateOnLink:
			b.WriteString("; author only when a durable external link target is needed")
		}
	}
	if len(ontoschema.EnumValuesSet(schema, field.TypeName)) > 0 {
		values := ontologyValues(ontoschema.EnumValuesSet(schema, field.TypeName))
		b.WriteString("; enum values: `")
		b.WriteString(strings.Join(values, "`, `"))
		b.WriteString("`")
		if enum := ontologyEnumDoc(schema, field.TypeName); enum != nil {
			for _, value := range enum.Values {
				if value.Summary == "" && value.Meaning == "" && value.Authoring == "" && value.AgentImplications == "" && value.Policy == nil {
					continue
				}
				b.WriteString("\n  value `")
				b.WriteString(value.Name)
				b.WriteString("`")
				if value.Summary != "" {
					b.WriteString(": ")
					b.WriteString(value.Summary)
				}
				if value.Meaning != "" {
					b.WriteString("; meaning: ")
					b.WriteString(value.Meaning)
				}
				if value.Authoring != "" {
					b.WriteString("; authoring: ")
					b.WriteString(value.Authoring)
				}
				if value.AgentImplications != "" {
					b.WriteString("; agent implications: ")
					b.WriteString(value.AgentImplications)
				}
				if value.Policy != nil {
					b.WriteString("; policy: ")
					b.WriteString(formatGuidePolicy(*value.Policy))
				}
			}
		}
	}
	if field.Guidance != nil {
		renderGuideGuidanceInline(b, guideSummary(field.Guidance, ""), guideMeaning(field.Guidance), guideAuthoring(field.Guidance), guideAgentImplications(field.Guidance))
	}
	if field.Policy != nil {
		b.WriteString("; policy: ")
		b.WriteString(formatGuidePolicy(*field.Policy))
	}
	if notes := fieldDisplayNotes(field.Display); len(notes) > 0 {
		b.WriteString("; display: ")
		b.WriteString(strings.Join(notes, ", "))
	}
	if notes := fieldToolingNotes(field); len(notes) > 0 {
		b.WriteString("; tooling: ")
		b.WriteString(strings.Join(notes, "; "))
	}
	if len(field.CompanionDocs) > 0 {
		b.WriteString("; companion docs: ")
		b.WriteString(strings.Join(formatCompanionDocs(field.CompanionDocs, resolve), "; "))
	}
}

func fieldDisplayNotes(display ontoschema.FieldDisplay) []string {
	var out []string
	if display.Role != ontoschema.FieldDisplayRoleNone {
		out = append(out, "role "+string(display.Role))
	}
	if display.EffectiveImportance() != ontoschema.FieldImportanceNormal {
		out = append(out, "importance "+string(display.EffectiveImportance()))
	}
	if display.HideHover {
		out = append(out, "hidden from hover previews")
	}
	return out
}

func locationLabel(field *ontoschema.Field) string {
	if field == nil {
		return "frontmatter key"
	}
	if field.Kind == ontoschema.FieldKindSection {
		return "markdown heading"
	}
	switch field.SourceKind {
	case ontoschema.FieldSourceInline:
		return "inline property"
	case ontoschema.FieldSourceCheckbox:
		return "checkbox state token (`[ ]` unchecked, `[x]` checked) on the host item"
	}
	return "frontmatter key"
}

func valueShape(field *ontoschema.Field) string {
	if field == nil {
		return "value"
	}
	base := field.TypeName
	if field.Kind == ontoschema.FieldKindSection {
		base = "section body"
	}
	if field.Kind == ontoschema.FieldKindLink && field.TypeName != "Note" {
		base = "link to " + field.TypeName
	}
	if field.Kind == ontoschema.FieldKindLink && field.TypeName == "Note" {
		base = "link to another note"
	}
	switch {
	case field.List:
		return "list of " + base + " values"
	default:
		return "single " + base + " value"
	}
}

func ambientLinkPolicy(field *ontoschema.Field) string {
	switch {
	case field.IncludeBodyLinks && field.IncludeBacklinks:
		return "includes body links and backlinks"
	case field.IncludeBodyLinks:
		return "includes body links only"
	case field.IncludeBacklinks:
		return "includes backlinks only"
	default:
		return "does not add body-link/backlink expansion"
	}
}

func ontologyValues(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validationGuideNotes(schema *ontoschema.Schema, role ontoschema.TypeRole, fields []*ontoschema.Field) []string {
	out := make([]string, 0, len(fields)+2)
	if role == ontoschema.TypeRoleNote {
		out = append(out, "the note must still match this type's selectors; `type:` alone is not enough")
	}
	seen := map[string]struct{}{}
	for _, field := range fields {
		appendValidationNotes(&out, seen, schema, field, field.Name)
	}
	return out
}

func appendValidationNotes(out *[]string, seen map[string]struct{}, schema *ontoschema.Schema, field *ontoschema.Field, fieldPath string) {
	if field == nil {
		return
	}
	add := func(note string) {
		if note == "" {
			return
		}
		if _, ok := seen[note]; ok {
			return
		}
		seen[note] = struct{}{}
		*out = append(*out, note)
	}
	if field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse {
		if field.Kind == ontoschema.FieldKindReverse {
			add(fmt.Sprintf("do not author `%s`; it is the exact reverse of `%s.%s`", fieldPath, field.TypeName, field.ReverseField))
			return
		}
		add(fmt.Sprintf("do not author `%s`; neighbor fields are derived from existing typed links/backlinks", fieldPath))
		return
	}
	if field.Kind == ontoschema.FieldKindSection {
		if field.SectionRequired {
			add(fmt.Sprintf("missing `%s` raises `missing_required_section`", fieldPath))
			add(fmt.Sprintf("empty `%s` raises `empty_required_section`", fieldPath))
			add(fmt.Sprintf("wrong `%s` heading level raises `wrong_section_level`", fieldPath))
		}
		if !field.List {
			add(fmt.Sprintf("duplicate `%s` headings raise `duplicate_section`", fieldPath))
		}
		if schema != nil {
			sectionType := schema.Types[field.TypeName]
			if sectionType != nil {
				for _, nested := range sectionType.Fields {
					appendValidationNotes(out, seen, schema, nested, fieldPath+"."+nested.Name)
				}
			}
		}
		return
	}
	// Checkbox-source fields are read directly from the host item's `[ ]`/`[x]`
	// token. Authors cannot omit them or duplicate them — the parser always sees
	// exactly one boolean per item — so the missing-required / multiple-values
	// traps don't apply.
	if field.SourceKind != ontoschema.FieldSourceCheckbox {
		if field.Required && !isOptionalAuthoredIdentifier(field) {
			add(fmt.Sprintf("missing `%s` raises `missing_required_field`", fieldPath))
		}
		if !field.List {
			add(fmt.Sprintf("multiple `%s` values raise `field_shape_mismatch`", fieldPath))
		}
	}
	if field.Kind == ontoschema.FieldKindLink {
		add(fmt.Sprintf("`%s` targets must resolve to notes; unresolved links raise `link_target_missing`", fieldPath))
		if field.TypeName != "Note" {
			add(fmt.Sprintf("`%s` targets must resolve to `%s`; mismatches raise `wrong_target_type`", fieldPath, field.TypeName))
		}
		if field.Inverse != "" {
			add(fmt.Sprintf("`%s` expects inverse `%s`; missing inverse edges raise `inverse_mismatch`", fieldPath, field.Inverse))
		}
	}
}

func relatedReason(schema *ontoschema.Schema, sourceName, relatedName string) string {
	source := guideTypeFromSchema(schema, sourceName)
	related := guideTypeFromSchema(schema, relatedName)
	if source == nil || related == nil {
		return "related by ontology links"
	}
	reasons := make([]string, 0, 4)
	for _, iface := range source.Implements {
		if iface == relatedName {
			reasons = append(reasons, "implemented interface")
		}
	}
	for _, field := range source.Fields {
		if (field.Kind == ontoschema.FieldKindLink || field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse || field.Kind == ontoschema.FieldKindSection) && field.TypeName == related.Name {
			reasons = append(reasons, fmt.Sprintf("referenced by `%s`", field.Name))
		}
	}
	for _, field := range related.Fields {
		if field.Kind == ontoschema.FieldKindLink && field.TypeName == source.Name && field.Inverse != "" {
			reasons = append(reasons, fmt.Sprintf("inverse owner `%s`", field.Name))
		}
	}
	if related.Role == ontoschema.TypeRoleNote {
		for _, iface := range related.Implements {
			if iface == sourceName {
				reasons = append(reasons, "implementer of this interface")
				break
			}
		}
	}
	if len(reasons) == 0 {
		return "related by ontology links"
	}
	return strings.Join(reasons, "; ")
}

func relatedTypeGuidance(schema *ontoschema.Schema, typeName string) string {
	related := guideTypeFromSchema(schema, typeName)
	if related == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if related.Summary != "" {
		parts = append(parts, compactGuideText(related.Summary))
	} else if related.Meaning != "" {
		parts = append(parts, compactGuideText(related.Meaning))
	} else if related.Description != "" {
		parts = append(parts, compactGuideText(related.Description))
	}
	switch related.Role {
	case ontoschema.TypeRoleNote:
		parts = append(parts, "Before creating or substantially editing one, run `rzm agent ontology-authoring-guide --type "+related.Name+"`.")
	case ontoschema.TypeRoleEmbeddedNode, ontoschema.TypeRoleSection:
		parts = append(parts, "Read this type's authoring guide before authoring that embedded structure directly.")
	}
	return strings.Join(parts, " ")
}

func compactGuideText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func authoredSource(field *ontoschema.Field, noteType *guideType) string {
	if field == nil {
		return ""
	}
	if strings.TrimSpace(field.Source) != "" {
		return field.Source
	}
	if field.SourceKind == ontoschema.FieldSourceInline {
		propertyCase := ontoschema.PropertyCaseKebab
		if noteType != nil && noteType.PropertyCase != "" {
			propertyCase = noteType.PropertyCase
		}
		return ontoschema.DefaultPropertyName(field.Name, propertyCase)
	}
	return field.Source
}
