package guide

import (
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"sort"
	"strings"
)

func renderGuideGuidance(b *strings.Builder, meaning, authoring, implications, indent string) {
	for _, entry := range []struct{ label, text string }{
		{"Meaning", meaning},
		{"Authoring guidance", authoring},
		{"Agent implications", implications},
	} {
		if text := strings.TrimSpace(entry.text); text != "" {
			fmt.Fprintf(b, "\n%s%s: %s\n", indent, entry.label, text)
		}
	}
}

func renderGuideGuidanceInline(b *strings.Builder, summary, meaning, authoring, implications string) {
	for _, entry := range []struct{ label, text string }{
		{"summary", summary},
		{"meaning", meaning},
		{"authoring", authoring},
		{"agent implications", implications},
	} {
		if text := strings.TrimSpace(entry.text); text != "" {
			fmt.Fprintf(b, "; %s: %s", entry.label, text)
		}
	}
}

func ontologyEnumDoc(schema *ontology.Schema, typeName string) *ontology.EnumDoc {
	if schema == nil {
		return nil
	}
	enumType := schema.EnumTypes[typeName]
	if enumType == nil {
		return nil
	}
	doc := &ontology.EnumDoc{
		Name:              enumType.Name,
		Summary:           guideSummary(enumType.Guidance, enumType.Description),
		Meaning:           guideMeaning(enumType.Guidance),
		Authoring:         guideAuthoring(enumType.Guidance),
		AgentImplications: guideAgentImplications(enumType.Guidance),
		Description:       enumType.Description,
		Values:            make([]ontology.EnumValueDoc, 0, len(enumType.Values)),
	}
	for _, value := range enumType.Values {
		if value == nil {
			continue
		}
		doc.Values = append(doc.Values, ontology.EnumValueDoc{
			Name:              value.Name,
			Summary:           guideSummary(value.Guidance, value.Description),
			Meaning:           guideMeaning(value.Guidance),
			Authoring:         guideAuthoring(value.Guidance),
			AgentImplications: guideAgentImplications(value.Guidance),
			Description:       value.Description,
			Policy:            cloneGuidePolicy(value.Policy),
		})
	}
	return doc
}

func guideSummary(guidance *ontology.Guidance, fallback string) string {
	if guidance != nil && strings.TrimSpace(guidance.Summary) != "" {
		return strings.TrimSpace(guidance.Summary)
	}
	return strings.TrimSpace(fallback)
}

func guideMeaning(guidance *ontology.Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.Meaning)
}

func guideAuthoring(guidance *ontology.Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.Authoring)
}

func guideAgentImplications(guidance *ontology.Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.AgentImplications)
}

func formatGuidePolicy(policy ontology.PolicyHint) string {
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

func cloneGuidePolicy(in *ontology.PolicyHint) *ontology.PolicyHint {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func toolingNotes(noteType *guideType) []string {
	if noteType == nil {
		return nil
	}
	out := make([]string, 0, 3)
	if noteType.Semantics != "" {
		out = append(out, fmt.Sprintf("type semantics: `%s`", noteType.Semantics))
	}
	for _, name := range sortedAnnotationNames(noteType.Annotations) {
		if name == "companionDocs" {
			continue
		}
		payload := normalizeAnnotationPayload(name, noteType.Annotations[name])
		if len(payload) == 0 {
			out = append(out, fmt.Sprintf("@%s", name))
			continue
		}
		encoded, _ := json.Marshal(payload)
		out = append(out, fmt.Sprintf("@%s %s", name, encoded))
	}
	return out
}

func fieldToolingNotes(field *ontology.Field) []string {
	if field == nil {
		return nil
	}
	out := make([]string, 0, 3)
	if field.Semantics != "" {
		out = append(out, fmt.Sprintf("semantics `%s`", field.Semantics))
	}
	for _, name := range sortedAnnotationNames(field.Annotations) {
		if name == "semantics" || name == "companionDocs" {
			continue
		}
		payload := normalizeAnnotationPayload(name, field.Annotations[name])
		if len(payload) == 0 {
			out = append(out, "@"+name)
			continue
		}
		encoded, _ := json.Marshal(payload)
		out = append(out, fmt.Sprintf("@%s %s", name, encoded))
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
	// @link directive defaults are noise: keep the payload focused on
	// non-default overrides so authors don't see contradictory tooling hints
	// (e.g. `sourceKind:"FRONTMATTER"` on a field the compiler routes to
	// inline-source on embedded nodes).
	if name == "link" {
		if cloned["sourceKind"] == string(ontology.FieldSourceFrontmatter) {
			delete(cloned, "sourceKind")
		}
		if cloned["includeBodyLinks"] == true {
			delete(cloned, "includeBodyLinks")
		}
		if cloned["includeBacklinks"] == true {
			delete(cloned, "includeBacklinks")
		}
		if cloned["contextInclude"] == false {
			delete(cloned, "contextInclude")
		}
	}
	if name == "field" && cloned["sourceKind"] == string(ontology.FieldSourceFrontmatter) {
		delete(cloned, "sourceKind")
	}
	if name == "identifier" && cloned["strategy"] == string(ontology.IdentifierStrategyDateTime) {
		delete(cloned, "pad")
	}
	if name == "node" && cloned["default"] == false {
		delete(cloned, "default")
	}
	return cloned
}

func formatCompanionDocs(docs []ontology.CompanionDocRef, resolve CompanionDocResolver) []string {
	if len(docs) == 0 {
		return nil
	}
	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		formatted := formatCompanionDoc(doc, resolve)
		if formatted != "" {
			out = append(out, formatted)
		}
	}
	return out
}

func formatCompanionDoc(doc ontology.CompanionDocRef, resolve CompanionDocResolver) string {
	path := strings.TrimSpace(doc.Path)
	if path == "" {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(doc.Purpose) != "" {
		b.WriteString("`")
		b.WriteString(doc.Purpose)
		b.WriteString("`: ")
	}
	b.WriteString("`")
	b.WriteString(path)
	b.WriteString("`")
	if resolve == nil {
		return b.String()
	}
	meta, ok := resolve(path)
	if !ok {
		return b.String()
	}
	if strings.TrimSpace(meta.Summary) != "" {
		b.WriteString(" — ")
		b.WriteString(strings.TrimSpace(meta.Summary))
		return b.String()
	}
	if strings.TrimSpace(meta.Title) != "" {
		b.WriteString(" — ")
		b.WriteString(strings.TrimSpace(meta.Title))
	}
	return b.String()
}

func sortedAnnotationNames(annotations map[string]map[string]any) []string {
	if len(annotations) == 0 {
		return nil
	}
	out := make([]string, 0, len(annotations))
	for name := range annotations {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
