// Package views executes repo-configured Rhizome workbench views.
package views

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var (
	ErrViewNotFound       = errors.New("view not found")
	ErrInvalidView        = errors.New("invalid view configuration")
	ErrUnsupportedVariant = errors.New("unsupported view variant")
	ErrInvalidRequest     = errors.New("invalid view execution request")
)

type ServiceOptions struct {
	VaultPath   string
	VaultDef    obsidian.VaultDefinition
	NoteReader  obsidian.NoteReader
	Store       noderead.Store
	Schema      *ontology.Schema
	ExecSchema  *ontologyquery.ExecutableSchema
	QueryDeps   ontologyquery.Deps
	ReadOverlay *ontologyquery.ReadOverlay
	Views       []viewconfig.ViewDefinition
	LoadIssues  []viewconfig.Issue
	// Bundled holds the views shipped with this build (web/bundled-views), as a
	// third catalog source beside repository and generated definitions. Nil
	// means none, as in a build without the web UI.
	Bundled        fs.FS
	SourceResolver SourceResolver
}

type Service struct {
	opts ServiceOptions
}

type Catalog struct {
	Targets []ViewTarget       `json:"targets"`
	Views   []CatalogEntry     `json:"views"`
	Issues  []viewconfig.Issue `json:"issues,omitempty"`
}

type CatalogEntry struct {
	Configuration     map[string]any            `json:"configuration,omitempty"`
	ID                string                    `json:"id"`
	Name              string                    `json:"name"`
	Description       string                    `json:"description,omitempty"`
	Generated         bool                      `json:"generated,omitempty"`
	Origin            viewconfig.Origin         `json:"origin"`
	Source            viewconfig.SourceSpec     `json:"source"`
	Mount             viewconfig.MountSpec      `json:"mount"`
	Defaults          viewconfig.DefaultsSpec   `json:"defaults"`
	Variants          viewconfig.VariantSet     `json:"variants"`
	AvailableVariants []string                  `json:"availableVariants"`
	Definition        viewconfig.ViewDefinition `json:"definition"`
	Issues            []viewconfig.Issue        `json:"issues,omitempty"`
}

type ExecuteRequest struct {
	Variant      string                  `json:"variant,omitempty"`
	FilterPreset string                  `json:"filterPreset,omitempty"`
	Search       string                  `json:"search,omitempty"`
	Filters      []viewconfig.FilterSpec `json:"filters,omitempty"`
	Sort         []viewconfig.SortSpec   `json:"sort,omitempty"`
	Group        *viewconfig.GroupSpec   `json:"group,omitempty"`
	Page         PageRequest             `json:"page,omitempty"`
	Source       SourceRequest           `json:"source,omitempty"`
	PageSet      bool                    `json:"-"`
	Inputs       map[string]string       `json:"inputs,omitempty"`
	EditSession  *EditSessionRequest     `json:"editSession,omitempty"`
	// ColumnField switches a board's column field among its lifecycle and
	// ordered fields; LaneField picks its lane field, and "none" turns lanes
	// off. Empty keeps the view's configuration, else the profile default.
	ColumnField string `json:"columnField,omitempty"`
	LaneField   string `json:"laneField,omitempty"`
}

func (r *ExecuteRequest) UnmarshalJSON(data []byte) error {
	type executeRequestAlias ExecuteRequest
	var raw struct {
		executeRequestAlias
		Page *PageRequest `json:"page"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = ExecuteRequest(raw.executeRequestAlias)
	if raw.Page != nil {
		r.Page = *raw.Page
		r.PageSet = true
	}
	return nil
}

type PageRequest struct {
	Offset int `json:"offset,omitempty"`
	First  int `json:"first,omitempty"`
}

type SourceRequest struct {
	MaxRows int `json:"maxRows,omitempty"`
}

type EditSessionRequest struct {
	ID        string          `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Snapshot  json.RawMessage `json:"snapshot,omitempty"`
}

func (r *EditSessionRequest) RequestedID() string {
	if r == nil {
		return ""
	}
	if r.SessionID != "" {
		return r.SessionID
	}
	return r.ID
}

func (r *EditSessionRequest) IsZero() bool {
	return r == nil || (r.ID == "" && r.SessionID == "" && len(r.Snapshot) == 0)
}

type SourceConstraints struct {
	Search  string                  `json:"search,omitempty"`
	Filters []viewconfig.FilterSpec `json:"filters,omitempty"`
	Sort    []viewconfig.SortSpec   `json:"sort,omitempty"`
	Page    PageRequest             `json:"page,omitempty"`
}

type SourceCompleteness string

const (
	SourceComplete SourceCompleteness = "complete"
	SourceBounded  SourceCompleteness = "bounded"
	SourceUnknown  SourceCompleteness = "unknown"
)

type ConstraintReliability string

const (
	ConstraintsExact      ConstraintReliability = "exact"
	ConstraintsCapBound   ConstraintReliability = "cap_bound"
	ConstraintsNotPlanned ConstraintReliability = "not_planned"
)

type SourceCapPolicy struct {
	Limit  int    `json:"limit,omitempty"`
	Source string `json:"source,omitempty"`
}

type ConstraintPlanSummary struct {
	Pushed             SourceConstraints     `json:"pushed,omitempty"`
	Residual           SourceConstraints     `json:"residual,omitempty"`
	CandidateLimit     int                   `json:"candidateLimit,omitempty"`
	CandidateCount     int                   `json:"candidateCount,omitempty"`
	SourceCompleteness SourceCompleteness    `json:"sourceCompleteness"`
	Reliability        ConstraintReliability `json:"reliability"`
	CapPolicy          SourceCapPolicy       `json:"capPolicy,omitempty"`
	Warnings           []Warning             `json:"warnings,omitempty"`
}

type ExecuteResponse struct {
	View         CatalogEntry      `json:"view"`
	Variant      string            `json:"variant"`
	State        ExecutionState    `json:"state"`
	Capabilities []FieldCapability `json:"capabilities,omitempty"`
	Columns      []TableColumn     `json:"columns"`
	Rows         []TableRow        `json:"rows"`
	Groups       []TableGroup      `json:"groups,omitempty"`
	Card         *CardLayout       `json:"card,omitempty"`
	Board        *BoardLayout      `json:"board,omitempty"`
	PageInfo     PageInfo          `json:"pageInfo"`
	// Profile is the source type's or interface's schema-derived profile.
	Profile *ontology.TypeProfile `json:"profile,omitempty"`
	// Stats summarizes every matching row before paging.
	Stats                 *ExecutionStats       `json:"stats,omitempty"`
	Warnings              []Warning             `json:"warnings,omitempty"`
	ConstraintPlan        bool                  `json:"constraintPlan,omitempty"`
	PushedConstraints     SourceConstraints     `json:"pushedConstraints,omitempty"`
	ResidualConstraints   SourceConstraints     `json:"residualConstraints,omitempty"`
	Plan                  ConstraintPlanSummary `json:"plan,omitempty"`
	DefinitionFingerprint string                `json:"definitionFingerprint"`
	SourceFingerprint     string                `json:"sourceFingerprint"`
	ExecutionFingerprint  string                `json:"executionFingerprint"`
}

type ExecutionState struct {
	Variant      string                  `json:"variant,omitempty"`
	FilterPreset string                  `json:"filterPreset,omitempty"`
	Search       string                  `json:"search,omitempty"`
	Filters      []viewconfig.FilterSpec `json:"filters,omitempty"`
	Sort         []viewconfig.SortSpec   `json:"sort,omitempty"`
	Group        *viewconfig.GroupSpec   `json:"group,omitempty"`
	Page         PageRequest             `json:"page"`
	Source       SourceRequest           `json:"source,omitempty"`
	Inputs       map[string]string       `json:"inputs,omitempty"`
	// ColumnField and LaneField are the board fields the request asked for
	// or the view configures; LaneField is empty when the profile chooses.
	ColumnField string `json:"columnField,omitempty"`
	LaneField   string `json:"laneField,omitempty"`
}

type FieldCapability struct {
	Key               string                          `json:"key"`
	Label             string                          `json:"label,omitempty"`
	ValueKind         string                          `json:"valueKind,omitempty"`
	CanonicalField    string                          `json:"canonicalField,omitempty"`
	SourceKeys        []string                        `json:"sourceKeys,omitempty"`
	SourceKind        string                          `json:"sourceKind,omitempty"`
	SemanticRole      string                          `json:"semanticRole,omitempty"`
	Required          bool                            `json:"required,omitempty"`
	Importance        ontology.FieldDisplayImportance `json:"importance"`
	Sortable          bool                            `json:"sortable"`
	IndexedSortable   bool                            `json:"indexedSortable,omitempty"`
	ResidualSortable  bool                            `json:"residualSortable,omitempty"`
	Groupable         bool                            `json:"groupable"`
	FilterOps         []string                        `json:"filterOps,omitempty"`
	IndexedFilterOps  []string                        `json:"indexedFilterOps,omitempty"`
	ResidualFilterOps []string                        `json:"residualFilterOps,omitempty"`
	Completeness      SourceCompleteness              `json:"completeness,omitempty"`
	Source            string                          `json:"source,omitempty"`
	Values            []any                           `json:"values,omitempty"`
	Facets            *FacetCapability                `json:"facets,omitempty"`
	Filter            *FilterCapability               `json:"filter,omitempty"`
	Group             *GroupCapability                `json:"group,omitempty"`
	EnumName          string                          `json:"enumName,omitempty"`
	EnumValues        []FieldEnumValue                `json:"enumValues,omitempty"`
	Edit              *EditCapability                 `json:"edit,omitempty"`
	// PolicyReason is the field's @policy reason, shown where an edit is
	// restricted or a gap facet explains why a field is left empty.
	PolicyReason     string `json:"policyReason,omitempty"`
	List             bool   `json:"-"`
	cardinalityKnown bool
	schemaOrder      int
	schemaOrderKnown bool
}

type FieldCandidatesRequest struct {
	Field string
	Query string
	Limit int
}

type FieldCandidatesResponse struct {
	Field      string          `json:"field"`
	TargetType string          `json:"targetType,omitempty"`
	Candidates []EditCandidate `json:"candidates,omitempty"`
	HasMore    bool            `json:"hasMore,omitempty"`
}

type FacetCapability struct {
	Values []any `json:"values,omitempty"`
	// Labels maps a link value to its target note's title.
	Labels map[string]string `json:"labels,omitempty"`
}

type FilterCapability struct {
	Ops     []string `json:"ops,omitempty"`
	Options []any    `json:"options,omitempty"`
}

type GroupCapability struct {
	Values []FieldEnumValue `json:"values,omitempty"`
}

type FieldEnumValue struct {
	Value              string `json:"value"`
	Label              string `json:"label,omitempty"`
	Description        string `json:"description,omitempty"`
	Rank               int    `json:"rank,omitempty"`
	Tone               string `json:"tone,omitempty"`
	CollapsedByDefault bool   `json:"collapsedByDefault,omitempty"`
	// Stage is the value's lifecycle stage; StageDeclared is false when it
	// was inferred from authored tone or collapsed metadata.
	Stage         ontology.LifecycleStage `json:"stage,omitempty"`
	StageDeclared bool                    `json:"stageDeclared,omitempty"`
}

// ExecutionStats summarizes every row that matches a view's filters and
// search, computed before paging.
type ExecutionStats struct {
	Total      int `json:"total"`
	IssueCount int `json:"issueCount"`
	// StaleCount counts rows in an active-stage lifecycle value unchanged for 30 days.
	StaleCount int               `json:"staleCount"`
	Lifecycle  []StatsValueCount `json:"lifecycle,omitempty"`
	// Kinds counts rows by resolved type for an interface source.
	Kinds []StatsValueCount `json:"kinds,omitempty"`
	// Fields reports how many rows fill each generated column field and KEY field.
	Fields []StatsFieldFill `json:"fields,omitempty"`
	Dates  *StatsDateRange  `json:"dates,omitempty"`
	// Truncated is true when the source cap may have cut rows, so the
	// statistics cover only the rows the source returned.
	Truncated bool `json:"truncated,omitempty"`
}

type StatsValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type StatsFieldFill struct {
	Field  string `json:"field"`
	Filled int    `json:"filled"`
}

// StatsDateRange describes the profile's primary date over matching rows.
type StatsDateRange struct {
	Field string `json:"field"`
	First string `json:"first,omitempty"`
	Last  string `json:"last,omitempty"`
	// Months counts rows per calendar month as YYYY-MM, oldest first.
	Months []StatsValueCount `json:"months,omitempty"`
}

type EditCapability struct {
	Kind       string          `json:"kind"`
	Operation  string          `json:"operation"`
	Field      string          `json:"field"`
	List       bool            `json:"list"`
	InputMode  string          `json:"inputMode,omitempty"`
	ValueKind  string          `json:"valueKind,omitempty"`
	Options    []string        `json:"options,omitempty"`
	TargetType string          `json:"targetType,omitempty"`
	Candidates []EditCandidate `json:"candidates,omitempty"`
}

type EditCandidate struct {
	Ref   ontology.NodeRef `json:"ref"`
	Value string           `json:"value"`
	Label string           `json:"label"`
	Path  string           `json:"path,omitempty"`
}

type TableColumn struct {
	Field string `json:"field"`
	Label string `json:"label,omitempty"`
}

type TableRelationValue struct {
	Value string            `json:"value"`
	Ref   *ontology.NodeRef `json:"ref,omitempty"`
	Title string            `json:"title,omitempty"`
	// Status is the linked record's lifecycle value from its type's profile,
	// set on board and card executions for up to eight values per field.
	Status *TableRelationStatus `json:"status,omitempty"`
}

// TableRelationStatus is a linked record's lifecycle value.
type TableRelationStatus struct {
	Value string                  `json:"value"`
	Label string                  `json:"label,omitempty"`
	Tone  string                  `json:"tone,omitempty"`
	Stage ontology.LifecycleStage `json:"stage,omitempty"`
}

type TableRow struct {
	Ref            ontology.NodeRef                `json:"ref"`
	Path           string                          `json:"path,omitempty"`
	Title          string                          `json:"title,omitempty"`
	ResolvedType   string                          `json:"resolvedType,omitempty"`
	UpdatedAt      int64                           `json:"updatedAt,omitempty"`
	HasIssues      bool                            `json:"hasIssues,omitempty"`
	Tags           []string                        `json:"tags,omitempty"`
	Fields         map[string]any                  `json:"fields,omitempty"`
	RelationValues map[string][]TableRelationValue `json:"relationValues,omitempty"`
}

type TableGroup struct {
	Field              string       `json:"field"`
	Key                string       `json:"key"`
	Label              string       `json:"label,omitempty"`
	Value              string       `json:"value"`
	Count              int          `json:"count"`
	TotalCount         int          `json:"totalCount"`
	Tone               string       `json:"tone,omitempty"`
	Depth              int          `json:"depth"`
	RowStart           int          `json:"rowStart"`
	RowEnd             int          `json:"rowEnd"`
	CollapsedByDefault bool         `json:"collapsedByDefault,omitempty"`
	Children           []TableGroup `json:"children,omitempty"`
}

type CardLayout struct {
	Eyebrow *TableColumn  `json:"eyebrow,omitempty"`
	Title   TableColumn   `json:"title"`
	Preview *TableColumn  `json:"preview,omitempty"`
	Fields  []TableColumn `json:"fields"`
}

type BoardLayout struct {
	ColumnField string        `json:"columnField"`
	Editable    bool          `json:"editable"`
	Columns     []BoardColumn `json:"columns"`
	// LaneField is the field that splits the board into lanes; empty when
	// the board has none. Lanes then lists them in display order.
	LaneField string      `json:"laneField,omitempty"`
	Lanes     []BoardLane `json:"lanes,omitempty"`
}

// BoardLane is one horizontal lane. Cells follow BoardLayout.Columns order
// and index into ExecuteResponse.Rows, so a row whose lane field holds several
// targets appears in each of those lanes.
type BoardLane struct {
	Key   string          `json:"key"`
	Value string          `json:"value"`
	Label string          `json:"label"`
	Tone  string          `json:"tone,omitempty"`
	Count int             `json:"count"`
	Cells []BoardLaneCell `json:"cells"`
}

type BoardLaneCell struct {
	Value string `json:"value"`
	Rows  []int  `json:"rows"`
}

type BoardColumn struct {
	Key                string `json:"key"`
	Value              string `json:"value"`
	Label              string `json:"label"`
	Tone               string `json:"tone,omitempty"`
	Count              int    `json:"count"`
	RowStart           int    `json:"rowStart"`
	RowEnd             int    `json:"rowEnd"`
	CollapsedByDefault bool   `json:"collapsedByDefault,omitempty"`
}

type PageInfo struct {
	Total    int  `json:"total"`
	Offset   int  `json:"offset"`
	First    int  `json:"first"`
	Returned int  `json:"returned"`
	HasMore  bool `json:"hasMore"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type SourceResult struct {
	Rows                []TableRow            `json:"rows"`
	Capabilities        []FieldCapability     `json:"capabilities,omitempty"`
	Warnings            []Warning             `json:"warnings,omitempty"`
	SourceFingerprint   string                `json:"sourceFingerprint,omitempty"`
	ConstraintPlan      bool                  `json:"constraintPlan,omitempty"`
	PushedConstraints   SourceConstraints     `json:"pushedConstraints,omitempty"`
	ResidualConstraints SourceConstraints     `json:"residualConstraints,omitempty"`
	Plan                ConstraintPlanSummary `json:"plan,omitempty"`
	relationScope       *noderead.Scope
}

type SourceResolver interface {
	ResolveSource(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error)
}
