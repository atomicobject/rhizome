package guide

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

// skeletonWithID renders the same skeleton as skeleton, but prefills the
// preferred-identifier field with suggestedID when the type declares one.
// Pass an empty suggestedID to keep the historical blank-id behavior.
func skeletonWithID(schema *ontology.Schema, noteType *guideType, suggestedID string) string {
	if noteType == nil {
		return "---\n---"
	}
	if noteType.Role == ontology.TypeRoleEmbeddedNode {
		switch noteType.SourceShape {
		case ontology.EmbeddedSourceShapeCheckboxItem, ontology.EmbeddedSourceShapeListItem:
			return embeddedItemSkeleton(noteType, suggestedID)
		}
	}
	preferredField := preferredIdentifierField(noteType)
	preferredAuthored := ""
	if preferredField != nil {
		preferredAuthored = authoredSource(preferredField, noteType)
	}

	writeField := func(b *strings.Builder, field *ontology.Field) {
		key := authoredSource(field, noteType)
		b.WriteString(key)
		if field.List {
			b.WriteString(": ")
			b.WriteString("\n  - ")
			if suggestedID != "" && key == "aliases" && preferredAuthored != "" {
				b.WriteString(suggestedID)
			}
			b.WriteByte('\n')
			return
		}
		if suggestedID != "" && preferredField != nil && field == preferredField {
			b.WriteString(": ")
			b.WriteString(suggestedID)
		} else {
			b.WriteByte(':')
		}
		b.WriteByte('\n')
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: ")
	b.WriteString(noteType.Name)
	b.WriteByte('\n')
	fields := authoredGuideTypeFields(noteType)
	orderedFields := append(fields.required, fields.optional...)
	for _, field := range orderedFields {
		if field.SourceKind == ontology.FieldSourceInline {
			continue
		}
		if field.Kind == ontology.FieldKindSection {
			continue
		}
		writeField(&b, field)
	}
	b.WriteString("---\n")
	inline := make([]string, 0)
	for _, field := range orderedFields {
		if field.SourceKind != ontology.FieldSourceInline {
			continue
		}
		if isOptionalAuthoredIdentifier(field) {
			continue
		}
		inline = append(inline, fmt.Sprintf("%s:: ", authoredSource(field, noteType)))
	}
	if len(inline) > 0 {
		b.WriteByte('\n')
		for _, line := range inline {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	seen := make(map[string]struct{})
	for _, field := range orderedFields {
		if field.Kind != ontology.FieldKindSection {
			continue
		}
		renderSectionSkeleton(&b, schema, field, seen)
	}
	return strings.TrimRight(b.String(), "\n")
}

// embeddedItemSkeleton renders a checkbox- or list-shaped embedded node as the
// raw markdown line authors should write into a host note, with inline
// properties indented underneath.
func embeddedItemSkeleton(noteType *guideType, suggestedID string) string {
	var b strings.Builder
	switch noteType.SourceShape {
	case ontology.EmbeddedSourceShapeCheckboxItem:
		b.WriteString("- [ ] <task description>")
	default:
		b.WriteString("- <item text>")
	}
	if marker := strings.TrimSpace(noteType.SourceMarker); marker != "" {
		b.WriteString(" ")
		b.WriteString(marker)
	}
	b.WriteByte('\n')

	preferredField := preferredIdentifierField(noteType)
	fields := authoredGuideTypeFields(noteType)
	for _, field := range append(fields.required, fields.optional...) {
		if field.SourceKind != ontology.FieldSourceInline {
			continue
		}
		if isOptionalAuthoredIdentifier(field) {
			continue
		}
		key := authoredSource(field, noteType)
		b.WriteString("  ")
		b.WriteString(key)
		b.WriteString(":: ")
		if suggestedID != "" && preferredField != nil && field == preferredField {
			b.WriteString(suggestedID)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// sourcePathsMatchAll reports whether the @source paths effectively match every
// markdown file (i.e. the rule does not narrow the host-note set). When that's
// the case, listing the paths in the resolution narrative is just noise.
func sourcePathsMatchAll(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		switch strings.TrimSpace(p) {
		case "**/*.md", "**/*", "*", "**":
			continue
		default:
			return false
		}
	}
	return true
}

func renderHeading(level ontology.SectionLevel, heading string) string {
	hashes := "##"
	switch level {
	case ontology.SectionLevelH1:
		hashes = "#"
	case ontology.SectionLevelH2:
		hashes = "##"
	case ontology.SectionLevelH3:
		hashes = "###"
	case ontology.SectionLevelH4:
		hashes = "####"
	case ontology.SectionLevelH5:
		hashes = "#####"
	case ontology.SectionLevelH6:
		hashes = "######"
	}
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return hashes
	}
	return hashes + " " + heading
}

func renderSectionSkeleton(b *strings.Builder, schema *ontology.Schema, field *ontology.Field, seen map[string]struct{}) {
	if b == nil || field == nil {
		return
	}
	b.WriteString(renderHeading(field.SectionLevel, field.SectionHeading))
	b.WriteString("\n\n")
	if schema == nil || strings.TrimSpace(field.TypeName) == "" {
		return
	}
	key := field.TypeName + "\x00" + field.Name
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	sectionType := guideTypeFromSchema(schema, field.TypeName)
	if sectionType == nil || sectionType.Role != ontology.TypeRoleSection {
		delete(seen, key)
		return
	}
	fields := authoredGuideTypeFields(sectionType)
	for _, nested := range append(fields.required, fields.optional...) {
		if nested.Kind != ontology.FieldKindSection {
			continue
		}
		renderSectionSkeleton(b, schema, nested, seen)
	}
	delete(seen, key)
}
