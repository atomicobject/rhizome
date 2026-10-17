package schemadoc

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
)

type guideType struct {
	preferredIdentifier *ontoschema.Field
	Name                string
	Description         string
	Summary             string
	Meaning             string
	Authoring           string
	AgentImplications   string
	Role                ontoschema.TypeRole
	Implements          []string
	PropertyCase        ontoschema.PropertyCase
	Paths               []string
	Matches             []string
	Fields              []*ontoschema.Field
	Semantics           ontoschema.SemanticsKind
	CompanionDocs       []ontoschema.CompanionDocRef
	Annotations         map[string]map[string]any
	SourceShape         ontoschema.EmbeddedSourceShape
	SourceMarker        string
	SourcePaths         []string
}

func selectTypes(schema *ontoschema.Schema, requested []string) ([]string, []string, error) {
	if len(requested) == 0 {
		names := make([]string, 0, len(schema.Types))
		for name, noteType := range schema.Types {
			if ontologyTypeRole(noteType) != ontoschema.TypeRoleNote {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil, nil
	}

	seen := make(map[string]struct{}, len(requested))
	primary := make([]string, 0, len(requested))
	for _, raw := range requested {
		typeName := strings.TrimSpace(raw)
		if typeName == "" {
			continue
		}
		if guideTypeFromSchema(schema, typeName) == nil {
			return nil, nil, fmt.Errorf("ontology type %q not found", typeName)
		}
		if _, ok := seen[typeName]; ok {
			continue
		}
		seen[typeName] = struct{}{}
		primary = append(primary, typeName)
	}

	supportingSeen := make(map[string]struct{})
	supporting := make([]string, 0)
	for _, typeName := range primary {
		for _, related := range supportingTypes(schema, typeName) {
			if _, ok := seen[related]; ok {
				continue
			}
			if _, ok := supportingSeen[related]; ok {
				continue
			}
			supportingSeen[related] = struct{}{}
			supporting = append(supporting, related)
		}
	}
	sort.Strings(supporting)
	return primary, supporting, nil
}

func supportingTypes(schema *ontoschema.Schema, typeName string) []string {
	current := guideTypeFromSchema(schema, typeName)
	if current == nil {
		return nil
	}
	seen := make(map[string]struct{})
	add := func(name string) {
		if name == "" || name == typeName || guideTypeFromSchema(schema, name) == nil {
			return
		}
		seen[name] = struct{}{}
	}
	for _, name := range current.Implements {
		add(name)
	}
	for _, field := range current.Fields {
		if (field.Kind == ontoschema.FieldKindLink || field.Kind == ontoschema.FieldKindSection) && field.TypeName != "Note" {
			add(field.TypeName)
		}
	}
	if current.Role == ontoschema.TypeRoleInterface {
		for candidateName, candidate := range schema.Types {
			if candidate == nil {
				continue
			}
			for _, name := range candidate.Implements {
				if name == typeName {
					add(candidateName)
					break
				}
			}
		}
	}
	return ontologyValues(seen)
}

func relatedTypes(schema *ontoschema.Schema, typeName string) []string {
	current := guideTypeFromSchema(schema, typeName)
	if current == nil {
		return nil
	}
	seen := supportingSet(supportingTypes(schema, typeName))
	add := func(name string) {
		if name == "" || name == typeName || guideTypeFromSchema(schema, name) == nil {
			return
		}
		seen[name] = struct{}{}
	}
	for _, field := range current.Fields {
		if (field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse) && field.TypeName != "Note" {
			add(field.TypeName)
		}
	}
	if current.Role == ontoschema.TypeRoleNote {
		for candidateName, candidate := range schema.Types {
			for _, field := range candidate.Fields {
				if field.Kind != ontoschema.FieldKindLink || field.Inverse == "" || field.TypeName != typeName {
					continue
				}
				add(candidateName)
			}
		}
	}
	return ontologyValues(seen)
}

func ontologyTypeRole(noteType *ontoschema.NoteType) ontoschema.TypeRole {
	if noteType == nil {
		return ""
	}
	if noteType.Role == "" {
		return ontoschema.TypeRoleNote
	}
	return noteType.Role
}

func guideTypeFromSchema(schema *ontoschema.Schema, name string) *guideType {
	if schema == nil {
		return nil
	}
	if noteType := schema.Types[name]; noteType != nil {
		return &guideType{
			Name:                noteType.Name,
			Description:         noteType.Description,
			Summary:             guideSummary(noteType.Guidance, noteType.Description),
			Meaning:             guideMeaning(noteType.Guidance),
			Authoring:           guideAuthoring(noteType.Guidance),
			AgentImplications:   guideAgentImplications(noteType.Guidance),
			Role:                ontologyTypeRole(noteType),
			Implements:          append([]string(nil), noteType.Implements...),
			PropertyCase:        noteType.PropertyCase,
			Paths:               append([]string(nil), noteType.Paths...),
			Matches:             append([]string(nil), noteType.Matches...),
			Fields:              noteType.Fields,
			preferredIdentifier: noteType.PreferredIdentifierField(),
			Semantics:           noteType.Semantics,
			CompanionDocs:       append([]ontoschema.CompanionDocRef(nil), noteType.CompanionDocs...),
			Annotations:         noteType.Annotations,
			SourceShape:         noteType.SourceShape,
			SourceMarker:        noteType.SourceMarker,
			SourcePaths:         append([]string(nil), noteType.SourcePaths...),
		}
	}
	if iface := schema.Interfaces[name]; iface != nil {
		return &guideType{
			Name:                iface.Name,
			Description:         iface.Description,
			Summary:             guideSummary(iface.Guidance, iface.Description),
			Meaning:             guideMeaning(iface.Guidance),
			Authoring:           guideAuthoring(iface.Guidance),
			AgentImplications:   guideAgentImplications(iface.Guidance),
			Role:                ontoschema.TypeRoleInterface,
			Implements:          append([]string(nil), iface.Implements...),
			Fields:              iface.Fields,
			preferredIdentifier: (&ontoschema.NoteType{Fields: iface.Fields}).PreferredIdentifierField(),
			Semantics:           iface.Semantics,
			CompanionDocs:       append([]ontoschema.CompanionDocRef(nil), iface.CompanionDocs...),
			Annotations:         iface.Annotations,
		}
	}
	return nil
}

func supportingSet(supporting []string) map[string]struct{} {
	out := make(map[string]struct{}, len(supporting))
	for _, name := range supporting {
		out[name] = struct{}{}
	}
	return out
}
