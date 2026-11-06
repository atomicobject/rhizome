package ontology

import (
	"regexp"
	"strings"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

var previewPlaceholder = regexp.MustCompile(`{{\s*([A-Za-z_][A-Za-z0-9_]*)\s*}}`)

// IdentifierFieldName selects the preferred identifier, then the first identifier.
func IdentifierFieldName(noteType *ontoschema.NoteType) string {
	if noteType == nil {
		return ""
	}
	if field := noteType.PreferredIdentifierField(); field != nil {
		return field.Name
	}
	for _, field := range noteType.Fields {
		if field != nil && field.IsIdentifier {
			return field.Name
		}
	}
	return ""
}

// InterpolatePreviewTemplate expands title and property placeholders. The caller
// chooses the replacement for a missing property's original placeholder.
func InterpolatePreviewTemplate(template string, properties map[string]string, title string, missing func(string) string) string {
	return previewPlaceholder.ReplaceAllStringFunc(template, func(placeholder string) string {
		name := strings.TrimSpace(previewPlaceholder.FindStringSubmatch(placeholder)[1])
		if name == "title" {
			return title
		}
		if value, ok := properties[name]; ok {
			return value
		}
		return missing(placeholder)
	})
}
