package store

import (
	"context"
	"database/sql"
	"strings"
)

type NotePropertySource int

const (
	NotePropertySourceFrontmatter NotePropertySource = 1
	NotePropertySourceInline      NotePropertySource = 2
)

type NotePropertyValueKind int

const (
	NotePropertyValueUnknown NotePropertyValueKind = iota
	NotePropertyValueString
	NotePropertyValueBool
	NotePropertyValueInt
	NotePropertyValueFloat
	NotePropertyValueDate
	NotePropertyValueDateTime
	NotePropertyValueURL
	NotePropertyValueWikilink
)

type NoteMetadataState struct {
	NotesHash    string
	RawNotesHash string
	LoadedAt     int64
	Ready        bool
}

type NoteMetadataRow struct {
	NoteID         int64
	Path           string
	Title          string
	ContentHash    string
	IndexerVersion string
	Mtime          int64
	Size           int64
	IndexedAt      int64
	FormatID       string
	Projection     NoteProjectionState
}

// NoteProjectionStatus records whether the provider projection associated
// with a current authored source is usable by downstream consumers.
type NoteProjectionStatus string

const (
	NoteProjectionStatusStale   NoteProjectionStatus = "stale"
	NoteProjectionStatusCurrent NoteProjectionStatus = "current"
	NoteProjectionStatusFatal   NoteProjectionStatus = "fatal"
)

// NoteProjectionState is format-provider provenance for one note source.
// Source identity remains on NoteMetadataRow; this state lets a current source
// remain durable while stale or fatal derived facts are unavailable.
type NoteProjectionState struct {
	ProviderVersion   string
	ProjectionVersion string
	SourceContentHash string
	Status            NoteProjectionStatus
	DiagnosticCode    string
	DiagnosticDetail  string
	UpdatedAt         int64
}

type NotePropertyValueRow struct {
	NoteID       int64
	NotePath     string
	PropertyName string
	Source       NotePropertySource
	ValueText    string
	ValueNorm    string
	ValueKind    NotePropertyValueKind
	IsList       bool
	ListOrdinal  int
}

type NoteTagRow struct {
	NoteID   int64
	NotePath string
	TagNorm  string
}

// DurableNoteFacts is one point-in-time read of the raw note facts needed for
// exact-note hydration while global metadata reconciliation is pending.
type DurableNoteFacts struct {
	MetadataRows          map[string]NoteMetadataRow
	PropertyValues        []NotePropertyValueRow
	Tags                  []NoteTagRow
	FragmentTargets       []NoteFragmentTargetRow
	ProjectionDiagnostics []NoteProjectionDiagnosticRow
	SearchRegions         []NoteSearchRegionRow
	LinkInputs            []NoteLinkInputRow
	ProjectionPayloads    map[string]string
}

type noteMetadataQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type NoteSearchScope int

const (
	NoteSearchScopePath NoteSearchScope = iota + 1
	NoteSearchScopeTitle
	NoteSearchScopeContent
	NoteSearchScopeSegment
)

type NoteSearchTermRow struct {
	NoteID         int64
	Scope          NoteSearchScope
	SegmentOrdinal int
	TokenOrdinal   int
	TokenText      string
}

type NoteFragmentTargetKind string

const (
	NoteFragmentTargetHeading    NoteFragmentTargetKind = "heading"
	NoteFragmentTargetBlock      NoteFragmentTargetKind = "block"
	NoteFragmentTargetElementID  NoteFragmentTargetKind = "element_id"
	NoteFragmentTargetLegacyName NoteFragmentTargetKind = "legacy_name"
)

// NoteFragmentTargetRow is one provider-authored addressable fragment.
type NoteFragmentTargetRow struct {
	NoteID     int64
	NotePath   string
	Kind       NoteFragmentTargetKind
	Target     string
	TargetNorm string
	Ordinal    int
}

type NoteProjectionDiagnosticRow struct {
	NoteID            int64
	NotePath          string
	Ordinal           int
	Code              string
	Category          string
	Message           string
	RangePresent      bool
	StartByte         int
	EndByte           int
	Blocking          bool
	AffectedOperation string
}

type NoteSearchRegionRow struct {
	NoteID       int64
	NotePath     string
	Ordinal      int
	Origin       string
	Kind         string
	Text         string
	MediaType    string
	RangePresent bool
	StartByte    int
	EndByte      int
}

func IsEmptyListSentinel(row NotePropertyValueRow) bool {
	return row.IsList && row.ListOrdinal == 0 && row.ValueKind == NotePropertyValueUnknown &&
		strings.TrimSpace(row.ValueText) == "" && strings.TrimSpace(row.ValueNorm) == ""
}

type NoteMetadataSnapshot struct {
	State                 NoteMetadataState
	Notes                 []NoteMetadataRow
	PropertyValues        []NotePropertyValueRow
	Tags                  []NoteTagRow
	FragmentTargets       []NoteFragmentTargetRow
	ProjectionDiagnostics []NoteProjectionDiagnosticRow
	SearchRegions         []NoteSearchRegionRow
	LinkInputs            []NoteLinkInputRow
	ProjectionPayloads    []NoteProjectionPayloadRow
	WikilinkEdges         []GraphDocEdgeRow
}

type NoteMetadataDelta struct {
	State                 NoteMetadataState
	Notes                 []NoteMetadataRow
	PropertyValues        []NotePropertyValueRow
	Tags                  []NoteTagRow
	FragmentTargets       []NoteFragmentTargetRow
	ProjectionDiagnostics []NoteProjectionDiagnosticRow
	SearchRegions         []NoteSearchRegionRow
	LinkInputs            []NoteLinkInputRow
	ProjectionPayloads    []NoteProjectionPayloadRow
	WikilinkEdges         []GraphDocEdgeRow
	DeletedPaths          []string
	// SourceTouches refreshes mtime/size for notes whose content identity is
	// unchanged. Their derived rows are deliberately left untouched.
	SourceTouches []NoteSourceTouch
}

const sqliteValuesBatchMaxParams = 900
