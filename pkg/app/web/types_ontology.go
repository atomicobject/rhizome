package web

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"strings"
	"time"
)

type OntologySummaryResponse struct {
	SchemaPresent      bool                       `json:"schemaPresent"`
	QuerySchemaPresent bool                       `json:"querySchemaPresent"`
	SchemaHash         string                     `json:"schemaHash,omitempty"`
	QuerySchemaSDL     string                     `json:"querySchemaSDL,omitempty"`
	QuerySchemaError   string                     `json:"querySchemaError,omitempty"`
	TotalNotes         int                        `json:"totalNotes"`
	TypedNotes         int                        `json:"typedNotes"`
	UntypedNotes       int                        `json:"untypedNotes"`
	AmbiguousNotes     int                        `json:"ambiguousNotes"`
	IssueNotes         int                        `json:"issueNotes"`
	Types              []OntologyTypeSummary      `json:"types,omitempty"`
	Interfaces         []OntologyInterfaceSummary `json:"interfaces,omitempty"`
	// Rebuilding marks a read taken while the index republishes note metadata.
	// Counts and types are then the last published snapshot, or zero before
	// the first publication, and must not be read as an empty vault.
	Rebuilding bool `json:"rebuilding,omitempty"`
}

type OntologyTypeSummary struct {
	Name          string   `json:"name"`
	Label         string   `json:"label,omitempty"`
	PluralLabel   string   `json:"pluralLabel,omitempty"`
	DisplayGroup  string   `json:"displayGroup,omitempty"`
	DisplayParent string   `json:"displayParent,omitempty"`
	Color         string   `json:"color,omitempty"`
	Description   string   `json:"description,omitempty"`
	Role          string   `json:"role,omitempty"` // "note" | "embedded"
	Count         int      `json:"count"`
	IssueCount    int      `json:"issueCount,omitempty"`
	StartingNotes []string `json:"startingNotes,omitempty"`
}

// OntologyInterfaceSummary describes an ontology interface that groups note-
// and embedded-node implementors behind one shared count/listing model so the
// Notes rail can render mixed implementor sets consistently.
type OntologyInterfaceSummary struct {
	Name          string   `json:"name"`
	Label         string   `json:"label,omitempty"`
	PluralLabel   string   `json:"pluralLabel,omitempty"`
	DisplayGroup  string   `json:"displayGroup,omitempty"`
	DisplayParent string   `json:"displayParent,omitempty"`
	Description   string   `json:"description,omitempty"`
	Count         int      `json:"count"`
	IssueCount    int      `json:"issueCount,omitempty"`
	Implementors  []string `json:"implementors"`
}

type OntologyTypeResponse struct {
	Type           *schemadoc.TypeDoc     `json:"type,omitempty"`
	Count          int                    `json:"count"`
	IssueCount     int                    `json:"issueCount,omitempty"`
	Notes          []OntologyNoteListItem `json:"notes,omitempty"`
	AuthoringGuide string                 `json:"authoringGuide,omitempty"`
}

// OntologyAtlasTypeEntry summarizes one ontology note- or embedded-role type
// for the atlas overview: schema contract (TypeDoc) plus vault-grounded counts
// and a few starting notes for the hub UI to surface examples without a
// follow-up fetch.
type OntologyAtlasTypeEntry struct {
	Type          *schemadoc.TypeDoc `json:"type"`
	Count         int                `json:"count"`
	IssueCount    int                `json:"issueCount,omitempty"`
	StartingNotes []string           `json:"startingNotes,omitempty"`
}

// OntologyAtlasResponse is a single-fetch payload for the atlas hub: every
// note- or embedded-role type with counts plus interface TypeDocs so the UI
// can render the schema graph + type cards without an N+1 round-trip.
type OntologyAtlasResponse struct {
	SchemaPresent bool                     `json:"schemaPresent"`
	SchemaHash    string                   `json:"schemaHash,omitempty"`
	VaultName     string                   `json:"vaultName,omitempty"`
	Types         []OntologyAtlasTypeEntry `json:"types,omitempty"`
	Sections      []schemadoc.TypeDoc      `json:"sections,omitempty"`
	Interfaces    []schemadoc.TypeDoc      `json:"interfaces,omitempty"`
}

type OntologyNoteListItem struct {
	Ref            ontology.NodeRef        `json:"ref"`
	Path           string                  `json:"path"`
	Title          string                  `json:"title"`
	ResolvedType   string                  `json:"resolvedType,omitempty"`
	HasIssues      bool                    `json:"hasIssues"`
	RelationCount  int                     `json:"relationCount,omitempty"`
	Tags           []string                `json:"tags,omitempty"`
	UpdatedAt      int64                   `json:"updatedAt,omitempty"`
	Issues         []OntologyNoteIssueItem `json:"issues,omitempty"`
	IdentityStatus string                  `json:"identityStatus,omitempty"`
}

type OntologyNoteIssueItem struct {
	Code    string           `json:"code,omitempty"`
	Field   string           `json:"field,omitempty"`
	Message string           `json:"message"`
	Fixable bool             `json:"fixable,omitempty"`
	FixOps  []OntologyEditOp `json:"fixOps,omitempty"`
}

type OntologyInspectResponse struct {
	OntologyAvailable bool                  `json:"ontologyAvailable"`
	SchemaHash        string                `json:"schemaHash,omitempty"`
	Note              *ontology.InspectNote `json:"note,omitempty"`
}

type OntologyQueryRequest struct {
	Query         string                  `json:"query"`
	Variables     map[string]any          `json:"variables,omitempty"`
	OperationName string                  `json:"operationName,omitempty"`
	EditSession   *EditSessionReadRequest `json:"editSession,omitempty"`
}

type EditSessionReadRequest struct {
	ID        string                       `json:"id,omitempty"`
	SessionID string                       `json:"sessionId,omitempty"`
	Snapshot  *OntologyEditSessionSnapshot `json:"snapshot,omitempty"`
}

func (r *EditSessionReadRequest) RequestedID() string {
	if r == nil {
		return ""
	}
	if r.SessionID != "" {
		return r.SessionID
	}
	return r.ID
}

func (r *EditSessionReadRequest) IsZero() bool {
	return r == nil || (strings.TrimSpace(r.ID) == "" && strings.TrimSpace(r.SessionID) == "" && r.Snapshot == nil)
}

type OntologyEditOp struct {
	ID               string                  `json:"id,omitempty"`
	Kind             string                  `json:"kind"`
	Path             string                  `json:"path"`
	NodeID           string                  `json:"nodeId,omitempty"`
	Structural       string                  `json:"structuralFingerprint,omitempty"`
	Field            string                  `json:"field,omitempty"`
	Collection       string                  `json:"collection,omitempty"`
	Value            string                  `json:"value,omitempty"`
	Values           []string                `json:"values,omitempty"`
	FieldValue       *OntologyEditFieldValue `json:"fieldValue,omitempty"`
	Expected         *OntologyEditExpected   `json:"expected,omitempty"`
	Markdown         string                  `json:"markdown,omitempty"`
	PreviousMarkdown string                  `json:"previousMarkdown,omitempty"`
	RangeStart       int                     `json:"rangeStart,omitempty"`
	RangeEnd         int                     `json:"rangeEnd,omitempty"`
	Heading          string                  `json:"heading,omitempty"`
	Body             string                  `json:"body,omitempty"`
	BlockID          string                  `json:"blockId,omitempty"`
	OrderedFragments []string                `json:"orderedFragments,omitempty"`
	OldTarget        string                  `json:"oldTarget,omitempty"`
	NewTarget        string                  `json:"newTarget,omitempty"`
	Property         string                  `json:"property,omitempty"`
	Level            string                  `json:"level,omitempty"`
}

// OntologyEditFieldValue preserves the distinction between a missing field,
// an explicitly empty scalar, and an explicitly empty list in browser edit
// operations. Kind is one of "unset", "scalar", or "list".
type OntologyEditFieldValue struct {
	Kind   string   `json:"kind"`
	Scalar string   `json:"scalar"`
	Items  []string `json:"items"`
}

// OntologyEditExpected carries the field and source revision observed by the
// workspace when it created an edit operation.
type OntologyEditExpected struct {
	Field         *OntologyEditFieldValue `json:"field,omitempty"`
	SourceHash    string                  `json:"sourceHash,omitempty"`
	SourceContent string                  `json:"sourceContent,omitempty"`
}

type OntologyEditSessionCreateRequest struct {
	SessionID string           `json:"sessionId,omitempty"`
	Ops       []OntologyEditOp `json:"ops,omitempty"`
}

type OntologyEditSessionSnapshot struct {
	Version          int                         `json:"version,omitempty"`
	Revision         uint64                      `json:"revision,omitempty"`
	SessionID        string                      `json:"sessionId,omitempty"`
	Ops              []OntologyEditOp            `json:"ops,omitempty"`
	BaseFingerprints map[string]string           `json:"baseFingerprints,omitempty"`
	BaseDocuments    []ontology.EditBaseDocument `json:"baseDocuments,omitempty"`
}

type OntologyEditSessionStageRequest struct {
	RequestID        string                       `json:"requestId,omitempty"`
	ExpectedRevision uint64                       `json:"expectedRevision,omitempty"`
	Ops              []OntologyEditOp             `json:"ops"`
	Replace          bool                         `json:"replace,omitempty"`
	Snapshot         *OntologyEditSessionSnapshot `json:"snapshot,omitempty"`
}

type OntologyEditSessionPreviewRequest struct {
	Snapshot *OntologyEditSessionSnapshot `json:"snapshot,omitempty"`
}

type OntologyEditSessionCommitRequest struct {
	RequestID        string                       `json:"requestId,omitempty"`
	ExpectedRevision uint64                       `json:"expectedRevision,omitempty"`
	Snapshot         *OntologyEditSessionSnapshot `json:"snapshot,omitempty"`
}

type OntologyCollectionChange struct {
	Path             string            `json:"path"`
	Ref              *ontology.NodeRef `json:"ref,omitempty"`
	Collection       string            `json:"collection"`
	OrderedFragments []string          `json:"orderedFragments,omitempty"`
}

type OntologyEditSessionStatus string

const (
	OntologyEditSessionStatusClean      OntologyEditSessionStatus = "clean"
	OntologyEditSessionStatusDirty      OntologyEditSessionStatus = "dirty"
	OntologyEditSessionStatusStale      OntologyEditSessionStatus = "stale"
	OntologyEditSessionStatusRebased    OntologyEditSessionStatus = "rebased"
	OntologyEditSessionStatusConflicted OntologyEditSessionStatus = "conflicted"
)

type OntologyEditSessionResponse struct {
	SessionID              string                       `json:"sessionId"`
	Revision               uint64                       `json:"revision"`
	Status                 OntologyEditSessionStatus    `json:"status"`
	Outcome                ontology.CommitOutcome       `json:"outcome,omitempty"`
	Ops                    []OntologyEditOp             `json:"ops,omitempty"`
	TouchedPaths           []string                     `json:"touchedPaths,omitempty"`
	TouchedNodes           []string                     `json:"touchedNodes,omitempty"`
	TouchedNodeRefs        []ontology.NodeRef           `json:"touchedNodeRefs,omitempty"`
	RefLineage             []ontology.PreviewRefLineage `json:"refLineage,omitempty"`
	ChangedFieldsByNode    map[string][]string          `json:"changedFieldsByNode,omitempty"`
	ChangedFieldsByNodeRef map[string][]string          `json:"changedFieldsByNodeRef,omitempty"`
	CollectionChanges      []OntologyCollectionChange   `json:"collectionChanges,omitempty"`
	BaseFingerprints       map[string]string            `json:"baseFingerprints,omitempty"`
	BaseDocuments          []ontology.EditBaseDocument  `json:"baseDocuments,omitempty"`
	StalePaths             []string                     `json:"stalePaths,omitempty"`
	Rebased                bool                         `json:"rebased,omitempty"`
	Conflicts              []ontology.ConflictReport    `json:"conflicts,omitempty"`
	HasUncommittedChanges  bool                         `json:"hasUncommittedChanges"`
	Plan                   *ontology.CommitPlan         `json:"plan,omitempty"`
	Warnings               []string                     `json:"warnings,omitempty"`
	CreatedAt              time.Time                    `json:"createdAt"`
	UpdatedAt              time.Time                    `json:"updatedAt"`
	Workspaces             []NodeWorkspaceResponse      `json:"workspaces,omitempty"`
}

// ModifiedNotesResponse describes the current edit-session work baselined
// against on-disk content. Rendered by the Modified home in the ontology
// workspace.
type ModifiedNotesResponse struct {
	SessionID  string                    `json:"sessionId"`
	Totals     ModifiedNotesTotals       `json:"totals"`
	Notes      []ModifiedNoteEntry       `json:"notes"`
	Conflicts  []ontology.ConflictReport `json:"conflicts,omitempty"`
	Rebased    bool                      `json:"rebased,omitempty"`
	StalePaths []string                  `json:"stalePaths,omitempty"`
	UpdatedAt  time.Time                 `json:"updatedAt"`
}

// ModifiedNotesTotals reports aggregate op counts per session.
type ModifiedNotesTotals struct {
	Notes        int `json:"notes"`
	Ops          int `json:"ops"`
	SetField     int `json:"setField,omitempty"`
	SetLinkField int `json:"setLinkField,omitempty"`
	SetNarrative int `json:"setNarrative,omitempty"`
	AddEmbedded  int `json:"addEmbedded,omitempty"`
	Delete       int `json:"delete,omitempty"`
	Reorder      int `json:"reorder,omitempty"`
}

// ModifiedNoteEntry groups ops and the unified diff for a single touched note.
type ModifiedNoteEntry struct {
	Path               string               `json:"path"`
	Title              string               `json:"title,omitempty"`
	ResolvedType       string               `json:"resolvedType,omitempty"`
	Diff               string               `json:"diff,omitempty"`
	BaseFingerprint    string               `json:"baseFingerprint,omitempty"`
	CurrentFingerprint string               `json:"currentFingerprint,omitempty"`
	UpdatedFingerprint string               `json:"updatedFingerprint,omitempty"`
	Rebased            bool                 `json:"rebased,omitempty"`
	HasMaterialChange  bool                 `json:"hasMaterialChange"`
	Ops                []ModifiedNoteOpView `json:"ops,omitempty"`
}

// ModifiedNoteOpView enriches a staged edit op with the current on-disk
// baseline so the UI can render "before/after" without a second fetch.
type ModifiedNoteOpView struct {
	ID                string           `json:"id,omitempty"`
	Kind              string           `json:"kind"`
	NodeRef           ontology.NodeRef `json:"nodeRef"`
	NodeTitle         string           `json:"nodeTitle,omitempty"` // committed node an in-note op targets
	Field             string           `json:"field,omitempty"`
	PreviousValue     string           `json:"previousValue,omitempty"`
	Value             string           `json:"value,omitempty"`
	PreviousValues    []string         `json:"previousValues,omitempty"`
	Values            []string         `json:"values,omitempty"`
	PreviousMarkdown  string           `json:"previousMarkdown,omitempty"`
	Markdown          string           `json:"markdown,omitempty"`
	RangeStart        int              `json:"rangeStart,omitempty"`
	RangeEnd          int              `json:"rangeEnd,omitempty"`
	Heading           string           `json:"heading,omitempty"`
	Body              string           `json:"body,omitempty"`
	BlockID           string           `json:"blockId,omitempty"`
	Collection        string           `json:"collection,omitempty"`
	OrderedFragments  []string         `json:"orderedFragments,omitempty"`
	PreviousFragments []string         `json:"previousFragments,omitempty"`
	OldTarget         string           `json:"oldTarget,omitempty"`
	NewTarget         string           `json:"newTarget,omitempty"`
	Property          string           `json:"property,omitempty"`
	Level             string           `json:"level,omitempty"`
}

type OntologyQuerySchemaResponse struct {
	SchemaPresent      bool   `json:"schemaPresent"`
	QuerySchemaPresent bool   `json:"querySchemaPresent"`
	SchemaHash         string `json:"schemaHash,omitempty"`
	SDL                string `json:"sdl,omitempty"`
	Error              string `json:"error,omitempty"`
}
