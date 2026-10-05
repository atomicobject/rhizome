package noderead

import (
	"context"
	"slices"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Store is the minimum indexed-read contract needed by a Scope.
//
// Implementations are expected to batch by paths/sources and return current
// index rows. Live projection fallback stays in Scope, not in the store.
type Store interface {
	CurrentNoteMetadataRows(context.Context) ([]semdb.NoteMetadataRow, error)
	CurrentNoteMetadataRowsByPaths(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
	CurrentNotePropertyValues(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
	CurrentNoteTags(context.Context, []string) ([]semdb.NoteTagRow, error)
	OntologyAssessmentsByPaths(context.Context, []string) (map[string]semdb.OntologyNoteAssessmentRow, error)
	OntologyAssessmentFlags(context.Context) (map[string]semdb.OntologyAssessmentFlags, error)
	GetOntologySchemaState(context.Context) (semdb.OntologySchemaState, error)
	OntologyTypesByPaths(context.Context, []string) (map[string]semdb.OntologyNoteTypeRow, error)
	OntologyPathsByType(context.Context, string, int) ([]string, error)
	OntologyEdgesForPaths(context.Context, []string, bool, string, int) ([]semdb.OntologyEdgeRow, error)
	OntologyStructuralEdgesBySources(context.Context, []string, string, int) ([]semdb.OntologyEdgeRow, error)
	OntologyAmbientEdgesBySources(context.Context, []string, string, int) ([]semdb.OntologyEdgeRow, error)
}

// CatalogStore extends Store with first-class ontology catalog rows.
//
// Catalog rows are the fast path for embedded and section NodeRef identity.
type CatalogStore interface {
	Store
	AllOntologyNodes(context.Context) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodesByIDs(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error)
	OntologyNodesByPaths(context.Context, []string) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodesByType(context.Context, string) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodesByTypePlan(context.Context, codeanchor.OntologyNodeQueryPlan) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodesBySourceLocators(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error)
	OntologyNodesByNoteFragments(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error)
	OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]codeanchor.IntelOntologyNodeFieldValue, error)
}

// GraphStore is the storage boundary for noderead-owned graph assembly.
//
// Callers should consume Scope.Graph or Scope.GraphFacts instead of joining
// ontology and fallback graph tables directly.
type GraphStore interface {
	Store
	readmodel.GraphStore
}

// NoteAliasesProvider exposes indexed note aliases keyed by note path.
type NoteAliasesProvider interface {
	CurrentNoteAliases(context.Context) (map[string][]string, error)
}

// Service owns the stable dependencies used to create request-scoped Scopes.
//
// Service is intentionally thin: caches that depend on request shape,
// hydration profile, or graph flags live on Scope so concurrent jobs do not
// share stale or over-broad reads.
type Service struct {
	VaultDef    obsidian.VaultDefinition
	NoteReader  obsidian.NoteReader
	Store       Store
	Schema      *ontology.Schema
	NoteFormats noteformat.Runtime
	// ExactNoteMetadataRows optionally bypasses the global metadata-ready bit
	// after application composition has checked provider and path ownership.
	ExactNoteMetadataRows func(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
	// ExactNotePropertyValues and ExactNoteTags load derived facts only for
	// paths already admitted by ExactNoteMetadataRows. They preserve complete
	// raw-note hydration while unrelated notes are reconciling.
	ExactNotePropertyValues func(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
	ExactNoteTags           func(context.Context, []string) ([]semdb.NoteTagRow, error)
	SharedCache             SharedNodeCache
	// ApplyLinkTargets is an explicit live-writer capability. Its owner holds
	// source mutation and metadata/ontology publication under the index lock.
	// The Store and NoteReader must observe that same live vault; published
	// read bundles and preview scopes must never receive this capability.
	ApplyLinkTargets func(context.Context, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error)
}

// SharedNodeCache is the optional process-level cache seam for node records.
//
// Scope remains the authoritative cache boundary today. Implementations of
// this interface must be invalidation-aware before they are safe to use broadly.
type SharedNodeCache interface {
	Get(NodeCacheKey) (CachedNode, bool)
	Set(NodeCacheKey, CachedNode, NodeCacheMeta)
	InvalidatePaths([]string)
	InvalidateAll(string)
}

// NodeCacheKey identifies a cached node record by canonical NodeRef plus the
// hydration profile that shaped the record.
//
// Do not reduce this to NodeRef.String: locator strings collapse note roots,
// headings, block IDs, structural refs, and embedded IDs that can require
// different projection/cache behavior.
type NodeCacheKey struct {
	Ref     ontology.NodeRef
	Profile HydrateProfile
}

// CachedNode wraps a hydrated record stored outside one Scope.
type CachedNode struct {
	Record NodeRecord
}

// NodeCacheMeta carries freshness inputs needed by shared cache users.
type NodeCacheMeta struct {
	SchemaHash string
	UpdatedAt  int64
}

// ScopeOptions configures one request/job Scope.
type ScopeOptions struct {
	// Budget is a caller-level guardrail for future expensive expansions. Today
	// most methods also accept explicit limits; keep new fan-out bounded by the
	// narrower of the request limit and this scope budget.
	Budget int
	// ReadOverlay is an optional request/session-local preview layer. It is
	// intended for web edit-session reads: touched notes project from preview
	// content while untouched notes continue using the indexed catalog/read cache.
	ReadOverlay *ReadOverlay
}

// ReadOverlay supplies one provider's preview-state content for a bounded set
// of touched notes. Markdown projection is allowed only when SourceFormat is
// exactly markdown; callers must not use a filename suffix as that proof.
//
// It does not own edits or session restore. Callers build it from a write-layer
// preview and pass it into noderead so Hydrate, TypeInstances, filters, sort, and
// paging observe one read contract.
type ReadOverlay struct {
	SourceFormat         noteformat.FormatID
	Notes                map[string]ReadOverlayNote
	UpdatedContentByPath map[string]string
	TouchedPaths         []string
	Index                *ReadOverlayIndex
	Status               string
	Conflicted           bool
	Rebased              bool
}

func (o *ReadOverlay) IsMarkdownSource() bool {
	return o != nil && o.SourceFormat == noteformat.FormatID("markdown")
}

func (o *ReadOverlay) Empty() bool {
	return o == nil || (o.Index == nil && len(o.Notes) == 0 && len(o.UpdatedContentByPath) == 0 && len(o.TouchedPaths) == 0)
}

// TouchesPath reports whether the overlay stages content for a note path.
func (o *ReadOverlay) TouchesPath(notePath string) bool {
	if o == nil || notePath == "" {
		return false
	}
	if o.Index != nil {
		if _, ok := o.Index.TouchedPaths[notePath]; ok {
			return true
		}
	}
	if _, ok := o.Notes[notePath]; ok {
		return true
	}
	if _, ok := o.UpdatedContentByPath[notePath]; ok {
		return true
	}
	return slices.Contains(o.TouchedPaths, notePath)
}

// StagedContent returns the staged source of a note the overlay touches; ok
// is false when the overlay leaves the note committed. A deleted note returns
// empty content.
func (o *ReadOverlay) StagedContent(notePath string) (content string, ok bool) {
	if o == nil || notePath == "" {
		return "", false
	}
	if note, found := o.Notes[notePath]; found {
		if note.Deleted {
			return "", true
		}
		return note.Content, true
	}
	if content, found := o.UpdatedContentByPath[notePath]; found {
		return content, true
	}
	if o.Index != nil {
		if snapshot := o.Index.SnapshotsByPath[notePath]; snapshot != nil {
			return snapshot.Content, true
		}
	}
	return "", false
}

// ReadOverlayNote represents one touched note in a ReadOverlay.
type ReadOverlayNote struct {
	Content   string
	Deleted   bool
	UpdatedAt int64
}

// ReadOverlayIndex is the reusable preview read model for touched edit-session
// documents. It is immutable after construction so web/session owners can share
// one index across request scopes for the same edit-session revision.
type ReadOverlayIndex struct {
	TouchedPaths           map[string]struct{}
	DeletedPaths           map[string]struct{}
	SnapshotsByPath        map[string]*ontology.DocumentSnapshot
	RecordsByRef           map[string]NodeRecord
	ItemsByType            map[string][]ontology.NodeListItem
	ItemKeysByTouchedPath  map[string]map[string]struct{}
	FieldValuesByRef       map[string]map[string][]string
	ProjectionCountByPath  map[string]int
	BuildProjectionCount   int
	BuildTouchedPathCount  int
	BuildOverlayRecordKeys []string
}

// Scope batches and memoizes ontology reads for one request, job, or worker
// batch.
//
// A Scope may be called concurrently; methods serialize access to its maps.
// Prefer creating independent scopes for independent concurrent work so cache
// contents and diagnostics remain bounded to one caller's intent.
type Scope struct {
	service         *Service
	opts            ScopeOptions
	mu              sync.Mutex
	cacheGeneration uint64 // fences builders that release mu before publication

	noteStateLoaded bool
	noteRows        []semdb.NoteMetadataRow
	noteRowByPath   map[string]semdb.NoteMetadataRow
	noteRowKnown    map[string]bool
	allNotePaths    []string
	issueByPath     map[string]bool
	flagsByPath     map[string]semdb.OntologyAssessmentFlags

	recordByPath map[string]NodeRecord
	recordKnown  map[string]bool
	contentKnown map[string]bool
	recordByRef  map[NodeCacheKey]NodeRecord
	refKnown     map[NodeCacheKey]bool

	locatorByRef map[string]ontology.NodeLocator
	locatorKnown map[string]bool

	cachedNotePathCache *obsidian.NotePathCache
	notePathCacheLoaded bool
	relationEntries     []obsidian.NoteEntry
	relationEntriesErr  error
	relationEntriesRead bool

	projectionByRef  map[string]*ontology.NodeProjection
	projectionKnown  map[string]bool
	snapshotByPath   map[string]*ontology.DocumentSnapshot
	snapshotKnownErr map[string]error
	overlayIndex     *ReadOverlayIndex

	typeRowsByPath map[string]semdb.OntologyNoteTypeRow
	typeRowsKnown  map[string]bool

	assessmentByPath map[string]*ontology.NoteAssessment
	assessmentKnown  map[string]bool

	traverseByKey     map[traverseCacheKey]TraverseResult
	graphByKey        map[string]GraphResult
	graphFactsByKey   map[string]GraphFactsResult
	typeInstancesByID map[string]ontology.TypeListResult

	embeddedReady  bool
	embeddedByType map[string][]ontology.NodeListItem
	embeddedSeen   map[string]map[string]struct{}

	diagnostics Diagnostics
}

type traverseCacheKey struct {
	structural     bool
	relation       string
	provenance     string
	dstType        string
	limitPerSource int
}

// HydrateProfile controls how much note/node data Hydrate should materialize.
type HydrateProfile string

const (
	// HydrateIdentity returns enough data to preserve canonical node identity.
	HydrateIdentity HydrateProfile = "identity"
	// HydrateSummary adds display metadata such as title, tags, type, and issues.
	HydrateSummary HydrateProfile = "summary"
	// HydrateContent includes authored note content when the caller needs it.
	HydrateContent HydrateProfile = "content"
	// HydrateWorkspace preserves projection data for node-workspace consumers.
	HydrateWorkspace HydrateProfile = "workspace"
)

// HydrateOptions selects the node materialization profile for Hydrate-like APIs.
type HydrateOptions struct {
	Profile HydrateProfile
}

// NodeRecord is the hydrated read-side representation of a note, section, or
// embedded ontology node.
type NodeRecord struct {
	Ref                    ontology.NodeRef
	Path                   string
	Title                  string
	Content                string
	Frontmatter            map[string]any
	InlineProps            map[string][]string
	FieldValues            map[string][]codeanchor.IntelOntologyNodeFieldValue
	Tags                   []string
	TypeName               string
	Format                 noteformat.FormatID
	SourceRepresentation   ontology.SourceRepresentation
	EvidenceRepresentation ontology.EvidenceRepresentation
	Capabilities           []noteformat.Capability
	NodeLocator            *ontology.NodeLocator
	LinkTarget             *ontology.NodeLinkTarget
	UpdatedAt              int64
	HasIssues              bool
	// resolvedFrom records request refs that projection verified against this
	// canonical record in the current hydration call.
	resolvedFrom     []ontology.NodeRef
	projectedFields  map[string]projectedFieldValues
	catalogNodeID    string
	catalogStartByte int
}

type projectedFieldValues struct {
	values  []string
	present bool
}

// NodeTarget is a user-facing locator string to resolve into a canonical NodeRef.
type NodeTarget struct {
	Input string
}

// ResolveRequest resolves one or more locator strings and optionally verifies
// durable link targets for embedded nodes.
type ResolveRequest struct {
	// OmitLocators skips author-facing link rendering when only canonical refs
	// and hydrated records are needed. Locator fields and link-fix diagnostics
	// are unavailable in this mode; resolution errors and ambiguity remain.
	// Explicit link-target planning or apply still computes locators so this
	// option cannot suppress required fixes.
	OmitLocators bool
	// IndexOnly rejects fragment targets missing from the catalog instead of
	// projecting note content to resolve them.
	IndexOnly        bool
	Targets          []NodeTarget
	EnsureLinkTarget ontology.EnsureLinkTargetMode
	FromPath         string
	Hydrate          HydrateOptions
}

// ResolveResult returns canonical refs, hydrated records, locators, and any
// safe link-fix plan needed to make embedded targets durable.
type ResolveResult struct {
	Resolved    []ResolvedNode
	Diagnostics []NodeResolveDiagnostic
	FixPlan     *ontology.NodeLinkFixPlan
}

// ResolvedNode binds one input target to its canonical NodeRef and locator.
type ResolvedNode struct {
	Input   string
	Ref     ontology.NodeRef
	Record  NodeRecord
	Locator ontology.NodeLocator
}

// NodeResolveDiagnostic explains why a target could not be resolved cleanly or
// why a fix plan is required.
type NodeResolveDiagnostic struct {
	Input      string             `json:"input,omitempty"`
	Code       string             `json:"code"`
	Message    string             `json:"message"`
	Candidates []ontology.NodeRef `json:"candidates,omitempty"`
}

// TypeInstancesRequest lists all indexed notes or embedded nodes of a type.
//
// When Predicates or Sort are supplied, callers route through the indexed plan
// path so filters and sort are applied before the limit cap. Predicates and
// Sort use the same shape the GraphQL surface emits, so view-layer pushdown
// stays consistent across note-root and embedded-node sources.
type TypeInstancesRequest struct {
	TypeName   string
	Limit      int
	Offset     int
	Predicates []codeanchor.OntologyFieldPredicate
	Sort       []codeanchor.OntologyFieldSort
}

// TraverseRequest fetches indexed relation rows grouped by source note path.
//
// It is the low-level batch edge primitive; use Neighborhood or Expand when
// node-scoped endpoints, directions, hydration, or budgets matter.
type TraverseRequest struct {
	Sources        []ontology.NodeRef
	Relation       string
	Provenance     map[string]struct{}
	DstType        string
	Structural     bool
	LimitPerSource int
}

// TraverseResult groups raw ontology edge rows by original source path.
type TraverseResult struct {
	EdgesBySource map[string][]semdb.OntologyEdgeRow
}

// TraversalDirection controls whether graph reads follow outbound, inbound, or
// both directions relative to the supplied source refs.
type TraversalDirection string

const (
	// TraversalDirectionOutbound follows edges from source to target.
	TraversalDirectionOutbound TraversalDirection = "outbound"
	// TraversalDirectionInbound follows edges from target back to source.
	TraversalDirectionInbound TraversalDirection = "inbound"
	// TraversalDirectionBoth includes inbound and outbound edge matches.
	TraversalDirectionBoth TraversalDirection = "both"
)

// NeighborhoodRequest returns one-hop typed relations around many NodeRefs.
//
// Unlike Traverse, it preserves embedded-node source identity and can hydrate
// related target records in the same request-scoped cache.
type NeighborhoodRequest struct {
	Sources           []ontology.NodeRef
	Direction         TraversalDirection
	RelationNames     []string
	Provenance        map[string]struct{}
	IncludeStructural bool
	IncludeAmbient    bool
	TargetTypes       []string
	TargetInterfaces  []string
	SourceKinds       []ontology.NodeKind
	TargetKinds       []ontology.NodeKind
	FirstPerSource    int
	FirstTotal        int
	Hydrate           HydrateOptions
}

// NeighborhoodResult is the grouped and flattened one-hop relation view.
type NeighborhoodResult struct {
	Sources  []NeighborhoodSourceResult
	BySource map[string]NeighborhoodSourceResult
	Edges    []NeighborhoodEdge
	Nodes    []NodeRecord
}

// RelationCountsRequest asks for cheap relation counts without hydrating nodes.
//
// It is intended for list and summary surfaces that need density hints.
type RelationCountsRequest struct {
	Sources           []ontology.NodeRef
	Direction         TraversalDirection
	RelationNames     []string
	Provenance        map[string]struct{}
	IncludeStructural bool
	IncludeAmbient    bool
	TargetTypes       []string
	TargetInterfaces  []string
	SourceKinds       []ontology.NodeKind
	TargetKinds       []ontology.NodeKind
	Selections        []RelationSelection
}

// RelationCountsResult returns stable counts in request order plus lookup form.
type RelationCountsResult struct {
	Sources  []RelationCountSourceResult
	BySource map[string]RelationCountSourceResult
}

// RelationCountSourceResult is one source ref's matching relation count.
type RelationCountSourceResult struct {
	Source ontology.NodeRef
	Count  int
}

// NeighborhoodSourceResult groups the edge slice for a single source ref.
type NeighborhoodSourceResult struct {
	Source    ontology.NodeRef
	Edges     []NeighborhoodEdge
	Truncated bool
}

// NeighborhoodEdge is a normalized relation edge with canonical NodeRef
// endpoints and the original indexed row for callers that need raw evidence.
type NeighborhoodEdge struct {
	Source       ontology.NodeRef
	Target       ontology.NodeRef
	Edge         semdb.OntologyEdgeRow
	Direction    TraversalDirection
	RelationName string
	Provenance   string
	Structural   bool
	TargetType   string
	Depth        int
}

// GraphEndpointKind identifies the endpoint family used in graph results.
type GraphEndpointKind string

const (
	// GraphEndpointNote is a note-root endpoint.
	GraphEndpointNote GraphEndpointKind = "note"
	// GraphEndpointEmbedded is a first-class embedded ontology node endpoint.
	GraphEndpointEmbedded GraphEndpointKind = "embedded"
	// GraphEndpointSection is a structural section endpoint.
	GraphEndpointSection GraphEndpointKind = "section"
	// GraphEndpointCode is a code-file or code-symbol endpoint from fallback graph evidence.
	GraphEndpointCode GraphEndpointKind = "code"
)

// GraphEdgeKind names noderead's normalized graph edge families.
type GraphEdgeKind string

const (
	// GraphEdgeKindOntology is a typed relation from indexed ontology edges.
	GraphEdgeKindOntology GraphEdgeKind = "ontology"
	// GraphEdgeKindEmbeds links a note or section endpoint to an embedded child.
	GraphEdgeKindEmbeds GraphEdgeKind = "embeds"
	// GraphEdgeKindWikilink is fallback markdown graph evidence.
	GraphEdgeKindWikilink GraphEdgeKind = "wikilink"
	// GraphEdgeKindCodeRef is fallback note-code evidence.
	GraphEdgeKindCodeRef GraphEdgeKind = "coderef"
)

// GraphEndpoint is a graph node with both display identity and canonical
// NodeRef/source-locator identity when the endpoint comes from ontology data.
type GraphEndpoint struct {
	ID            string
	Ref           ontology.NodeRef
	Kind          GraphEndpointKind
	Path          string
	NotePath      string
	NodeID        string
	TypeName      string
	Label         string
	SourceLocator string
	ParentID      string
}

// GraphReadEdge is the normalized internal edge shape returned by Graph.
//
// It preserves typed ontology relation metadata and fallback graph evidence
// without forcing all consumers into the heavier GraphFactEdge payload.
type GraphReadEdge struct {
	Source        string
	Target        string
	Kind          string
	RelationName  string
	RelationLabel string
	Provenance    string
	Structural    bool
	Weight        int
	Confidence    float64
}

// GraphProfile selects the default mix of ontology, doc-link, untyped, and
// code endpoints for Graph.
type GraphProfile string

const (
	// GraphProfileOntologyNative returns typed ontology endpoints and edges only.
	GraphProfileOntologyNative GraphProfile = "ontology_native"
	// GraphProfileNotesOnly adds untyped notes and markdown doc-link fallback edges.
	GraphProfileNotesOnly GraphProfile = "notes_only"
	// GraphProfileCodeAware adds note-code evidence but still requires explicit
	// IncludeCodeEdges for code-code calls/imports/type edges.
	GraphProfileCodeAware GraphProfile = "code_aware"
)

// GraphRequest asks Scope.Graph to assemble a profile-aware graph from indexed
// ontology rows plus permitted fallback doc/code graph rows.
//
// Embedded or section NodeRef sources are endpoint-scoped: unresolved endpoints
// must not silently widen to the whole parent note.
type GraphRequest struct {
	Sources          []ontology.NodeRef
	Paths            []string
	PathPrefixes     []string
	Profile          GraphProfile
	Limit            int
	NodeLimit        int
	EdgeLimit        int
	IncludeCodeEdges bool
	Diagnostics      bool
}

// GraphResult is the profile-aware graph model used by browser, CLI, search,
// and MCP consumers.
type GraphResult struct {
	Nodes       []GraphEndpoint
	Edges       []GraphReadEdge
	Diagnostics *GraphDiagnostics
}

// GraphFactsRequest asks for the consumer-friendly graph fact envelope.
//
// It filters Graph output into explicit ontology/doc/code/call/embed families
// and carries richer endpoint metadata for search, graph-path, and diagnostics.
type GraphFactsRequest struct {
	Sources         []ontology.NodeRef
	Paths           []string
	PathPrefixes    []string
	NodeLimit       int
	EdgeLimit       int
	IncludeOntology bool
	IncludeDocLinks bool
	IncludeCode     bool
	IncludeCalls    bool
	IncludeEmbedded bool
	Diagnostics     bool
}

// GraphFactsResult is a graph result where each edge carries endpoint metadata
// needed by consumers that cannot look up GraphEndpoint rows separately.
type GraphFactsResult struct {
	Nodes       []GraphEndpoint
	Edges       []GraphFactEdge
	Diagnostics *GraphDiagnostics
}

// GraphFactEdge is a denormalized edge fact with endpoint refs, locators,
// type names, weights, and provenance.
type GraphFactEdge struct {
	Source         string
	Target         string
	SourceRef      ontology.NodeRef
	TargetRef      ontology.NodeRef
	SourcePath     string
	TargetPath     string
	SourceKind     GraphEndpointKind
	TargetKind     GraphEndpointKind
	Kind           string
	RelationName   string
	RelationLabel  string
	Provenance     string
	Structural     bool
	Weight         float64
	Confidence     float64
	SourceLocator  string
	TargetLocator  string
	SourceNodeID   string
	TargetNodeID   string
	SourceTypeName string
	TargetTypeName string
}

// GraphCodeLink summarizes note-code or code-note graph evidence incident to a
// source NodeRef.
type GraphCodeLink struct {
	Path       string
	Kind       string
	Provenance string
	Weight     int
}

// GraphDiagnostics explains graph assembly decisions without changing result
// semantics.
type GraphDiagnostics struct {
	Nodes  []GraphNodeDiagnostic   `json:"nodes,omitempty"`
	Edges  []GraphEdgeDiagnostic   `json:"edges,omitempty"`
	Skips  []GraphSkipDiagnostic   `json:"skips,omitempty"`
	Dedupe []GraphDedupeDiagnostic `json:"dedupe,omitempty"`
}

// GraphNodeDiagnostic records why an endpoint was added.
type GraphNodeDiagnostic struct {
	ID      string           `json:"id,omitempty"`
	Source  string           `json:"source,omitempty"`
	Reason  string           `json:"reason,omitempty"`
	Ref     ontology.NodeRef `json:"ref,omitempty"`
	Locator string           `json:"locator,omitempty"`
}

// GraphEdgeDiagnostic records why an edge was added.
type GraphEdgeDiagnostic struct {
	Source       string `json:"source,omitempty"`
	Target       string `json:"target,omitempty"`
	Kind         string `json:"kind,omitempty"`
	RelationName string `json:"relationName,omitempty"`
	Provenance   string `json:"provenance,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// GraphSkipDiagnostic records why a candidate endpoint or edge was excluded.
type GraphSkipDiagnostic struct {
	ID     string `json:"id,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// GraphDedupeDiagnostic records fallback evidence suppressed by stronger typed
// ontology evidence.
type GraphDedupeDiagnostic struct {
	Source         string `json:"source,omitempty"`
	Target         string `json:"target,omitempty"`
	SuppressedKind string `json:"suppressedKind,omitempty"`
	PreferredKind  string `json:"preferredKind,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// ExpansionPlan describes a bounded multi-step traversal from many roots.
//
// Each root keeps its own seen set so shared intermediate nodes do not erase
// per-root evidence.
type ExpansionPlan struct {
	Sources []ontology.NodeRef
	Steps   []ExpansionStep
	Hydrate HydrateOptions
	Limits  TraverseLimits
	Budget  TraverseBudget
	Purpose string
}

// ExpansionStep describes one repeated Neighborhood traversal within Expand.
type ExpansionStep struct {
	Name              string
	Direction         TraversalDirection
	IncludeStructural bool
	IncludeAmbient    bool
	RelationNames     []string
	Provenance        map[string]struct{}
	TargetTypes       []string
	TargetInterfaces  []string
	SourceKinds       []ontology.NodeKind
	TargetKinds       []ontology.NodeKind
	MaxDepth          int
}

// TraverseLimits bounds Neighborhood and Expand fanout.
type TraverseLimits struct {
	FirstPerSource int
	FirstTotal     int
	MaxDepth       int
	MaxNodes       int
	MaxEdges       int
}

// TraverseBudget is a coarse edge/node cap passed by callers that translate
// external budgets into noderead traversal work.
type TraverseBudget struct {
	MaxNodes int
	MaxEdges int
}

// ExpansionResult is the grouped and flattened multi-hop traversal result.
type ExpansionResult struct {
	Sources   []ExpansionSourceResult
	BySource  map[string]ExpansionSourceResult
	Edges     []NeighborhoodEdge
	Nodes     []NodeRecord
	Truncated bool
}

// ExpansionSourceResult groups multi-hop edges for one original source.
type ExpansionSourceResult struct {
	Source    ontology.NodeRef
	Edges     []NeighborhoodEdge
	Truncated bool
}

// Diagnostics exposes coarse per-scope load/cache counters for tests and
// higher-level observability.
type Diagnostics struct {
	RecordLoads         int
	RecordHits          int
	ContentLoads        int
	ContentHits         int
	CatalogHits         int
	CatalogMisses       int
	ProjectionFallbacks int
	TypeLoads           int
	TypeHits            int
	AssessmentLoads     int
	AssessmentHits      int
	AssessmentFlagLoads int
	EdgeLoads           int
	EdgeHits            int
	UnsupportedRefs     int
}

// NewService constructs a noderead service from stable vault/index dependencies.
func NewService(vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, store Store, schema *ontology.Schema) *Service {
	return &Service{VaultDef: vaultDef, NoteReader: noteReader, Store: store, Schema: schema}
}

// WithNoteFormats supplies the immutable runtime used to disclose executable
// source capabilities on hydrated roots.
func (s *Service) WithNoteFormats(formats noteformat.Runtime) *Service {
	if s != nil {
		s.NoteFormats = formats
	}
	return s
}

// NewScope creates a request/job-local read scope with empty memoization maps.
func (s *Service) NewScope(_ context.Context, opts ScopeOptions) *Scope {
	scope := &Scope{service: s, opts: opts}
	scope.initializeCaches()
	return scope
}

func (s *Scope) initializeCaches() {
	s.relationEntries = nil
	s.relationEntriesErr = nil
	s.relationEntriesRead = false
	s.noteRowByPath = map[string]semdb.NoteMetadataRow{}
	s.noteRowKnown = map[string]bool{}
	s.issueByPath = map[string]bool{}
	s.flagsByPath = map[string]semdb.OntologyAssessmentFlags{}
	s.recordByPath = map[string]NodeRecord{}
	s.recordKnown = map[string]bool{}
	s.contentKnown = map[string]bool{}
	s.recordByRef = map[NodeCacheKey]NodeRecord{}
	s.refKnown = map[NodeCacheKey]bool{}
	s.locatorByRef = map[string]ontology.NodeLocator{}
	s.locatorKnown = map[string]bool{}
	s.projectionByRef = map[string]*ontology.NodeProjection{}
	s.projectionKnown = map[string]bool{}
	s.snapshotByPath = map[string]*ontology.DocumentSnapshot{}
	s.snapshotKnownErr = map[string]error{}
	s.typeRowsByPath = map[string]semdb.OntologyNoteTypeRow{}
	s.typeRowsKnown = map[string]bool{}
	s.assessmentByPath = map[string]*ontology.NoteAssessment{}
	s.assessmentKnown = map[string]bool{}
	s.traverseByKey = map[traverseCacheKey]TraverseResult{}
	s.graphByKey = map[string]GraphResult{}
	s.graphFactsByKey = map[string]GraphFactsResult{}
	s.typeInstancesByID = map[string]ontology.TypeListResult{}
	s.embeddedByType = map[string][]ontology.NodeListItem{}
	s.embeddedSeen = map[string]map[string]struct{}{}
}

// Diagnostics returns a snapshot of this scope's cache/load counters.
func (s *Scope) Diagnostics() Diagnostics {
	if s == nil {
		return Diagnostics{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnostics
}
