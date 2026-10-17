package schema

import (
	"regexp"

	"github.com/vektah/gqlparser/v2/ast"
)

type FieldSource string

const (
	FieldSourceFrontmatter FieldSource = "FRONTMATTER"
	FieldSourceInline      FieldSource = "INLINE"
	FieldSourceCheckbox    FieldSource = "CHECKBOX"
	FieldSourceItemTitle   FieldSource = "ITEM_TITLE"
	FieldSourceItemSummary FieldSource = "ITEM_SUMMARY"
	FieldSourceItemDetail  FieldSource = "ITEM_DETAIL"
)

type EmbeddedSourceShape string

const (
	EmbeddedSourceShapeSection      EmbeddedSourceShape = "SECTION"
	EmbeddedSourceShapeListItem     EmbeddedSourceShape = "LIST_ITEM"
	EmbeddedSourceShapeCheckboxItem EmbeddedSourceShape = "CHECKBOX_ITEM"
)

type FieldKind string

const (
	FieldKindScalar   FieldKind = "scalar"
	FieldKindEnum     FieldKind = "enum"
	FieldKindLink     FieldKind = "link"
	FieldKindReverse  FieldKind = "reverse"
	FieldKindNeighbor FieldKind = "neighbor"
	FieldKindSection  FieldKind = "section"
)

type NeighborDirection string

const (
	NeighborDirectionOutbound NeighborDirection = "OUTBOUND"
	NeighborDirectionInbound  NeighborDirection = "INBOUND"
	NeighborDirectionBoth     NeighborDirection = "BOTH"
)

type NeighborScope string

const (
	NeighborScopeNote    NeighborScope = "NOTE"
	NeighborScopeSubtree NeighborScope = "SUBTREE"
)

type SectionLevel string

const (
	SectionLevelH1 SectionLevel = "H1"
	SectionLevelH2 SectionLevel = "H2"
	SectionLevelH3 SectionLevel = "H3"
	SectionLevelH4 SectionLevel = "H4"
	SectionLevelH5 SectionLevel = "H5"
	SectionLevelH6 SectionLevel = "H6"
)

type SectionDisplay string

const (
	SectionDisplayPane   SectionDisplay = "PANE"
	SectionDisplayInline SectionDisplay = "INLINE"
)

type SemanticsKind string

const (
	SemanticsKindBehavioral  SemanticsKind = "BEHAVIORAL"
	SemanticsKindDocumentary SemanticsKind = "DOCUMENTARY"
)

type FieldAuthoringStyle string

const (
	FieldAuthoringStyleAny            FieldAuthoringStyle = "ANY"
	FieldAuthoringStyleFrontmatter    FieldAuthoringStyle = "FRONTMATTER"
	FieldAuthoringStyleInlineProperty FieldAuthoringStyle = "INLINE_PROPERTY"
	FieldAuthoringStyleListMetadata   FieldAuthoringStyle = "LIST_METADATA"
	FieldAuthoringStyleItemText       FieldAuthoringStyle = "ITEM_TEXT"
	FieldAuthoringStyleCheckbox       FieldAuthoringStyle = "CHECKBOX"
)

type TypeRole string

const (
	TypeRoleNote         TypeRole = "NOTE"
	TypeRoleSection      TypeRole = "SECTION"
	TypeRoleEmbeddedNode TypeRole = "EMBEDDED_NODE"
	TypeRoleInterface    TypeRole = "INTERFACE"
)

type Schema struct {
	Hash                  string
	Dir                   string
	Files                 []string
	EnumTypes             map[string]*EnumType
	Types                 map[string]*NoteType
	Interfaces            map[string]*InterfaceType
	Scalars               map[string]struct{}
	NoteInterfaceAuthored bool
	DefaultNoteType       string
}

type Guidance struct {
	Summary           string `json:"summary,omitempty"`
	Meaning           string `json:"meaning,omitempty"`
	Authoring         string `json:"authoring,omitempty"`
	AgentImplications string `json:"agentImplications,omitempty"`
}

type PolicyHint struct {
	RequiresUserConfirmation      bool   `json:"requiresUserConfirmation,omitempty"`
	ForbidAutonomousSemanticEdits bool   `json:"forbidAutonomousSemanticEdits,omitempty"`
	EditScope                     string `json:"editScope,omitempty"`
	Reason                        string `json:"reason,omitempty"`
}

type EnumType struct {
	Name        string
	Description string
	Guidance    *Guidance
	Values      []*EnumValue
	ByName      map[string]*EnumValue
}

func (t *EnumType) HasValue(name string) bool {
	if t == nil {
		return false
	}
	if t.ByName != nil {
		_, ok := t.ByName[name]
		return ok
	}
	for _, value := range t.Values {
		if value != nil && value.Name == name {
			return true
		}
	}
	return false
}

// Tones returns each value's presentation tone in declaration order: the
// authored @view(tone:), else the default for its declared stage (open
// neutral, active progress, done success, dropped muted), else "muted" for
// values collapsed by default, "neutral" for the lowest-ranked value, and
// "progress" for the rest. Inferred stages never change tone.
func (t *EnumType) Tones() []string {
	if t == nil {
		return nil
	}
	lowest := -1
	for index, value := range t.Values {
		if value != nil && value.Name != "" && (lowest == -1 || value.View.Order < t.Values[lowest].View.Order) {
			lowest = index
		}
	}
	tones := make([]string, len(t.Values))
	for index, value := range t.Values {
		switch {
		case value == nil:
		case value.View.Tone != "":
			tones[index] = value.View.Tone
		case value.View.Stage != "":
			tones[index] = stageDefaultTones[value.View.Stage]
		case value.View.Collapsed != nil && *value.View.Collapsed:
			tones[index] = "muted"
		case index == lowest:
			tones[index] = "neutral"
		default:
			tones[index] = "progress"
		}
	}
	return tones
}

type EnumValue struct {
	Name        string
	Description string
	Guidance    *Guidance
	Policy      *PolicyHint
	View        EnumValueView
}

type EnumValueView struct {
	Label     string
	Order     int
	Collapsed *bool
	Tone      string
	// Stage is the authored @view(stage:); empty when the value declares none.
	Stage LifecycleStage
	// orderSet distinguishes an authored order of 0 from no order.
	orderSet bool
}

type NoteType struct {
	Name        string
	Description string
	Guidance    *Guidance
	Role        TypeRole
	Label       string
	// LabelAuthored reports whether Label came from @display(singular:) or
	// @node(label:) rather than falling back to the type name.
	LabelAuthored  bool
	PluralLabel    string
	DisplayGroup   string
	DisplayParent  string
	Color          string
	KeyField       string
	PropertyCase   PropertyCase
	Default        bool
	Paths          []string
	Matches        []string
	Matchers       []*NoteMatcher
	Implements     []string
	Fields         []*Field
	ByName         map[string]*Field
	Semantics      SemanticsKind
	CompanionDocs  []CompanionDocRef
	Annotations    map[string]map[string]any
	Title          *TitleConstraint
	RequiresWhen   []ConditionalRequirement
	SourceShape    EmbeddedSourceShape
	SourceMarker   string
	SourcePaths    []string
	SourceMatchers []*NoteMatcher

	// Preview is set on section-role types that declare @preview. Note-role
	// types reject @preview at compile time — it only makes sense inside
	// repeated, collapsible section cards.
	Preview *PreviewSpec
}

// PreviewSpec captures a @preview directive on a section-role type. Template
// is the raw string the author wrote; Placeholders is the ordered list of
// `{{name}}` tokens validated against the type's scalar/enum fields plus the
// built-in `{{title}}`. Keeping the raw string around lets callers interpolate
// cheaply at resolve/render time without re-parsing.
type PreviewSpec struct {
	Template     string
	Placeholders []string
	Collapsed    bool
}

type TitleConstraint struct {
	Pattern      string
	NotPattern   string
	patternRE    *regexp.Regexp
	notPatternRE *regexp.Regexp
}

type FieldFormatConstraint struct {
	Pattern      string
	NotPattern   string
	patternRE    *regexp.Regexp
	notPatternRE *regexp.Regexp
}

type ConditionalRequirement struct {
	Field   string
	Equals  string
	Require []RequiredFieldCondition
}

type RequiredFieldCondition struct {
	Field  string
	Equals string
}

type InterfaceType struct {
	Name          string
	Description   string
	Guidance      *Guidance
	Role          TypeRole
	Label         string
	PluralLabel   string
	DisplayGroup  string
	DisplayParent string
	Implements    []string
	Fields        []*Field
	ByName        map[string]*Field
	Semantics     SemanticsKind
	CompanionDocs []CompanionDocRef
	Annotations   map[string]map[string]any
}

type Field struct {
	Name                 string
	Description          string
	Guidance             *Guidance
	Kind                 FieldKind
	TypeName             string
	List                 bool
	Required             bool
	Source               string
	SourceAliases        []string
	SourceKind           FieldSource
	Inverse              string
	ReverseField         string
	Direction            NeighborDirection
	Scope                NeighborScope
	IncludeBodyLinks     bool
	IncludeBacklinks     bool
	ContextInclude       bool
	WorkspaceMember      bool
	WorkspaceMemberLabel string
	Semantics            SemanticsKind
	Policy               *PolicyHint
	Display              FieldDisplay
	SectionHeading       string
	SectionLevel         SectionLevel
	SectionRequired      bool
	SectionDisplay       SectionDisplay
	ContainsMin          int
	ContainsMax          int
	EmbeddedSourceShape  EmbeddedSourceShape
	EmbeddedSourceMarker string
	CompanionDocs        []CompanionDocRef
	AuthoringStyle       FieldAuthoringStyle
	Format               *FieldFormatConstraint
	Annotations          map[string]map[string]any
	position             *ast.Position
	displayRoleOwner     string
	displayRoleSet       bool
	displayImportanceSet bool
	displayHoverSet      bool

	// IsIdentifier marks a scalar String/ID field as a stable identifier. On
	// note types this is a frontmatter identifier that must also appear in
	// aliases. On embedded section types this is an inline identifier that may
	// carry the section's block-safe Obsidian block target.
	IsIdentifier bool

	// IsPreferredIdentifier marks this identifier as the canonical handle
	// for the note. At most one field per NoteType may set this. The
	// validator also enforces vault-wide uniqueness of the value.
	IsPreferredIdentifier bool

	// IsDerivableIdentifier marks an embedded preferred-identifier field as
	// structurally derivable: when no `id::` line is authored, the indexer can
	// derive `${parent.id}-${DerivedSuffix}${n}` from sibling position. The
	// populate policy decides whether that value must be authored on create or
	// may remain structural until first link.
	//
	// Default true when the containing type is @node(locator: EMBEDDED) and
	// IsPreferredIdentifier is true; false otherwise. Top-level note ids stay
	// required-authored because there is no parent context to derive from.
	IsDerivableIdentifier bool

	// DerivedSuffix supplies the per-type tag used when deriving or minting
	// ids for IsDerivableIdentifier fields, e.g. "US" for UserStory or "AC"
	// for AcceptanceCriterion. Stacked derivations preserve the no-separator
	// convention: SPEC-0023-US1-AC2, never SPEC-0023-US-1-AC-2.
	DerivedSuffix string

	// IdentifierPopulate controls when tooling should materialize a preferred
	// identifier into markdown. Derivable identity and authored population are
	// separate: a value can be computed from structure but still be required in
	// source so authors get a stable link target immediately.
	IdentifierPopulate IdentifierPopulate

	// IdentifierFormat captures the optional id allocation contract declared
	// on @identifier(strategy:..., prefix:..., pad:..., separator:...). When
	// IdentifierFormat is non-nil, agents and tooling can deterministically
	// allocate the next id for the type (see pkg/ontology/idalloc and
	// `rzm agent next-id`).
	// When IdentifierFormat is nil, the field is still a stable identifier,
	// but new ids must be authored manually.
	IdentifierFormat *IdentifierFormat
}

// IdentifierFormat describes the strategy and rendered namespace of a typed
// note's identifier values. StrategyContract owns the strategy-specific
// syntax; prefix and separator define the shared collision namespace.
type IdentifierFormat struct {
	// Strategy selects the allocation algorithm.
	Strategy IdentifierStrategy `json:"strategy"`
	// Prefix is the uppercase letter prefix authored before the separator,
	// e.g. "SPEC" or "EFF". Required when IdentifierFormat is non-nil.
	Prefix string `json:"prefix"`
	// Separator sits between the prefix and the numeric suffix. Defaults to
	// "-" when omitted on the directive.
	Separator string `json:"separator"`
	// Pad is the zero-pad width for the numeric suffix. Defaults to 4.
	Pad int `json:"pad"`
}

// IdentifierStrategy names the allocation algorithm for a schema-declared
// identifier format.
type IdentifierStrategy string

const (
	IdentifierStrategySequential IdentifierStrategy = "SEQUENTIAL"
	IdentifierStrategyDateTime   IdentifierStrategy = "DATETIME"
)

type IdentifierPopulate string

const (
	IdentifierPopulateManual   IdentifierPopulate = "MANUAL"
	IdentifierPopulateOnCreate IdentifierPopulate = "ON_CREATE"
	IdentifierPopulateOnLink   IdentifierPopulate = "ON_LINK"
)

type CompanionDocRef struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose,omitempty"`
}

type NoteMatcher struct {
	Raw        string
	PathPrefix string
	Expression *matchExpression
}

// PreferredIdentifierField returns the first declared preferred identifier.
func (t *NoteType) PreferredIdentifierField() *Field {
	if t == nil {
		return nil
	}
	for _, field := range t.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field
		}
	}
	return nil
}
