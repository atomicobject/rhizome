// Package viewconfig loads and validates native Rhizome configured view files.
package viewconfig

import (
	"encoding/json"
	"io/fs"
)

// WHY: configured views are native Rhizome repo settings, not Obsidian `.base`
// files or saved query recipes. The envelope keeps source, mount, and variants
// separate so table, card, and kanban capabilities share one execution model
// without changing repo-authored view identity.
const APIVersion = "rhizome.view.v1"

type SourceKind string

const (
	SourceKindOntologyType      SourceKind = "ontology_type"
	SourceKindOntologyInterface SourceKind = "ontology_interface"
	SourceKindQueryRecipe       SourceKind = "query_recipe"
	SourceKindCustom            SourceKind = "custom"
)

type MountKind string

const (
	MountKindType       MountKind = "type"
	MountKindInterface  MountKind = "interface"
	MountKindStandalone MountKind = "standalone"
	MountKindWorkspace  MountKind = "workspace"
	MountKindGroup      MountKind = "group"
	MountKindNode       MountKind = "node"
)

// Origin says where a catalog definition came from. A repository definition
// replaces a bundled one with the same id; generated definitions are runtime
// defaults for ontology types and interfaces.
type Origin string

const (
	OriginRepository Origin = "repository"
	OriginBundled    Origin = "bundled"
	OriginGenerated  Origin = "generated"
)

type Source struct {
	Path  string `json:"path,omitempty"`
	Line  int    `json:"line,omitempty"`
	Block string `json:"block,omitempty"`
}

type ViewDefinition struct {
	APIVersion    string         `json:"apiVersion" yaml:"apiVersion"`
	ID            string         `json:"id" yaml:"id"`
	Name          string         `json:"name" yaml:"name"`
	Configuration map[string]any `json:"configuration,omitempty" yaml:"configuration,omitempty"`
	Description   string         `json:"description,omitempty" yaml:"description,omitempty"`
	SourceSpec    SourceSpec     `json:"source" yaml:"source"`
	Mount         MountSpec      `json:"mount" yaml:"mount"`
	Defaults      DefaultsSpec   `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	FilterPresets []FilterPreset `json:"filterPresets,omitempty" yaml:"filterPresets,omitempty"`
	Variants      VariantSet     `json:"variants" yaml:"variants"`
	Generated     bool           `json:"generated,omitempty" yaml:"-"`
	// Origin is assigned by the catalog. Bundled definitions carry a Source.Path
	// relative to the bundled views filesystem, not an absolute path.
	Origin Origin `json:"-" yaml:"-"`
	Source Source `json:"sourceLocation,omitempty" yaml:"-"`
}

type SourceSpec struct {
	Kind        SourceKind        `json:"kind" yaml:"kind"`
	Type        string            `json:"type,omitempty" yaml:"type,omitempty"`
	Interface   string            `json:"interface,omitempty" yaml:"interface,omitempty"`
	QueryRecipe string            `json:"queryRecipe,omitempty" yaml:"queryRecipe,omitempty"`
	ResultPath  string            `json:"resultPath,omitempty" yaml:"resultPath,omitempty"`
	Inputs      map[string]string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Entry       string            `json:"entry,omitempty" yaml:"entry,omitempty"`
}

type MountSpec struct {
	Kind      MountKind `json:"kind" yaml:"kind"`
	Type      string    `json:"type,omitempty" yaml:"type,omitempty"`
	Interface string    `json:"interface,omitempty" yaml:"interface,omitempty"`
	Group     string    `json:"group,omitempty" yaml:"group,omitempty"`
	Order     int       `json:"order,omitempty" yaml:"order,omitempty"`
	Default   bool      `json:"default,omitempty" yaml:"default,omitempty"`
	Hidden    bool      `json:"hidden,omitempty" yaml:"hidden,omitempty"`
	// ReplaceGenerated makes a type or interface mount take the generated
	// view's place: its layouts become the subject's standard Table/Cards/Board.
	ReplaceGenerated bool `json:"replaceGenerated,omitempty" yaml:"replaceGenerated,omitempty"`
}

type DefaultsSpec struct {
	Variant   string       `json:"variant,omitempty" yaml:"variant,omitempty"`
	First     int          `json:"first,omitempty" yaml:"first,omitempty"`
	SourceCap int          `json:"sourceCap,omitempty" yaml:"sourceCap,omitempty"`
	Search    string       `json:"search,omitempty" yaml:"search,omitempty"`
	Filters   []FilterSpec `json:"filters,omitempty" yaml:"filters,omitempty"`
	Sort      []SortSpec   `json:"sort,omitempty" yaml:"sort,omitempty"`
	Group     *GroupSpec   `json:"group,omitempty" yaml:"group,omitempty"`
	Page      *PageSpec    `json:"page,omitempty" yaml:"page,omitempty"`
}

type VariantSet struct {
	Table  *TableVariant  `json:"table,omitempty" yaml:"table,omitempty"`
	Card   *CardSpec      `json:"card,omitempty" yaml:"card,omitempty"`
	Kanban *KanbanVariant `json:"kanban,omitempty" yaml:"kanban,omitempty"`

	cardDecodeError   string
	kanbanDecodeError string
}

type TableVariant struct {
	Columns []ViewColumn `json:"columns" yaml:"columns"`
	// Density is "two-line" (summary under the title) or "one-line"; empty uses two-line when the source type has a summary field.
	Density string `json:"density,omitempty" yaml:"density,omitempty"`
}

type CardSpec struct {
	Eyebrow string       `json:"eyebrow,omitempty" yaml:"eyebrow,omitempty"`
	Title   string       `json:"title,omitempty" yaml:"title,omitempty"`
	Preview string       `json:"preview,omitempty" yaml:"preview,omitempty"`
	Fields  []ViewColumn `json:"fields,omitempty" yaml:"fields,omitempty"`
}

func (c CardSpec) MarshalJSON() ([]byte, error) {
	type cardSpecJSON struct {
		Eyebrow string        `json:"eyebrow,omitempty"`
		Title   string        `json:"title,omitempty"`
		Preview string        `json:"preview,omitempty"`
		Fields  *[]ViewColumn `json:"fields,omitempty"`
	}
	encoded := cardSpecJSON{Eyebrow: c.Eyebrow, Title: c.Title, Preview: c.Preview}
	if c.Fields != nil {
		encoded.Fields = &c.Fields
	}
	return json.Marshal(encoded)
}

type KanbanVariant struct {
	ColumnField      string    `json:"columnField" yaml:"columnField"`
	HideEmptyColumns bool      `json:"hideEmptyColumns,omitempty" yaml:"hideEmptyColumns,omitempty"`
	Card             *CardSpec `json:"card,omitempty" yaml:"card,omitempty"`
	// LaneField splits the board into horizontal lanes; empty means the
	// profile-derived default, and "none" disables lanes.
	LaneField string `json:"laneField,omitempty" yaml:"laneField,omitempty"`
}

type ViewColumn struct {
	Field string `json:"field" yaml:"field"`
	Label string `json:"label,omitempty" yaml:"label,omitempty"`
}

type FilterSpec struct {
	Field     string   `json:"field" yaml:"field"`
	Op        string   `json:"op" yaml:"op"`
	Value     string   `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom string   `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
	Values    []string `json:"values,omitempty" yaml:"values,omitempty"`
}

type FilterPreset struct {
	ID      string       `json:"id" yaml:"id"`
	Label   string       `json:"label,omitempty" yaml:"label,omitempty"`
	Filters []FilterSpec `json:"filters" yaml:"filters"`
}

type SortSpec struct {
	Field     string `json:"field" yaml:"field"`
	Direction string `json:"direction,omitempty" yaml:"direction,omitempty"`
}

type GroupSpec struct {
	Fields []string         `json:"fields,omitempty" yaml:"fields,omitempty"`
	Field  string           `json:"field,omitempty" yaml:"field,omitempty"`
	Values []GroupValueSpec `json:"values,omitempty" yaml:"values,omitempty"`
	// Bucket groups a Date or DateTime field by period; "month" is the only value.
	Bucket string `json:"bucket,omitempty" yaml:"bucket,omitempty"`
}

// GroupBucketMonth groups dates by calendar month, newest first.
const GroupBucketMonth = "month"

// GroupNone as the only group field, `group: {field: none}`, says the view is
// explicitly ungrouped, so no default grouping applies.
const GroupNone = "none"

// Table densities: two-line rows show the summary under the title.
const (
	TableDensityTwoLine = "two-line"
	TableDensityOneLine = "one-line"
)

type GroupValueSpec struct {
	Field              string `json:"field,omitempty" yaml:"field,omitempty"`
	Value              string `json:"value" yaml:"value"`
	Label              string `json:"label,omitempty" yaml:"label,omitempty"`
	Order              int    `json:"order,omitempty" yaml:"order,omitempty"`
	CollapsedByDefault *bool  `json:"collapsedByDefault,omitempty" yaml:"collapsedByDefault,omitempty"`
	Collapsed          *bool  `json:"collapsed,omitempty" yaml:"collapsed,omitempty"`
}

type PageSpec struct {
	Offset int `json:"offset,omitempty" yaml:"offset,omitempty"`
	First  int `json:"first,omitempty" yaml:"first,omitempty"`
}

type IssueSeverity string

const (
	IssueFatal   IssueSeverity = "fatal"
	IssueWarning IssueSeverity = "warning"
	IssueInfo    IssueSeverity = "info"
)

type Issue struct {
	Code     string        `json:"code"`
	Severity IssueSeverity `json:"severity,omitempty"`
	Message  string        `json:"message"`
	View     string        `json:"view,omitempty"`
	Path     string        `json:"path,omitempty"`
	Line     int           `json:"line,omitempty"`
	Field    string        `json:"field,omitempty"`
	// Variant is the unknown name an unknown_ontology_* issue references. The
	// validation views check reports it as the issue variant; the views API
	// does not expose it.
	Variant string `json:"-"`
}

type ValidateOptions struct {
	GroupNames          map[string]struct{}
	TypeNames           map[string]struct{}
	NodeTypeNames       map[string]struct{}
	InterfaceNames      map[string]struct{}
	QueryRecipeIDs      map[string]struct{}
	QueryRecipeRowPaths map[string]string
	CheckReferences     bool
	GeneratedIDs        map[string]struct{}
	// DateFields lists, per type or interface, the selectors of its single
	// Date and DateTime fields; month grouping is valid only on those.
	DateFields map[string]map[string]struct{}
	// EntryFS, when set, holds the definitions' folders: Source.Path values are
	// slash paths inside it, and custom entries are checked there instead of on
	// disk. Bundled views are validated this way.
	EntryFS fs.FS
}

type ValidationResult struct {
	Views  []ViewDefinition `json:"views"`
	Issues []Issue          `json:"issues,omitempty"`
}
