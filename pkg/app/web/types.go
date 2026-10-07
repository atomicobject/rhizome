package web

import (
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/search"
)

// StatusResponse reports basic server and indexing state.
//
// IndexState is "initializing" until the runtime gate opens, then flips to
// "ready" exactly once. Clients should treat the transition as a refetch
// trigger for index-dependent reads.
type StatusResponse struct {
	VaultName   string                `json:"vaultName"`
	VaultPath   string                `json:"vaultPath"`
	IndexPath   string                `json:"indexPath,omitempty"`
	Embeddings  bool                  `json:"embeddings"`
	CodeIndex   bool                  `json:"codeIndex"`
	Validation  ValidationStatusInfo  `json:"validation"`
	UpdatedAt   time.Time             `json:"updatedAt"`
	Ready       bool                  `json:"ready"`
	ReadyReason string                `json:"readyReason,omitempty"`
	IndexState  string                `json:"indexState"`
	Live        *bootstrap.LiveHealth `json:"live,omitempty"`
}

type ValidationStatusInfo struct {
	Status              string     `json:"status"`
	Health              string     `json:"health"`
	Generation          int64      `json:"generation"`
	PublishedGeneration int64      `json:"publishedGeneration"`
	ComputedAt          *time.Time `json:"computedAt,omitempty"`
	DurationMs          int64      `json:"durationMs,omitempty"`
	Error               string     `json:"error,omitempty"`
}

// PublicCapabilitiesResponse describes the stable v1 API contract available to
// custom applications. It is intentionally denormalized so clients can make one
// startup call before deciding which REST, SSE, and GraphQL surfaces to use.
type PublicCapabilitiesResponse struct {
	RhizomeVersion   string                 `json:"rhizomeVersion"`
	APIVersion       string                 `json:"apiVersion"`
	Vault            PublicVaultInfo        `json:"vault"`
	Status           StatusResponse         `json:"status"`
	GraphQL          PublicGraphQLInfo      `json:"graphql"`
	REST             PublicRESTInfo         `json:"rest"`
	Events           PublicEventsInfo       `json:"events"`
	QueryRecipes     PublicQueryRecipeInfo  `json:"queryRecipes"`
	Views            PublicViewsInfo        `json:"views"`
	Validation       PublicValidationInfo   `json:"validation"`
	Index            PublicIndexInfo        `json:"index"`
	Providers        PublicProviderInfo     `json:"providers"`
	EditSessions     PublicEditSessionsInfo `json:"editSessions"`
	Errors           PublicErrorsInfo       `json:"errors"`
	DegradedReasons  []string               `json:"degradedReasons,omitempty"`
	GeneratedAt      time.Time              `json:"generatedAt"`
	CompatibilityURL string                 `json:"compatibilityUrl,omitempty"`
}

type PublicVaultInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type PublicGraphQLInfo struct {
	Available      bool     `json:"available"`
	Endpoint       string   `json:"endpoint"`
	SchemaEndpoint string   `json:"schemaEndpoint"`
	ExplorerURL    string   `json:"explorerUrl,omitempty"`
	SchemaHash     string   `json:"schemaHash,omitempty"`
	OntologyHash   string   `json:"ontologyHash,omitempty"`
	Introspection  bool     `json:"introspection"`
	Operations     []string `json:"operations,omitempty"`
}

type PublicRESTInfo struct {
	BasePath  string   `json:"basePath"`
	OpenAPI   string   `json:"openapi"`
	Resources []string `json:"resources"`
}

type PublicEventsInfo struct {
	Endpoint          string   `json:"endpoint"`
	NodeEndpoint      string   `json:"nodeEndpoint,omitempty"`
	Format            string   `json:"format"`
	Replay            string   `json:"replay"`
	Heartbeat         string   `json:"heartbeat"`
	Reconnect         string   `json:"reconnect"`
	Invalidations     []string `json:"invalidations"`
	StreamedKinds     []string `json:"streamedKinds"`
	NodeStreamedKinds []string `json:"nodeStreamedKinds,omitempty"`
	SupportedKinds    []string `json:"supportedKinds"`
}

type PublicQueryRecipeInfo struct {
	Available  bool     `json:"available"`
	Generation string   `json:"generation,omitempty"`
	Count      int      `json:"count"`
	Issues     int      `json:"issues"`
	Endpoints  []string `json:"endpoints"`
}

type PublicViewsInfo struct {
	Available  bool     `json:"available"`
	Generation string   `json:"generation,omitempty"`
	Count      int      `json:"count"`
	Issues     int      `json:"issues"`
	Endpoints  []string `json:"endpoints"`
}

type PublicValidationInfo struct {
	Available       bool     `json:"available"`
	Endpoint        string   `json:"endpoint"`
	Endpoints       []string `json:"endpoints,omitempty"`
	RepairAvailable bool     `json:"repairAvailable"`
	RepairEndpoints []string `json:"repairEndpoints,omitempty"`
	Generation      string   `json:"generation,omitempty"`
	Status          string   `json:"status,omitempty"`
}

type PublicIndexInfo struct {
	Available  bool   `json:"available"`
	Generation string `json:"generation,omitempty"`
}

type PublicProviderInfo struct {
	Embeddings PublicProviderState `json:"embeddings"`
	CodeIndex  PublicProviderState `json:"codeIndex"`
}

type PublicProviderState struct {
	Available bool   `json:"available"`
	State     string `json:"state"`
}

type PublicEditSessionsInfo struct {
	Available bool     `json:"available"`
	Endpoints []string `json:"endpoints"`
}

type PublicErrorsInfo struct {
	Format string   `json:"format"`
	Codes  []string `json:"codes"`
}

type PublicQueryRecipeListResponse struct {
	Recipes []queryrecipe.Recipe `json:"recipes"`
	Issues  []queryrecipe.Issue  `json:"issues,omitempty"`
}

type PublicQueryRecipeExecuteRequest struct {
	Inputs map[string]string `json:"inputs,omitempty"`
}

// SuggestResponse returns fuzzy file suggestions.
type SuggestResponse struct {
	Query   string      `json:"query"`
	Matches []FileMatch `json:"matches"`
}

// FileMatch is a lightweight file match for suggestion/search UIs.
type FileMatch struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
	Kind  string `json:"kind"` // note|code|file
	Lang  string `json:"lang,omitempty"`
	Score int    `json:"score,omitempty"`
}

// SearchResponse mirrors semantic query results with lightweight matches.
type SearchResponse struct {
	Profile           searchapplication.Profile         `json:"profile,omitempty"`
	Policy            searchapplication.EffectivePolicy `json:"policy,omitempty"`
	Query             string                            `json:"query"`
	Returned          int                               `json:"returned,omitempty"`
	Total             int                               `json:"total,omitempty"`
	Count             int                               `json:"count"`
	ContinuationToken string                            `json:"continuationToken,omitempty"`
	Text              string                            `json:"text,omitempty"`
	Matches           []SearchMatch                     `json:"matches,omitempty"`
	Warnings          []search.Warning                  `json:"warnings,omitempty"`
	Lanes             []search.LaneStatus               `json:"lanes,omitempty"`
	TargetStatus      search.TargetStatus               `json:"targetStatus,omitempty"`
	Confidence        answer.ConfidenceReport           `json:"confidence,omitempty"`
	Coverage          answer.CoverageReport             `json:"coverage,omitempty"`
}

// SearchMatch is a trimmed semantic match payload for UI use.
type SearchMatch struct {
	Type             string                   `json:"type"`
	Path             string                   `json:"path,omitempty"`
	Title            string                   `json:"title,omitempty"`
	Symbol           string                   `json:"symbol,omitempty"`
	FQN              string                   `json:"fqn,omitempty"`
	AnchorID         string                   `json:"anchorID,omitempty"`
	NodeID           string                   `json:"nodeID,omitempty"`
	NodeRefJSON      string                   `json:"nodeRefJson,omitempty"`
	SourceLocator    string                   `json:"sourceLocator,omitempty"`
	NodeKind         string                   `json:"nodeKind,omitempty"`
	ParentNodeID     string                   `json:"parentNodeID,omitempty"`
	NodeRef          *ontology.NodeRef        `json:"nodeRef,omitempty"`
	LinkTarget       *ontology.NodeLinkTarget `json:"linkTarget,omitempty"`
	Kind             string                   `json:"kind,omitempty"`
	ChunkIndex       int                      `json:"chunkIndex,omitempty"`
	StartLine        int                      `json:"startLine,omitempty"`
	EndLine          int                      `json:"endLine,omitempty"`
	Score            float64                  `json:"score,omitempty"`
	Heading          string                   `json:"heading,omitempty"`
	Snippet          string                   `json:"snippet,omitempty"`
	SnippetKind      string                   `json:"snippetKind,omitempty"`
	SnippetStatus    string                   `json:"snippetStatus,omitempty"`
	SnippetTruncated bool                     `json:"snippetTruncated,omitempty"`
	NoteType         string                   `json:"noteType,omitempty"`
	Tags             []string                 `json:"tags,omitempty"`
}

// GraphResponse provides nodes/edges for visualization.
type GraphResponse struct {
	Nodes       []GraphNode                `json:"nodes"`
	Edges       []GraphEdge                `json:"edges"`
	Truncated   bool                       `json:"truncated,omitempty"`
	CenterID    string                     `json:"centerID,omitempty"`
	NeedsIndex  bool                       `json:"needsIndex,omitempty"`
	Diagnostics *noderead.GraphDiagnostics `json:"diagnostics,omitempty"`
}

// GraphNode represents a graph node (note, code, or module).
//
// ResolvedType is populated for `note` nodes when the vault has an ontology
// schema, and carries the note's resolved type name. The ontology workspace
// uses it to filter+color the graph by type; the general explorer ignores it.
type GraphNode struct {
	ID            string            `json:"id"`
	Path          string            `json:"path,omitempty"`
	NotePath      string            `json:"notePath,omitempty"`
	NodeID        string            `json:"nodeId,omitempty"`
	NodeRef       *ontology.NodeRef `json:"nodeRef,omitempty"`
	SourceLocator string            `json:"sourceLocator,omitempty"`
	Label         string            `json:"label"`
	Kind          string            `json:"kind"` // note|section|code|module
	Lang          string            `json:"lang,omitempty"`
	Module        string            `json:"module,omitempty"`
	Community     string            `json:"community,omitempty"`
	ResolvedType  string            `json:"resolvedType,omitempty"`
	Hub           float64           `json:"hub,omitempty"`
	Authority     float64           `json:"authority,omitempty"`
	PageRank      float64           `json:"pageRank,omitempty"`
	Score         float64           `json:"score,omitempty"`
	Collapsed     bool              `json:"collapsed,omitempty"`
	ChildCount    int               `json:"childCount,omitempty"`
}

// GraphEdge represents a graph edge.
type GraphEdge struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	Kind          string `json:"kind"`
	Weight        int    `json:"weight,omitempty"`
	RelationName  string `json:"relationName,omitempty"`
	RelationLabel string `json:"relationLabel,omitempty"`
	Provenance    string `json:"provenance,omitempty"`
	Structural    bool   `json:"structural,omitempty"`
}

// TreeResponse provides children for a directory.
type TreeResponse struct {
	Path      string      `json:"path"`
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// TreeEntry represents a file or directory in the tree.
type TreeEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"` // dir|note|code|file
	Lang        string `json:"lang,omitempty"`
	HasChildren bool   `json:"hasChildren"`
}

// FileViewResponse returns content + metadata for a file.
type FileViewResponse struct {
	Path         string                 `json:"path"`
	Kind         string                 `json:"kind"` // note|code|file
	Title        string                 `json:"title,omitempty"`
	Lang         string                 `json:"lang,omitempty"`
	Frontmatter  map[string]interface{} `json:"frontmatter,omitempty"`
	Content      string                 `json:"content,omitempty"`
	Links        []ResolvedLink         `json:"links,omitempty"`
	CodeRefs     []CodeRefLink          `json:"codeRefs,omitempty"`
	RelatedNotes []RelatedNote          `json:"relatedNotes,omitempty"`
}

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
	Type           *ontology.TypeDoc      `json:"type,omitempty"`
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
	Type          *ontology.TypeDoc `json:"type"`
	Count         int               `json:"count"`
	IssueCount    int               `json:"issueCount,omitempty"`
	StartingNotes []string          `json:"startingNotes,omitempty"`
}

// OntologyAtlasResponse is a single-fetch payload for the atlas hub: every
// note- or embedded-role type with counts plus interface TypeDocs so the UI
// can render the schema graph + type cards without an N+1 round-trip.
type OntologyAtlasResponse struct {
	SchemaPresent bool                     `json:"schemaPresent"`
	SchemaHash    string                   `json:"schemaHash,omitempty"`
	VaultName     string                   `json:"vaultName,omitempty"`
	Types         []OntologyAtlasTypeEntry `json:"types,omitempty"`
	Sections      []ontology.TypeDoc       `json:"sections,omitempty"`
	Interfaces    []ontology.TypeDoc       `json:"interfaces,omitempty"`
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

type RenderedFileResponse struct {
	Path         string                 `json:"path"`
	Title        string                 `json:"title"`
	ResolvedType string                 `json:"resolvedType,omitempty"`
	Frontmatter  map[string]interface{} `json:"frontmatter,omitempty"`
	Content      string                 `json:"content,omitempty"`
	Rendered     string                 `json:"rendered,omitempty"`
	Links        []ResolvedLink         `json:"links,omitempty"`
	Embeds       []RenderedEmbed        `json:"embeds,omitempty"`
	Sections     []RenderedSection      `json:"sections,omitempty"`
}

type RenderedEmbed struct {
	Target   string `json:"target"`
	Title    string `json:"title,omitempty"`
	Kind     string `json:"kind"`
	Preview  string `json:"preview,omitempty"`
	Resolved bool   `json:"resolved"`
}

type RenderedSection struct {
	ID       string                `json:"id"`
	Title    string                `json:"title"`
	Level    ontology.SectionLevel `json:"level"`
	Content  string                `json:"content,omitempty"`
	Children []RenderedSection     `json:"children,omitempty"`
	NotePath string                `json:"notePath,omitempty"`
	ParentID string                `json:"parentId,omitempty"`
	Locator  string                `json:"locator,omitempty"`
	BlockID  string                `json:"blockId,omitempty"`

	// TypeName is the ontology type this section is bound to, if any.
	// Populated via the web annotation pass when the enclosing note has a
	// resolved type whose schema declares a matching @contains field.
	TypeName string `json:"typeName,omitempty"`

	// FieldName is the immediate schema field that bound this section.
	// FieldPath is the dotted path from the note root (for example
	// `userStories.stories`).
	FieldName      string                  `json:"fieldName,omitempty"`
	FieldPath      string                  `json:"fieldPath,omitempty"`
	FieldList      bool                    `json:"fieldList,omitempty"`
	SectionDisplay ontology.SectionDisplay `json:"sectionDisplay,omitempty"`

	// Properties carries the inline `Key:: Value` pairs scoped to this
	// section's own content (descendant ranges subtracted). Keys are the
	// declared field names (not the raw inline keys); list fields are
	// joined with ", ". Populated only when the section has a typed shape.
	Properties      map[string]string `json:"properties,omitempty"`
	IdentifierField string            `json:"identifierField,omitempty"`

	// PreviewTemplate carries the *interpolated* template string — the
	// frontend renders it as markdown directly without re-resolving
	// property values. Empty unless the section type declares @preview.
	PreviewTemplate string `json:"previewTemplate,omitempty"`

	// Collapsed mirrors the @preview directive's `collapsed` argument;
	// the frontend uses it to decide whether the typed card starts open.
	Collapsed bool `json:"collapsed,omitempty"`
}

type StructuralViewResponse struct {
	Root        StructuralNodeResponse  `json:"root"`
	Tabs        []StructuralTabResponse `json:"tabs,omitempty"`
	DefaultView string                  `json:"defaultView,omitempty"`
}

type StructuralTabResponse struct {
	Key       string                   `json:"key"`
	Label     string                   `json:"label"`
	FieldName string                   `json:"fieldName,omitempty"`
	Count     int                      `json:"count"`
	Nodes     []StructuralNodeResponse `json:"nodes,omitempty"`
}

type StructuralNodeResponse struct {
	NodeID          string                   `json:"nodeId"`
	Fragment        string                   `json:"fragment,omitempty"`
	Title           string                   `json:"title"`
	TypeName        string                   `json:"typeName,omitempty"`
	Locator         string                   `json:"locator"`
	NotePath        string                   `json:"notePath"`
	ParentNodeID    string                   `json:"parentNodeId,omitempty"`
	FieldName       string                   `json:"fieldName,omitempty"`
	FieldPath       string                   `json:"fieldPath,omitempty"`
	FieldList       bool                     `json:"fieldList,omitempty"`
	SectionDisplay  ontology.SectionDisplay  `json:"sectionDisplay,omitempty"`
	Level           ontology.SectionLevel    `json:"level,omitempty"`
	Content         string                   `json:"content,omitempty"`
	Properties      map[string]string        `json:"properties,omitempty"`
	IdentifierField string                   `json:"identifierField,omitempty"`
	Preview         string                   `json:"preview,omitempty"`
	PreviewTemplate string                   `json:"previewTemplate,omitempty"`
	Collapsed       bool                     `json:"collapsed,omitempty"`
	Children        []StructuralNodeResponse `json:"children,omitempty"`
}

type NoteSearchResponse struct {
	Query   string            `json:"query"`
	Offset  int               `json:"offset"`
	Limit   int               `json:"limit"`
	Count   int               `json:"count"`
	Total   int               `json:"total"`
	Matches []NoteSearchMatch `json:"matches,omitempty"`
}

type NoteSearchMatch struct {
	Path         string   `json:"path"`
	Title        string   `json:"title"`
	ResolvedType string   `json:"resolvedType,omitempty"`
	Heading      string   `json:"heading,omitempty"`
	Snippet      string   `json:"snippet,omitempty"`
	Score        float64  `json:"score,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type NodeWorkspaceResponse struct {
	RequestedRef          string                          `json:"requestedRef"`
	FocusedNodeID         string                          `json:"focusedNodeId,omitempty"`
	Node                  NodeDescriptorResponse          `json:"node"`
	Content               NodeContentResponse             `json:"content"`
	Nodes                 []WorkspaceNodeResponse         `json:"nodes,omitempty"`
	Edges                 []WorkspaceEdgeResponse         `json:"edges,omitempty"`
	Views                 *WorkspaceViewsResponse         `json:"views,omitempty"`
	Fields                []ontology.NodeFieldState       `json:"fields,omitempty"`
	Collections           []ontology.NodeCollectionState  `json:"collections,omitempty"`
	Relations             []NoteWorkspaceGroup            `json:"relations,omitempty"`
	SectionRelationGroups map[string][]NoteWorkspaceGroup `json:"sectionRelationGroups,omitempty"`
	Loaded                NodeWorkspaceLoadedResponse     `json:"loaded"`
	Capabilities          ontology.NodeCapabilities       `json:"capabilities"`
	Status                ontology.NodeStatus             `json:"status"`
	Version               string                          `json:"version"`
	SourceRevision        ontology.NodeSourceRevision     `json:"sourceRevision"`
	NodeLocator           *ontology.NodeLocator           `json:"nodeLocator,omitempty"`
	LinkTarget            *ontology.NodeLinkTarget        `json:"linkTarget,omitempty"`
	LinkFixOps            []OntologyEditOp                `json:"linkFixOps,omitempty"`
}

type NodeWorkspaceLoadedResponse struct {
	Rendered   bool `json:"rendered"`
	Assessment bool `json:"assessment"`
	Structure  bool `json:"structure"`
	Relations  bool `json:"relations"`
}

type NodeDescriptorResponse struct {
	Ref          ontology.NodeRef      `json:"ref"`
	ResolvedType string                `json:"resolvedType,omitempty"`
	NotePath     string                `json:"notePath"`
	Title        string                `json:"title,omitempty"`
	Locator      string                `json:"locator"`
	NodeLocator  *ontology.NodeLocator `json:"nodeLocator,omitempty"`
	ParentRef    *ontology.NodeRef     `json:"parentRef,omitempty"`
}

type NodeContentResponse struct {
	Path                   string                          `json:"path"`
	Title                  string                          `json:"title"`
	ResolvedType           string                          `json:"resolvedType,omitempty"`
	Markdown               string                          `json:"markdown,omitempty"`
	Format                 noteformat.FormatID             `json:"format,omitempty"`
	SourceRepresentation   ontology.SourceRepresentation   `json:"sourceRepresentation,omitempty"`
	EvidenceRepresentation ontology.EvidenceRepresentation `json:"evidenceRepresentation,omitempty"`
	SourceCapabilities     []noteformat.Capability         `json:"sourceCapabilities,omitempty"`
	Rendered               *RenderedFileResponse           `json:"rendered,omitempty"`
	Assessment             *ontology.NoteAssessment        `json:"assessment,omitempty"`
	TypeDoc                *ontology.TypeDoc               `json:"typeDoc,omitempty"`
	Structural             *StructuralViewResponse         `json:"structural,omitempty"`
}

type WorkspaceNodeKind string

const (
	WorkspaceNodeKindNote       WorkspaceNodeKind = "note"
	WorkspaceNodeKindSection    WorkspaceNodeKind = "section"
	WorkspaceNodeKindEmbedded   WorkspaceNodeKind = "embedded"
	WorkspaceNodeKindField      WorkspaceNodeKind = "field"
	WorkspaceNodeKindCollection WorkspaceNodeKind = "collection"
)

type WorkspaceNodeResponse struct {
	ID           string                    `json:"id"`
	Kind         WorkspaceNodeKind         `json:"kind"`
	Ref          ontology.NodeRef          `json:"ref"`
	NotePath     string                    `json:"notePath"`
	ParentID     string                    `json:"parentId,omitempty"`
	ChildIDs     []string                  `json:"childIds,omitempty"`
	Status       ontology.NodeStatus       `json:"status"`
	Capabilities ontology.NodeCapabilities `json:"capabilities"`
	Version      string                    `json:"version,omitempty"`
	NodeLocator  *ontology.NodeLocator     `json:"nodeLocator,omitempty"`
	// Body is the ordered list of body blocks (narrative / inline_field /
	// child_section / collection) that make up this node's rendered body.
	// Populated for note/section/embedded nodes; empty for field/collection
	// synthetic nodes. Field and child-section blocks reference other nodes
	// via their canonical NodeRef, resolved against workspace.nodes[].
	Body []ontology.NodeBodyBlock `json:"body,omitempty"`
	// Data carries the uniform content payload for note/section/embedded
	// nodes. The three were previously modeled as separate types, but they
	// share the same shape (title + level + locator + blockId + fragment +
	// markdown + binding) and should be rendered uniformly on the client.
	// `Kind` stays on the response as an informational tag for callers that
	// route edit ops differently (e.g. block-id generation for embedded
	// types), but rendering must never branch on it.
	Data       *WorkspaceNodeData           `json:"data,omitempty"`
	Field      *WorkspaceFieldNodeData      `json:"field,omitempty"`
	Collection *WorkspaceCollectionNodeData `json:"collection,omitempty"`
}

// WorkspaceNodeData is the uniform payload for note/section/embedded nodes.
// All fields are optional — NOTE nodes omit Level/BlockID/Fragment/Binding;
// section/embedded nodes populate them as the source provides. Consumers
// should read these fields without switching on Kind.
type WorkspaceNodeData struct {
	Title        string                       `json:"title,omitempty"`
	ResolvedType string                       `json:"resolvedType,omitempty"`
	Markdown     string                       `json:"markdown,omitempty"`
	Locator      string                       `json:"locator,omitempty"`
	Level        ontology.SectionLevel        `json:"level,omitempty"`
	BlockID      string                       `json:"blockId,omitempty"`
	Fragment     string                       `json:"fragment,omitempty"`
	Binding      *WorkspaceSectionBindingData `json:"binding,omitempty"`
}

type WorkspaceSectionBindingData struct {
	TypeName        string                  `json:"typeName,omitempty"`
	FieldName       string                  `json:"fieldName,omitempty"`
	FieldPath       string                  `json:"fieldPath,omitempty"`
	FieldList       bool                    `json:"fieldList,omitempty"`
	SectionDisplay  ontology.SectionDisplay `json:"sectionDisplay,omitempty"`
	Properties      map[string]string       `json:"properties,omitempty"`
	IdentifierField string                  `json:"identifierField,omitempty"`
	PreviewTemplate string                  `json:"previewTemplate,omitempty"`
	Collapsed       bool                    `json:"collapsed,omitempty"`
}

type WorkspaceFieldNodeData struct {
	Name string `json:"name"`
	// ValueKind is a coarse shape hint (scalar/list/section-ref).
	ValueKind string `json:"valueKind"`
	// TypeName is the schema-declared type name for the field (e.g. "String",
	// "SpecStatus", "Date"). Empty when the owner note has no resolved type or
	// the field is not declared in the ontology.
	TypeName string `json:"typeName,omitempty"`
	// EnumValues is the ordered set of admitted values when TypeName names an
	// ontology enum. Empty for non-enum fields.
	EnumValues          []string                       `json:"enumValues,omitempty"`
	Identifier          bool                           `json:"identifier,omitempty"`
	PreferredIdentifier bool                           `json:"preferredIdentifier,omitempty"`
	Present             bool                           `json:"present"`
	Values              []string                       `json:"values,omitempty"`
	Range               ontology.NodeRange             `json:"range"`
	ValueRanges         []ontology.NodeRange           `json:"valueRanges,omitempty"`
	InlineSpans         []ontology.NodeInlineFieldSpan `json:"inlineSpans,omitempty"`
	SectionRefs         []ontology.NodeRef             `json:"sectionRefs,omitempty"`
}

type WorkspaceCollectionNodeData struct {
	Name             string                             `json:"name"`
	ItemRefs         []ontology.NodeCollectionItemState `json:"itemRefs,omitempty"`
	OrderFingerprint string                             `json:"orderFingerprint,omitempty"`
	Range            ontology.NodeRange                 `json:"range"`
}

type WorkspaceEdgeKind string

const (
	WorkspaceEdgeKindContains       WorkspaceEdgeKind = "contains"
	WorkspaceEdgeKindBindsField     WorkspaceEdgeKind = "binds_field"
	WorkspaceEdgeKindCollectionItem WorkspaceEdgeKind = "collection_item"
	WorkspaceEdgeKindRelatesTo      WorkspaceEdgeKind = "relates_to"
)

type WorkspaceEdgeResponse struct {
	Kind           WorkspaceEdgeKind `json:"kind"`
	FromID         string            `json:"fromId"`
	ToID           string            `json:"toId"`
	FieldName      string            `json:"fieldName,omitempty"`
	CollectionName string            `json:"collectionName,omitempty"`
	RelationKey    string            `json:"relationKey,omitempty"`
	RelationLabel  string            `json:"relationLabel,omitempty"`
	ScopeNodeID    string            `json:"scopeNodeId,omitempty"`
	Index          int               `json:"index,omitempty"`
}

type WorkspaceViewsResponse struct {
	RenderedOutline   *WorkspaceRenderedOutlineViewResponse   `json:"renderedOutline,omitempty"`
	StructuralOutline *WorkspaceStructuralOutlineViewResponse `json:"structuralOutline,omitempty"`
	RelationGroups    []WorkspaceRelationGroupsViewResponse   `json:"relationGroups,omitempty"`
}

type WorkspaceRenderedOutlineViewResponse struct {
	RootIDs []string `json:"rootIds,omitempty"`
}

type WorkspaceStructuralOutlineViewResponse struct {
	RootID      string                               `json:"rootId,omitempty"`
	DefaultView string                               `json:"defaultView,omitempty"`
	Tabs        []WorkspaceStructuralTabViewResponse `json:"tabs,omitempty"`
}

type WorkspaceStructuralTabViewResponse struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	FieldName string   `json:"fieldName,omitempty"`
	Count     int      `json:"count"`
	NodeIDs   []string `json:"nodeIds,omitempty"`
}

type WorkspaceRelationGroupsViewResponse struct {
	ScopeNodeID string               `json:"scopeNodeId,omitempty"`
	Groups      []NoteWorkspaceGroup `json:"groups,omitempty"`
}

type NoteWorkspaceGroup struct {
	Key        string              `json:"key"`
	Label      string              `json:"label"`
	OwnerTitle string              `json:"ownerTitle,omitempty"`
	Navigation bool                `json:"navigation,omitempty"`
	Items      []NoteWorkspaceLink `json:"items,omitempty"`
}

type NoteWorkspaceLink struct {
	Path           string                  `json:"path"`
	Title          string                  `json:"title"`
	TargetTitle    string                  `json:"targetTitle,omitempty"`
	Kind           string                  `json:"kind"`
	ResolvedType   string                  `json:"resolvedType,omitempty"`
	Anchor         string                  `json:"anchor,omitempty"`
	StructuralNode *StructuralNodeResponse `json:"structuralNode,omitempty"`
	RelationName   string                  `json:"relationName,omitempty"`
	Provenance     string                  `json:"provenance,omitempty"`
	Direction      string                  `json:"direction,omitempty"`
	Structural     bool                    `json:"structural,omitempty"`
	Current        bool                    `json:"current,omitempty"`
	Score          float64                 `json:"score,omitempty"`
	Summary        string                  `json:"summary,omitempty"`
}

// Direction values for NoteWorkspaceLink: whether the focused note points at
// the target or the target points at the focused note.
const (
	linkDirectionOutgoing = "outgoing"
	linkDirectionIncoming = "incoming"
)

// workspaceLinkDirection reports which way the authored link runs given the
// edge's orientation relative to the focused note. The index mirrors every
// body link A→B with a synthetic B→A edge whose provenance is "backlink".
func workspaceLinkDirection(inbound bool, provenance string) string {
	if inbound != (provenance == "backlink") {
		return linkDirectionIncoming
	}
	return linkDirectionOutgoing
}

// ResolvedLink represents a parsed markdown or wikilink.
type ResolvedLink struct {
	Target string `json:"target"`
	Text   string `json:"text,omitempty"`
	Kind   string `json:"kind"` // wikilink|markdown
	Anchor string `json:"anchor,omitempty"`
}

// CodeRefLink represents a coderef discovered in code comments.
type CodeRefLink struct {
	Target  string `json:"target"`
	Kind    string `json:"kind"` // wikilink|mention
	Line    int    `json:"line"`
	Snippet string `json:"snippet,omitempty"`
}

// RelatedNote represents a note linked to a code file (coderefs or code anchors).
type RelatedNote struct {
	Path        string `json:"path"`
	Title       string `json:"title,omitempty"`
	Reason      string `json:"reason,omitempty"`      // coderef|codeanchor
	AnchorLabel string `json:"anchorLabel,omitempty"` // for codeanchor matches
}

// ErrorResponse is returned on API failures.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}
