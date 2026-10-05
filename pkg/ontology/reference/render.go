package reference

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

func RenderMarkdown(schema *ontology.Schema, typeName string) (string, error) {
	docs, err := ontology.SchemaDocs(schema, typeName)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Ontology Reference\n")
	for _, doc := range docs {
		b.WriteString("\n## ")
		b.WriteString(doc.Name)
		b.WriteByte('\n')
		if doc.Summary != "" {
			b.WriteByte('\n')
			b.WriteString(doc.Summary)
			b.WriteByte('\n')
		}
		renderGuidanceBlock(&b, doc.Meaning, doc.Authoring, doc.AgentImplications, "")
		b.WriteString("\n- label: ")
		b.WriteString(firstNonEmpty(doc.Label, doc.Name))
		if doc.PluralLabel != "" {
			b.WriteString("\n- plural label: ")
			b.WriteString(doc.PluralLabel)
		}
		if doc.Role != "" {
			b.WriteString("\n- role: ")
			b.WriteString(string(doc.Role))
		}
		if structure := roleStructureLabel(doc); structure != "" {
			b.WriteString("\n- structure: ")
			b.WriteString(structure)
		}
		if len(doc.Implements) > 0 {
			b.WriteString("\n- implements: ")
			b.WriteString(strings.Join(doc.Implements, ", "))
		}
		if doc.Color != "" {
			b.WriteString("\n- color: ")
			b.WriteString(doc.Color)
		}
		if doc.KeyField != "" {
			b.WriteString("\n- keyField: ")
			b.WriteString(doc.KeyField)
		}
		if doc.PropertyCase != "" {
			b.WriteString("\n- propertyCase: ")
			b.WriteString(string(doc.PropertyCase))
		}
		if doc.Semantics != "" {
			b.WriteString("\n- semantics: ")
			b.WriteString(string(doc.Semantics))
		}
		if len(doc.Paths) > 0 {
			b.WriteString("\n- paths: ")
			b.WriteString(strings.Join(doc.Paths, ", "))
		}
		if len(doc.Matches) > 0 {
			b.WriteString("\n- matches: ")
			b.WriteString(strings.Join(doc.Matches, ", "))
		}
		if len(doc.CompanionDocs) > 0 {
			b.WriteString("\n- companion docs: ")
			b.WriteString(strings.Join(formatCompanionDocs(doc.CompanionDocs), "; "))
		}
		if annotations := formatAnnotations(doc.Annotations); len(annotations) > 0 {
			b.WriteString("\n- annotations: ")
			b.WriteString(strings.Join(annotations, "; "))
		}
		if profile := formatProfile(doc.Profile); profile != "" {
			b.WriteString("\n- view profile: ")
			b.WriteString(profile)
		}
		b.WriteString("\n\n### Fields\n")
		for _, field := range doc.Fields {
			b.WriteString("\n- ")
			b.WriteString(field.Name)
			if field.Required {
				b.WriteString("!")
			}
			b.WriteString(": ")
			b.WriteString(field.TypeName)
			if field.List {
				b.WriteString("[]")
			}
			b.WriteString(" [")
			b.WriteString(string(field.Kind))
			b.WriteString("]")
			if field.Description != "" {
				b.WriteString(" - ")
				b.WriteString(field.Summary)
			}
			if field.Semantics != "" {
				b.WriteString(" (")
				b.WriteString(string(field.Semantics))
				b.WriteString(")")
			}
			if field.Source != "" {
				b.WriteString("\n  source: ")
				b.WriteString(field.Source)
				if field.SourceKind != "" {
					b.WriteString(" [")
					b.WriteString(string(field.SourceKind))
					b.WriteString("]")
				}
			} else if field.SourceKind != "" {
				b.WriteString("\n  source kind: ")
				b.WriteString(string(field.SourceKind))
			}
			if len(field.SourceAliases) > 0 {
				b.WriteString("\n  source aliases: ")
				b.WriteString(strings.Join(field.SourceAliases, ", "))
			}
			if field.Inverse != "" {
				b.WriteString("\n  inverse: ")
				b.WriteString(field.Inverse)
			}
			if field.ReverseField != "" {
				b.WriteString("\n  reverse of authored link: ")
				b.WriteString(field.TypeName + "." + field.ReverseField)
			}
			if field.Direction != "" {
				b.WriteString("\n  direction: ")
				b.WriteString(string(field.Direction))
			}
			if field.Scope != "" {
				b.WriteString("\n  scope: ")
				b.WriteString(string(field.Scope))
				if field.Scope == ontology.NeighborScopeSubtree {
					b.WriteString(" (section-local subtree traversal)")
				}
			}
			if field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != ontology.EmbeddedSourceShapeSection {
				b.WriteString("\n  binding: ")
				b.WriteString(string(field.EmbeddedSourceShape))
				b.WriteString(" source span")
				if field.EmbeddedSourceMarker != "" {
					b.WriteString("\n  marker: ")
					b.WriteString(field.EmbeddedSourceMarker)
				}
			}
			if field.SectionHeading != "" {
				b.WriteString("\n  binding: heading-derived subtree")
				b.WriteString("\n  heading: ")
				b.WriteString(field.SectionHeading)
			}
			if field.SectionLevel != "" {
				b.WriteString("\n  level: ")
				b.WriteString(string(field.SectionLevel))
				b.WriteString("\n  matching: exact heading text + exact level")
			}
			if field.SectionRequired {
				b.WriteString("\n  section required: true")
			}
			if len(field.EnumValues) > 0 {
				b.WriteString("\n  enum: ")
				b.WriteString(strings.Join(field.EnumValues, ", "))
			}
			renderGuidanceBlock(&b, field.Meaning, field.Authoring, field.AgentImplications, "  ")
			if field.Policy != nil {
				b.WriteString("\n  policy: ")
				b.WriteString(formatPolicy(*field.Policy))
			}
			if field.Display != nil {
				b.WriteString("\n  display: ")
				b.WriteString(formatFieldDisplay(*field.Display))
			}
			if len(field.CompanionDocs) > 0 {
				b.WriteString("\n  companion docs: ")
				b.WriteString(strings.Join(formatCompanionDocs(field.CompanionDocs), "; "))
			}
			if annotations := formatAnnotations(field.Annotations); len(annotations) > 0 {
				b.WriteString("\n  annotations: ")
				b.WriteString(strings.Join(annotations, "; "))
			}
		}
		if len(doc.Enums) > 0 {
			b.WriteString("\n### Enums\n")
			for _, enum := range doc.Enums {
				b.WriteString("\n- ")
				b.WriteString(enum.Name)
				if enum.Summary != "" {
					b.WriteString(": ")
					b.WriteString(enum.Summary)
				}
				renderGuidanceBlock(&b, enum.Meaning, enum.Authoring, enum.AgentImplications, "  ")
				for _, value := range enum.Values {
					b.WriteString("\n  - `")
					b.WriteString(value.Name)
					b.WriteString("`")
					if value.Summary != "" {
						b.WriteString(": ")
						b.WriteString(value.Summary)
					}
					renderGuidanceBlock(&b, value.Meaning, value.Authoring, value.AgentImplications, "    ")
					if value.Policy != nil {
						b.WriteString("\n    policy: ")
						b.WriteString(formatPolicy(*value.Policy))
					}
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func renderGuidanceBlock(b *strings.Builder, meaning, authoring, implications, indent string) {
	if strings.TrimSpace(meaning) != "" {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString("meaning: ")
		b.WriteString(strings.TrimSpace(meaning))
	}
	if strings.TrimSpace(authoring) != "" {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString("authoring: ")
		b.WriteString(strings.TrimSpace(authoring))
	}
	if strings.TrimSpace(implications) != "" {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString("agent implications: ")
		b.WriteString(strings.TrimSpace(implications))
	}
}

func formatPolicy(policy ontology.PolicyHint) string {
	parts := make([]string, 0, 4)
	if policy.RequiresUserConfirmation {
		parts = append(parts, "requires user confirmation")
	}
	if policy.ForbidAutonomousSemanticEdits {
		parts = append(parts, "forbids autonomous semantic edits")
	}
	if strings.TrimSpace(policy.EditScope) != "" {
		parts = append(parts, "edit scope "+strings.TrimSpace(policy.EditScope))
	}
	if strings.TrimSpace(policy.Reason) != "" {
		parts = append(parts, strings.TrimSpace(policy.Reason))
	}
	if len(parts) == 0 {
		return "advisory metadata"
	}
	return strings.Join(parts, "; ")
}

func formatFieldDisplay(display ontology.FieldDisplay) string {
	parts := make([]string, 0, 3)
	if display.Role != ontology.FieldDisplayRoleNone {
		parts = append(parts, "role "+string(display.Role))
	}
	if display.EffectiveImportance() != ontology.FieldImportanceNormal {
		parts = append(parts, "importance "+string(display.EffectiveImportance()))
	}
	if display.HideHover {
		parts = append(parts, "hover false")
	}
	return strings.Join(parts, "; ")
}

// formatProfile renders a type profile's shape and the field roles it names.
func formatProfile(profile *ontology.TypeProfile) string {
	if profile == nil {
		return ""
	}
	parts := []string{string(profile.Shape)}
	for _, role := range []struct {
		label  string
		fields []string
	}{
		{"lifecycle", []string{profile.LifecycleField}},
		{"ordered", profile.OrderedFields},
		{"category", profile.CategoryFields},
		{"summary", []string{profile.SummaryField}},
		{"date", []string{profile.PrimaryDateField}},
		{"people", profile.PeopleFields},
		{"key text", profile.KeyTextFields},
		{"relations", profile.RelationFields},
		{"reverse", profile.ReverseFields},
		{"gaps", profile.GapFields},
	} {
		if fields := strings.Join(role.fields, ", "); fields != "" {
			parts = append(parts, role.label+" "+fields)
		}
	}
	return strings.Join(parts, "; ")
}

func RenderJSON(schema *ontology.Schema, typeName string) ([]byte, error) {
	docs, err := ontology.SchemaDocs(schema, typeName)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"types": docs})
}

func roleStructureLabel(doc ontology.TypeDoc) string {
	switch doc.Role {
	case ontology.TypeRoleSection:
		return "heading-derived structure used inside parent note bodies"
	case ontology.TypeRoleEmbeddedNode:
		return "embedded graph node persisted inside a parent note body"
	case ontology.TypeRoleInterface:
		if doc.Name == "Section" {
			return "built-in contract for heading-derived section nodes"
		}
		return "abstract shared contract"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatAnnotations(annotations map[string]map[string]any) []string {
	if len(annotations) == 0 {
		return nil
	}
	names := make([]string, 0, len(annotations))
	for name := range annotations {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "companionDocs" {
			continue
		}
		encoded, err := json.Marshal(normalizeAnnotationPayload(name, annotations[name]))
		if err != nil {
			out = append(out, "@"+name)
			continue
		}
		if string(encoded) == "{}" {
			out = append(out, "@"+name)
			continue
		}
		out = append(out, fmt.Sprintf("@%s %s", name, string(encoded)))
	}
	return out
}

func normalizeAnnotationPayload(name string, values map[string]any) map[string]any {
	if len(values) == 0 {
		return values
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	if name == "contains" && cloned["display"] == string(ontology.SectionDisplayPane) {
		delete(cloned, "display")
	}
	if name == "contains" && cloned["shape"] == string(ontology.EmbeddedSourceShapeSection) {
		delete(cloned, "shape")
	}
	return cloned
}

func formatCompanionDocs(docs []ontology.CompanionDocRef) []string {
	if len(docs) == 0 {
		return nil
	}
	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		if strings.TrimSpace(doc.Path) == "" {
			continue
		}
		label := fmt.Sprintf("`%s`", doc.Path)
		if strings.TrimSpace(doc.Purpose) != "" {
			label += " (" + doc.Purpose + ")"
		}
		out = append(out, label)
	}
	return out
}
