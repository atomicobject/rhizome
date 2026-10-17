package schemadoc

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

// TypeDoc is a lightweight reference-oriented projection of a compiled ontology type.
// It backs inspect payloads and schema-reference renderers; Schema remains the source of truth.
type TypeDoc struct {
	Name              string                       `json:"name"`
	Summary           string                       `json:"summary,omitempty"`
	Meaning           string                       `json:"meaning,omitempty"`
	Authoring         string                       `json:"authoring,omitempty"`
	AgentImplications string                       `json:"agentImplications,omitempty"`
	Description       string                       `json:"description,omitempty"`
	Role              ontoschema.TypeRole          `json:"role,omitempty"`
	Locator           string                       `json:"locator,omitempty"`
	Implements        []string                     `json:"implements,omitempty"`
	Label             string                       `json:"label,omitempty"`
	PluralLabel       string                       `json:"pluralLabel,omitempty"`
	DisplayGroup      string                       `json:"displayGroup,omitempty"`
	DisplayParent     string                       `json:"displayParent,omitempty"`
	Color             string                       `json:"color,omitempty"`
	KeyField          string                       `json:"keyField,omitempty"`
	PropertyCase      ontoschema.PropertyCase      `json:"propertyCase,omitempty"`
	Semantics         ontoschema.SemanticsKind     `json:"semantics,omitempty"`
	Paths             []string                     `json:"paths,omitempty"`
	Matches           []string                     `json:"matches,omitempty"`
	CompanionDocs     []ontoschema.CompanionDocRef `json:"companionDocs,omitempty"`
	Annotations       map[string]map[string]any    `json:"annotations,omitempty"`
	Enums             []EnumDoc                    `json:"enums,omitempty"`
	Fields            []FieldDoc                   `json:"fields,omitempty"`
	// SummaryField names the effective summary field: the SUMMARY display
	// role, else the conventional field named summary.
	SummaryField string `json:"summaryField,omitempty"`
	// ParentField names the field carrying the PARENT display role.
	ParentField string `json:"parentField,omitempty"`
	// Profile is the schema-derived view profile of a note type or
	// interface, with people fields linking to identity.CurrentUserType.
	Profile *ontoschema.TypeProfile `json:"profile,omitempty"`
}

type FieldDoc struct {
	Name                 string                         `json:"name"`
	Summary              string                         `json:"summary,omitempty"`
	Meaning              string                         `json:"meaning,omitempty"`
	Authoring            string                         `json:"authoring,omitempty"`
	AgentImplications    string                         `json:"agentImplications,omitempty"`
	Description          string                         `json:"description,omitempty"`
	Kind                 ontoschema.FieldKind           `json:"kind"`
	TypeName             string                         `json:"typeName"`
	Required             bool                           `json:"required"`
	List                 bool                           `json:"list"`
	Source               string                         `json:"source,omitempty"`
	SourceAliases        []string                       `json:"sourceAliases,omitempty"`
	SourceKind           ontoschema.FieldSource         `json:"sourceKind,omitempty"`
	Inverse              string                         `json:"inverse,omitempty"`
	ReverseField         string                         `json:"reverseField,omitempty"`
	Direction            ontoschema.NeighborDirection   `json:"direction,omitempty"`
	Scope                ontoschema.NeighborScope       `json:"scope,omitempty"`
	IncludeBodyLinks     bool                           `json:"includeBodyLinks,omitempty"`
	IncludeBacklinks     bool                           `json:"includeBacklinks,omitempty"`
	WorkspaceMember      bool                           `json:"workspaceMember,omitempty"`
	WorkspaceMemberLabel string                         `json:"workspaceMemberLabel,omitempty"`
	Identifier           bool                           `json:"identifier,omitempty"`
	PreferredIdentifier  bool                           `json:"preferredIdentifier,omitempty"`
	IdentifierFormat     *ontoschema.IdentifierFormat   `json:"identifierFormat,omitempty"`
	SectionHeading       string                         `json:"sectionHeading,omitempty"`
	SectionLevel         ontoschema.SectionLevel        `json:"sectionLevel,omitempty"`
	SectionRequired      bool                           `json:"sectionRequired,omitempty"`
	SectionDisplay       ontoschema.SectionDisplay      `json:"sectionDisplay,omitempty"`
	EmbeddedSourceShape  ontoschema.EmbeddedSourceShape `json:"embeddedSourceShape,omitempty"`
	EmbeddedSourceMarker string                         `json:"embeddedSourceMarker,omitempty"`
	Semantics            ontoschema.SemanticsKind       `json:"semantics,omitempty"`
	EnumValues           []string                       `json:"enumValues,omitempty"`
	Enum                 *EnumDoc                       `json:"enum,omitempty"`
	CompanionDocs        []ontoschema.CompanionDocRef   `json:"companionDocs,omitempty"`
	Policy               *ontoschema.PolicyHint         `json:"policy,omitempty"`
	Display              *ontoschema.FieldDisplay       `json:"display,omitempty"`
	Annotations          map[string]map[string]any      `json:"annotations,omitempty"`
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
	Name              string                 `json:"name"`
	Summary           string                 `json:"summary,omitempty"`
	Meaning           string                 `json:"meaning,omitempty"`
	Authoring         string                 `json:"authoring,omitempty"`
	AgentImplications string                 `json:"agentImplications,omitempty"`
	Description       string                 `json:"description,omitempty"`
	Policy            *ontoschema.PolicyHint `json:"policy,omitempty"`
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
	Stage         ontoschema.LifecycleStage `json:"stage,omitempty"`
	StageDeclared bool                      `json:"stageDeclared,omitempty"`
}
