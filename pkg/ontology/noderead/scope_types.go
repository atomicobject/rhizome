package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"sync"
)

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

// ScopeOptions configures one request/job Scope.
type ScopeOptions struct {
	// ReadOverlay is an optional request/session-local preview layer. It is
	// intended for web edit-session reads: touched notes project from preview
	// content while untouched notes continue using the indexed catalog/read cache.
	ReadOverlay *ReadOverlay
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
	noteRowsLoaded  bool
	noteRows        []semdb.NoteMetadataRow
	noteRowByPath   map[string]semdb.NoteMetadataRow
	noteRowKnown    map[string]bool
	allNotePaths    []string
	issueByPath     map[string]bool
	flagsByPath     map[string]semdb.OntologyAssessmentFlags
	flagKnown       map[string]bool

	recordByPath map[string]NodeRecord
	recordKnown  map[string]bool
	contentKnown map[string]bool
	recordByRef  map[NodeCacheKey]NodeRecord
	refKnown     map[NodeCacheKey]bool

	preferredIdentifiers      map[string][]ontology.NodeRef
	preferredIdentifierClaims map[string]bool
	locatorByRef              map[string]ontology.NodeLocator
	locatorKnown              map[string]bool

	nodeLinkService     *ontology.NodeLinkService
	cachedNotePathCache *obsidian.NotePathCache
	notePathCacheLoaded bool
	relationEntries     []obsidian.NoteEntry
	relationEntriesErr  error
	relationEntriesRead bool

	projectionByRef    map[string]*ontology.NodeProjection
	projectionKnown    map[string]bool
	rootSnapshotByPath map[string]*ontology.RootDocumentSnapshot
	snapshotByPath     map[string]*ontology.DocumentSnapshot
	snapshotKnownErr   map[string]error
	overlayIndex       *ReadOverlayIndex

	typeRowsByPath map[string]semdb.OntologyNoteTypeRow
	typeRowsKnown  map[string]bool

	assessmentByPath map[string]*ontology.NoteAssessment
	assessmentKnown  map[string]bool

	neighborhoodRowsByKey map[neighborhoodRowCacheKey][]semdb.OntologyEdgeRow
	graphByKey            map[string]GraphResult
	graphFactsByKey       map[string]GraphFactsResult
	typeInstancesByID     map[string]ontology.TypeListResult

	diagnostics Diagnostics
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
