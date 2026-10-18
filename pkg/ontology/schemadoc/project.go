package schemadoc

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
)

func SchemaDocs(schema *ontoschema.Schema, typeName string, personType string) ([]TypeDoc, error) {
	if schema == nil {
		return nil, fmt.Errorf("ontology schema is required")
	}
	names := make([]string, 0, len(schema.Types))
	if strings.TrimSpace(typeName) != "" {
		if noteType := schema.Types[typeName]; noteType != nil {
			names = append(names, noteType.Name)
		} else if iface := schema.Interfaces[typeName]; iface != nil {
			names = append(names, iface.Name)
		} else if typeName == "Section" && schemaUsesSections(schema) {
			names = append(names, typeName)
		} else {
			return nil, fmt.Errorf("ontology type %q not found", typeName)
		}
	} else {
		for name := range schema.Types {
			names = append(names, name)
		}
		for name := range schema.Interfaces {
			names = append(names, name)
		}
		if schemaUsesSections(schema) {
			names = append(names, "Section")
		}
		sort.Strings(names)
	}

	out := make([]TypeDoc, 0, len(names))
	for _, name := range names {
		var (
			role          ontoschema.TypeRole
			locator       string
			description   string
			guidance      *ontoschema.Guidance
			label         string
			pluralLabel   string
			displayGroup  string
			displayParent string
			color         string
			keyField      string
			propertyCase  ontoschema.PropertyCase
			paths         []string
			matches       []string
			implements    []string
			companionDocs []ontoschema.CompanionDocRef
			annotations   map[string]map[string]any
			fieldsSrc     []*ontoschema.Field
			semantics     ontoschema.SemanticsKind
			requiresWhen  []ontoschema.ConditionalRequirement
		)
		if name == "Section" {
			role = ontoschema.TypeRoleInterface
			description = "Synthetic heading-derived section node inside a note body. Query sections through the owning note or parent section; sections do not have standalone roots."
			guidance = &ontoschema.Guidance{Summary: description}
			fieldsSrc = ontoschema.BuiltinSectionFields()
		} else if noteType := schema.Types[name]; noteType != nil {
			role = noteType.Role
			switch noteType.Role {
			case ontoschema.TypeRoleNote:
				locator = "FILE"
			case ontoschema.TypeRoleEmbeddedNode:
				locator = "EMBEDDED"
			}
			description = noteType.Description
			guidance = noteType.Guidance
			label = noteType.Label
			pluralLabel = noteType.PluralLabel
			displayGroup = noteType.DisplayGroup
			displayParent = noteType.DisplayParent
			color = noteType.Color
			keyField = noteType.KeyField
			propertyCase = noteType.PropertyCase
			paths = append([]string(nil), noteType.Paths...)
			matches = append([]string(nil), noteType.Matches...)
			implements = append([]string(nil), noteType.Implements...)
			companionDocs = append([]ontoschema.CompanionDocRef(nil), noteType.CompanionDocs...)
			annotations = cloneAnnotations(noteType.Annotations)
			fieldsSrc = noteType.Fields
			semantics = noteType.Semantics
			requiresWhen = noteType.RequiresWhen
		} else if iface := schema.Interfaces[name]; iface != nil {
			role = ontoschema.TypeRoleInterface
			description = iface.Description
			guidance = iface.Guidance
			label = iface.Label
			pluralLabel = iface.PluralLabel
			displayGroup = iface.DisplayGroup
			displayParent = iface.DisplayParent
			implements = append([]string(nil), iface.Implements...)
			companionDocs = append([]ontoschema.CompanionDocRef(nil), iface.CompanionDocs...)
			annotations = cloneAnnotations(iface.Annotations)
			fieldsSrc = iface.Fields
			semantics = iface.Semantics
		}
		summaryField := ontoschema.SummaryField(fieldsSrc)
		fields := make([]FieldDoc, 0, len(fieldsSrc))
		for _, field := range fieldsSrc {
			var identifierFormat *ontoschema.IdentifierFormat
			if field.IdentifierFormat != nil {
				metadata := *field.IdentifierFormat
				identifierFormat = &metadata
			}
			metadata := field.Display
			metadata.Importance = metadata.EffectiveImportance()
			if field == summaryField && metadata.Role == ontoschema.FieldDisplayRoleNone {
				metadata.Role = ontoschema.FieldDisplayRoleSummary
			}
			var display *ontoschema.FieldDisplay
			if metadata.Role != ontoschema.FieldDisplayRoleNone || metadata.Importance != ontoschema.FieldImportanceNormal || metadata.HideHover {
				display = &metadata
			}
			fields = append(fields, FieldDoc{
				Name:                 field.Name,
				Summary:              guidanceSummary(field.Guidance, field.Description),
				Meaning:              guidanceMeaning(field.Guidance),
				Authoring:            guidanceAuthoring(field.Guidance),
				AgentImplications:    guidanceAgentImplications(field.Guidance),
				Description:          field.Description,
				Kind:                 field.Kind,
				TypeName:             field.TypeName,
				Required:             field.Required,
				List:                 field.List,
				Source:               field.Source,
				SourceAliases:        append([]string(nil), field.SourceAliases...),
				SourceKind:           field.SourceKind,
				Inverse:              field.Inverse,
				ReverseField:         field.ReverseField,
				Direction:            field.Direction,
				Scope:                field.Scope,
				IncludeBodyLinks:     field.IncludeBodyLinks,
				IncludeBacklinks:     field.IncludeBacklinks,
				WorkspaceMember:      field.WorkspaceMember,
				WorkspaceMemberLabel: field.WorkspaceMemberLabel,
				Identifier:           field.IsIdentifier,
				PreferredIdentifier:  field.IsPreferredIdentifier,
				IdentifierFormat:     identifierFormat,
				SectionHeading:       field.SectionHeading,
				SectionLevel:         field.SectionLevel,
				SectionRequired:      field.SectionRequired,
				SectionDisplay:       field.SectionDisplay,
				EmbeddedSourceShape:  field.EmbeddedSourceShape,
				EmbeddedSourceMarker: field.EmbeddedSourceMarker,
				Semantics:            field.Semantics,
				EnumValues:           enumValues(schema, field.TypeName),
				Enum:                 enumDoc(schema, field.TypeName),
				CompanionDocs:        append([]ontoschema.CompanionDocRef(nil), field.CompanionDocs...),
				Policy:               clonePolicy(field.Policy),
				Display:              display,
				Annotations:          cloneAnnotations(field.Annotations),
				RequiredWhen:         requiredWhen(requiresWhen, field.Name),
			})
		}
		var parentField string
		if field := ontoschema.ParentField(fieldsSrc); field != nil {
			parentField = field.Name
		}
		var summaryFieldName string
		if summaryField != nil {
			summaryFieldName = summaryField.Name
		}
		var profile *ontoschema.TypeProfile
		if derived, ok := ontoschema.DeriveTypeProfile(schema, name, personType); ok {
			profile = &derived
		}
		out = append(out, TypeDoc{
			Profile:           profile,
			Name:              name,
			Summary:           guidanceSummary(guidance, description),
			Meaning:           guidanceMeaning(guidance),
			Authoring:         guidanceAuthoring(guidance),
			AgentImplications: guidanceAgentImplications(guidance),
			Description:       description,
			Role:              role,
			Locator:           locator,
			Implements:        implements,
			Label:             label,
			PluralLabel:       pluralLabel,
			DisplayGroup:      displayGroup,
			DisplayParent:     displayParent,
			Color:             color,
			KeyField:          keyField,
			PropertyCase:      propertyCase,
			Semantics:         semantics,
			Paths:             paths,
			Matches:           matches,
			CompanionDocs:     companionDocs,
			Annotations:       annotations,
			Enums:             fieldEnumDocs(schema, fieldsSrc),
			Fields:            fields,
			SummaryField:      summaryFieldName,
			ParentField:       parentField,
		})
	}
	return out, nil
}

func schemaUsesSections(schema *ontoschema.Schema) bool {
	if schema == nil {
		return false
	}
	for _, noteType := range schema.Types {
		if noteType == nil {
			continue
		}
		if noteType.Role == ontoschema.TypeRoleSection || noteType.Role == ontoschema.TypeRoleEmbeddedNode {
			return true
		}
		for _, field := range noteType.Fields {
			if field != nil && field.Kind == ontoschema.FieldKindSection {
				return true
			}
		}
	}
	return false
}
