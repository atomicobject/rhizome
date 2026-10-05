package ontology

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

// TypeDoc is a lightweight reference-oriented projection of a compiled ontology type.
// It backs inspect payloads and schema-reference renderers; Schema remains the source of truth.
type TypeDoc struct {
	Name              string                    `json:"name"`
	Summary           string                    `json:"summary,omitempty"`
	Meaning           string                    `json:"meaning,omitempty"`
	Authoring         string                    `json:"authoring,omitempty"`
	AgentImplications string                    `json:"agentImplications,omitempty"`
	Description       string                    `json:"description,omitempty"`
	Role              TypeRole                  `json:"role,omitempty"`
	Locator           string                    `json:"locator,omitempty"`
	Implements        []string                  `json:"implements,omitempty"`
	Label             string                    `json:"label,omitempty"`
	PluralLabel       string                    `json:"pluralLabel,omitempty"`
	DisplayGroup      string                    `json:"displayGroup,omitempty"`
	DisplayParent     string                    `json:"displayParent,omitempty"`
	Color             string                    `json:"color,omitempty"`
	KeyField          string                    `json:"keyField,omitempty"`
	PropertyCase      PropertyCase              `json:"propertyCase,omitempty"`
	Semantics         SemanticsKind             `json:"semantics,omitempty"`
	Paths             []string                  `json:"paths,omitempty"`
	Matches           []string                  `json:"matches,omitempty"`
	CompanionDocs     []CompanionDocRef         `json:"companionDocs,omitempty"`
	Annotations       map[string]map[string]any `json:"annotations,omitempty"`
	Enums             []EnumDoc                 `json:"enums,omitempty"`
	Fields            []FieldDoc                `json:"fields,omitempty"`
	// SummaryField names the effective summary field: the SUMMARY display
	// role, else the conventional field named summary.
	SummaryField string `json:"summaryField,omitempty"`
	// ParentField names the field carrying the PARENT display role.
	ParentField string `json:"parentField,omitempty"`
	// Profile is the schema-derived view profile of a note type or
	// interface, with people fields linking to identity.CurrentUserType.
	Profile *TypeProfile `json:"profile,omitempty"`
}

type FieldDoc struct {
	Name                 string                    `json:"name"`
	Summary              string                    `json:"summary,omitempty"`
	Meaning              string                    `json:"meaning,omitempty"`
	Authoring            string                    `json:"authoring,omitempty"`
	AgentImplications    string                    `json:"agentImplications,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Kind                 FieldKind                 `json:"kind"`
	TypeName             string                    `json:"typeName"`
	Required             bool                      `json:"required"`
	List                 bool                      `json:"list"`
	Source               string                    `json:"source,omitempty"`
	SourceAliases        []string                  `json:"sourceAliases,omitempty"`
	SourceKind           FieldSource               `json:"sourceKind,omitempty"`
	Inverse              string                    `json:"inverse,omitempty"`
	ReverseField         string                    `json:"reverseField,omitempty"`
	Direction            NeighborDirection         `json:"direction,omitempty"`
	Scope                NeighborScope             `json:"scope,omitempty"`
	IncludeBodyLinks     bool                      `json:"includeBodyLinks,omitempty"`
	IncludeBacklinks     bool                      `json:"includeBacklinks,omitempty"`
	WorkspaceMember      bool                      `json:"workspaceMember,omitempty"`
	WorkspaceMemberLabel string                    `json:"workspaceMemberLabel,omitempty"`
	Identifier           bool                      `json:"identifier,omitempty"`
	PreferredIdentifier  bool                      `json:"preferredIdentifier,omitempty"`
	IdentifierFormat     *IdentifierFormat         `json:"identifierFormat,omitempty"`
	SectionHeading       string                    `json:"sectionHeading,omitempty"`
	SectionLevel         SectionLevel              `json:"sectionLevel,omitempty"`
	SectionRequired      bool                      `json:"sectionRequired,omitempty"`
	SectionDisplay       SectionDisplay            `json:"sectionDisplay,omitempty"`
	EmbeddedSourceShape  EmbeddedSourceShape       `json:"embeddedSourceShape,omitempty"`
	EmbeddedSourceMarker string                    `json:"embeddedSourceMarker,omitempty"`
	Semantics            SemanticsKind             `json:"semantics,omitempty"`
	EnumValues           []string                  `json:"enumValues,omitempty"`
	Enum                 *EnumDoc                  `json:"enum,omitempty"`
	CompanionDocs        []CompanionDocRef         `json:"companionDocs,omitempty"`
	Policy               *PolicyHint               `json:"policy,omitempty"`
	Display              *FieldDisplay             `json:"display,omitempty"`
	Annotations          map[string]map[string]any `json:"annotations,omitempty"`
	// RequiredWhen lists the @requiresWhen conditions under which this field
	// becomes required.
	RequiredWhen []FieldCondition `json:"requiredWhen,omitempty"`
}

// FieldCondition is one field-equals-value condition.
type FieldCondition struct {
	Field  string `json:"field"`
	Equals string `json:"equals"`
}

type EnumDoc struct {
	Name              string         `json:"name"`
	Summary           string         `json:"summary,omitempty"`
	Meaning           string         `json:"meaning,omitempty"`
	Authoring         string         `json:"authoring,omitempty"`
	AgentImplications string         `json:"agentImplications,omitempty"`
	Description       string         `json:"description,omitempty"`
	Values            []EnumValueDoc `json:"values,omitempty"`
}

type EnumValueDoc struct {
	Name              string      `json:"name"`
	Summary           string      `json:"summary,omitempty"`
	Meaning           string      `json:"meaning,omitempty"`
	Authoring         string      `json:"authoring,omitempty"`
	AgentImplications string      `json:"agentImplications,omitempty"`
	Description       string      `json:"description,omitempty"`
	Policy            *PolicyHint `json:"policy,omitempty"`
	// Label and Order come from @view on the value and are omitted when the
	// author did not set them. Tone and Collapsed are the authored values,
	// else the defaults of a declared stage (SPEC-0112), and are omitted when
	// neither applies; inferred stages never set them.
	Label     string `json:"label,omitempty"`
	Order     *int   `json:"order,omitempty"`
	Tone      string `json:"tone,omitempty"`
	Collapsed *bool  `json:"collapsed,omitempty"`
	// Stage is the value's declared or inferred lifecycle stage;
	// StageDeclared is false when it was inferred.
	Stage         LifecycleStage `json:"stage,omitempty"`
	StageDeclared bool           `json:"stageDeclared,omitempty"`
}

func SchemaDocs(schema *Schema, typeName string) ([]TypeDoc, error) {
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
			role          TypeRole
			locator       string
			description   string
			guidance      *Guidance
			label         string
			pluralLabel   string
			displayGroup  string
			displayParent string
			color         string
			keyField      string
			propertyCase  PropertyCase
			paths         []string
			matches       []string
			implements    []string
			companionDocs []CompanionDocRef
			annotations   map[string]map[string]any
			fieldsSrc     []*Field
			semantics     SemanticsKind
			requiresWhen  []ConditionalRequirement
		)
		if name == "Section" {
			role = TypeRoleInterface
			description = "Synthetic heading-derived section node inside a note body. Query sections through the owning note or parent section; sections do not have standalone roots."
			guidance = &Guidance{Summary: description}
			fieldsSrc = builtinSectionFields()
		} else if noteType := schema.Types[name]; noteType != nil {
			role = noteType.Role
			switch noteType.Role {
			case TypeRoleNote:
				locator = "FILE"
			case TypeRoleEmbeddedNode:
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
			companionDocs = append([]CompanionDocRef(nil), noteType.CompanionDocs...)
			annotations = cloneAnnotations(noteType.Annotations)
			fieldsSrc = noteType.Fields
			semantics = noteType.Semantics
			requiresWhen = noteType.RequiresWhen
		} else if iface := schema.Interfaces[name]; iface != nil {
			role = TypeRoleInterface
			description = iface.Description
			guidance = iface.Guidance
			label = iface.Label
			pluralLabel = iface.PluralLabel
			displayGroup = iface.DisplayGroup
			displayParent = iface.DisplayParent
			implements = append([]string(nil), iface.Implements...)
			companionDocs = append([]CompanionDocRef(nil), iface.CompanionDocs...)
			annotations = cloneAnnotations(iface.Annotations)
			fieldsSrc = iface.Fields
			semantics = iface.Semantics
		}
		summaryField := SummaryField(fieldsSrc)
		fields := make([]FieldDoc, 0, len(fieldsSrc))
		for _, field := range fieldsSrc {
			var identifierFormat *IdentifierFormat
			if field.IdentifierFormat != nil {
				metadata := *field.IdentifierFormat
				identifierFormat = &metadata
			}
			metadata := field.Display
			metadata.Importance = metadata.EffectiveImportance()
			if field == summaryField && metadata.Role == FieldDisplayRoleNone {
				metadata.Role = FieldDisplayRoleSummary
			}
			var display *FieldDisplay
			if metadata.Role != FieldDisplayRoleNone || metadata.Importance != FieldImportanceNormal || metadata.HideHover {
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
				CompanionDocs:        append([]CompanionDocRef(nil), field.CompanionDocs...),
				Policy:               clonePolicy(field.Policy),
				Display:              display,
				Annotations:          cloneAnnotations(field.Annotations),
				RequiredWhen:         requiredWhen(requiresWhen, field.Name),
			})
		}
		var parentField string
		if field := ParentField(fieldsSrc); field != nil {
			parentField = field.Name
		}
		var summaryFieldName string
		if summaryField != nil {
			summaryFieldName = summaryField.Name
		}
		var profile *TypeProfile
		if derived, ok := DeriveTypeProfile(schema, name, identity.CurrentUserType); ok {
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

func schemaUsesSections(schema *Schema) bool {
	if schema == nil {
		return false
	}
	for _, noteType := range schema.Types {
		if noteType == nil {
			continue
		}
		if noteType.Role == TypeRoleSection || noteType.Role == TypeRoleEmbeddedNode {
			return true
		}
		for _, field := range noteType.Fields {
			if field != nil && field.Kind == FieldKindSection {
				return true
			}
		}
	}
	return false
}

func enumValues(schema *Schema, typeName string) []string {
	enumType := enumType(schema, typeName)
	if enumType == nil {
		return nil
	}
	out := make([]string, 0, len(enumType.Values))
	for _, value := range enumType.Values {
		if value == nil {
			continue
		}
		out = append(out, value.Name)
	}
	return out
}

func enumDoc(schema *Schema, typeName string) *EnumDoc {
	enumType := enumType(schema, typeName)
	if enumType == nil {
		return nil
	}
	doc := &EnumDoc{
		Name:              enumType.Name,
		Summary:           guidanceSummary(enumType.Guidance, enumType.Description),
		Meaning:           guidanceMeaning(enumType.Guidance),
		Authoring:         guidanceAuthoring(enumType.Guidance),
		AgentImplications: guidanceAgentImplications(enumType.Guidance),
		Description:       enumType.Description,
		Values:            make([]EnumValueDoc, 0, len(enumType.Values)),
	}
	stages := enumType.Stages()
	for index, value := range enumType.Values {
		if value == nil {
			continue
		}
		valueDoc := EnumValueDoc{
			Name:              value.Name,
			Summary:           guidanceSummary(value.Guidance, value.Description),
			Meaning:           guidanceMeaning(value.Guidance),
			Authoring:         guidanceAuthoring(value.Guidance),
			AgentImplications: guidanceAgentImplications(value.Guidance),
			Description:       value.Description,
			Policy:            clonePolicy(value.Policy),
			Label:             value.View.Label,
			Order:             enumValueOrder(value.View),
			Tone:              value.View.Tone,
			Collapsed:         cloneBool(value.View.Collapsed),
		}
		if valueDoc.Tone == "" {
			valueDoc.Tone = stageDefaultTones[value.View.Stage]
		}
		if valueDoc.Collapsed == nil && value.View.Stage == StageDropped {
			collapsed := true
			valueDoc.Collapsed = &collapsed
		}
		if stages != nil {
			valueDoc.Stage, valueDoc.StageDeclared = stages[index].Stage, stages[index].Declared
		}
		doc.Values = append(doc.Values, valueDoc)
	}
	return doc
}

func enumValueOrder(view EnumValueView) *int {
	if !view.orderSet {
		return nil
	}
	order := view.Order
	return &order
}

func cloneBool(in *bool) *bool {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// requiredWhen lists the @requiresWhen conditions that make fieldName
// required. A rule that also requires a specific value still makes the field
// required, so it is listed too.
func requiredWhen(rules []ConditionalRequirement, fieldName string) []FieldCondition {
	var out []FieldCondition
	for _, rule := range rules {
		for _, required := range rule.Require {
			if required.Field == fieldName {
				out = append(out, FieldCondition{Field: rule.Field, Equals: rule.Equals})
				break
			}
		}
	}
	return out
}

func fieldEnumDocs(schema *Schema, fields []*Field) []EnumDoc {
	if schema == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []EnumDoc
	for _, field := range fields {
		if field == nil {
			continue
		}
		if _, ok := seen[field.TypeName]; ok {
			continue
		}
		doc := enumDoc(schema, field.TypeName)
		if doc == nil {
			continue
		}
		seen[field.TypeName] = struct{}{}
		out = append(out, *doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func guidanceSummary(guidance *Guidance, fallback string) string {
	if guidance != nil && strings.TrimSpace(guidance.Summary) != "" {
		return strings.TrimSpace(guidance.Summary)
	}
	return strings.TrimSpace(fallback)
}

func guidanceMeaning(guidance *Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.Meaning)
}

func guidanceAuthoring(guidance *Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.Authoring)
}

func guidanceAgentImplications(guidance *Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.AgentImplications)
}

func clonePolicy(in *PolicyHint) *PolicyHint {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func cloneAnnotations(in map[string]map[string]any) map[string]map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for key, values := range in {
		if len(values) == 0 {
			out[key] = map[string]any{}
			continue
		}
		cloned := make(map[string]any, len(values))
		for valueKey, value := range values {
			cloned[valueKey] = value
		}
		out[key] = cloned
	}
	return out
}
