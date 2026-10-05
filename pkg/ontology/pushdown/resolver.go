package pushdown

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Resolver maps a requested field key to a schema field. Two implementations
// ship with the package:
//
//   - SchemaResolver: graphql-style canonical names only (case-sensitive).
//   - ViewResolver: view-style with frontmatter./inline. prefixes and
//     case-insensitive matching.
type Resolver interface {
	Resolve(key string) (FieldKey, *ontology.Field, bool)
}

// IsBuiltinFieldKey reports whether the key is the notePath/path builtin.
func IsBuiltinFieldKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "notePath", "path":
		return true
	default:
		return false
	}
}

// SchemaResolver matches the canonical schema field name only. Used by
// public GraphQL roots where clients pass schema field names directly.
type SchemaResolver struct {
	NoteType *ontology.NoteType
}

func (r SchemaResolver) Resolve(key string) (FieldKey, *ontology.Field, bool) {
	key = strings.TrimSpace(key)
	if IsBuiltinFieldKey(key) {
		return FieldKey{Requested: key, Canonical: key, Source: FieldKeyBuiltin}, nil, true
	}
	if r.NoteType == nil {
		return FieldKey{Requested: key}, nil, false
	}
	field, ok := r.NoteType.ByName[key]
	if !ok || field == nil {
		return FieldKey{Requested: key}, nil, false
	}
	return FieldKey{Requested: key, Canonical: field.Name, Source: FieldKeySchema}, field, true
}

// ViewResolver matches view-authored keys, including frontmatter./inline.
// prefixes and case-insensitive field matching. Used by configured views
// where authors write the keys.
type ViewResolver struct {
	NoteType *ontology.NoteType
}

func (r ViewResolver) Resolve(key string) (FieldKey, *ontology.Field, bool) {
	key = strings.TrimSpace(key)
	if IsBuiltinFieldKey(key) {
		return FieldKey{Requested: key, Canonical: key, Source: FieldKeyBuiltin}, nil, true
	}
	if r.NoteType == nil || key == "" {
		return FieldKey{Requested: key}, nil, false
	}
	var sourceKind ontology.FieldSource
	source := FieldKeySchema
	sourceName := key
	switch {
	case strings.HasPrefix(key, "frontmatter."):
		sourceKind = ontology.FieldSourceFrontmatter
		source = FieldKeyFrontmatter
		sourceName = strings.TrimPrefix(key, "frontmatter.")
	case strings.HasPrefix(key, "inline."):
		sourceKind = ontology.FieldSourceInline
		source = FieldKeyInline
		sourceName = strings.TrimPrefix(key, "inline.")
	}
	if sourceKind == "" {
		// No prefix: try canonical lookup, then case-insensitive scan.
		if field, ok := r.NoteType.ByName[key]; ok && field != nil {
			return FieldKey{Requested: key, Canonical: field.Name, Source: FieldKeySchema}, field, true
		}
		for _, field := range r.NoteType.Fields {
			if field == nil {
				continue
			}
			if strings.EqualFold(field.Name, key) {
				return FieldKey{Requested: key, Canonical: field.Name, Source: FieldKeySchema}, field, true
			}
		}
		return FieldKey{Requested: key}, nil, false
	}
	for _, field := range r.NoteType.Fields {
		if field == nil || field.SourceKind != sourceKind {
			continue
		}
		for _, srcName := range ontology.FieldSourceNames(field) {
			if strings.EqualFold(srcName, sourceName) {
				return FieldKey{Requested: key, Canonical: field.Name, Source: source}, field, true
			}
		}
		if strings.EqualFold(field.Name, sourceName) {
			return FieldKey{Requested: key, Canonical: field.Name, Source: source}, field, true
		}
	}
	return FieldKey{Requested: key}, nil, false
}
