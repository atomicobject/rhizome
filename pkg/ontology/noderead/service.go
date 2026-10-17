package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ReadStore is the complete indexed read contract. Required capabilities are
// checked at composition rather than silently changing request behavior.
type ReadStore interface {
	CurrentNoteMetadataRows(context.Context) ([]semdb.NoteMetadataRow, error)
	CurrentNoteMetadataRowsByPaths(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
	CurrentNotePropertyValues(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
	CurrentNoteTags(context.Context, []string) ([]semdb.NoteTagRow, error)
	CurrentNoteAliases(context.Context) (map[string][]string, error)
	OntologyAssessmentsByPaths(context.Context, []string) (map[string]semdb.OntologyNoteAssessmentRow, error)
	OntologyAssessmentFlags(context.Context) (map[string]semdb.OntologyAssessmentFlags, error)
	OntologyAssessmentFlagsByPaths(context.Context, []string) (map[string]semdb.OntologyAssessmentFlags, error)
	GetOntologySchemaState(context.Context) (semdb.OntologySchemaState, error)
	OntologyTypesByPaths(context.Context, []string) (map[string]semdb.OntologyNoteTypeRow, error)
	OntologyPathsByType(context.Context, string, int) ([]string, error)
	OntologyNeighborhood(context.Context, readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error)
	AllOntologyNodes(context.Context) ([]readmodel.NodeRow, error)
	OntologyNodesByIDs(context.Context, []string) (map[string]readmodel.NodeRow, error)
	OntologyNodesByPaths(context.Context, []string) ([]readmodel.NodeRow, error)
	OntologyNodesByType(context.Context, string) ([]readmodel.NodeRow, error)
	OntologyNodesByTypePlan(context.Context, readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error)
	OntologyNodesBySourceLocators(context.Context, []string) (map[string]readmodel.NodeRow, error)
	OntologyNodesByNoteFragments(context.Context, []string) (map[string]readmodel.NodeRow, error)
	OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]readmodel.FieldValueRow, error)
	readmodel.GraphStore
	readmodel.ShapeStore
	readmodel.RecentStore
}

// Service owns the stable dependencies used to create request-scoped Scopes.
//
// Service is intentionally thin: caches that depend on request shape,
// hydration profile, or graph flags live on Scope so concurrent jobs do not
// share stale or over-broad reads.
type Service struct {
	VaultDef    obsidian.VaultDefinition
	NoteReader  obsidian.NoteReader
	Store       ReadStore
	Schema      *ontoschema.Schema
	NoteFormats noteformat.Runtime
	// ExactNoteMetadataRows optionally bypasses the global metadata-ready bit
	// after application composition has checked provider and path ownership.
	ExactNoteMetadataRows func(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
	// ExactNotePropertyValues and ExactNoteTags load derived facts only for
	// paths already admitted by ExactNoteMetadataRows. They preserve complete
	// raw-note hydration while unrelated notes are reconciling.
	ExactNotePropertyValues func(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
	ExactNoteTags           func(context.Context, []string) ([]semdb.NoteTagRow, error)
	// ApplyLinkTargets is an explicit live-writer capability. Its owner holds
	// source mutation and metadata/ontology publication under the index lock.
	// The Store and NoteReader must observe that same live vault; published
	// read bundles and preview scopes must never receive this capability.
	ApplyLinkTargets func(context.Context, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error)
}

// NewService constructs a noderead service from stable vault/index dependencies.
func NewService(vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, store ReadStore, schema *ontoschema.Schema) *Service {
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
func (s *Service) NewScope(opts ScopeOptions) *Scope {
	scope := &Scope{service: s, opts: opts}
	scope.initializeCaches()
	return scope
}

func (s *Scope) initializeCaches() {
	s.nodeLinkService = nil
	s.relationEntries = nil
	s.relationEntriesErr = nil
	s.relationEntriesRead = false
	s.noteRowByPath = map[string]semdb.NoteMetadataRow{}
	s.noteRowKnown = map[string]bool{}
	s.issueByPath = map[string]bool{}
	s.flagsByPath = map[string]semdb.OntologyAssessmentFlags{}
	s.flagKnown = map[string]bool{}
	s.recordByPath = map[string]NodeRecord{}
	s.recordKnown = map[string]bool{}
	s.contentKnown = map[string]bool{}
	s.recordByRef = map[NodeCacheKey]NodeRecord{}
	s.refKnown = map[NodeCacheKey]bool{}
	s.preferredIdentifiers = map[string][]ontology.NodeRef{}
	s.preferredIdentifierClaims = map[string]bool{}
	s.locatorByRef = map[string]ontology.NodeLocator{}
	s.locatorKnown = map[string]bool{}
	s.projectionByRef = map[string]*ontology.NodeProjection{}
	s.projectionKnown = map[string]bool{}
	s.rootSnapshotByPath = map[string]*ontology.RootDocumentSnapshot{}
	s.snapshotByPath = map[string]*ontology.DocumentSnapshot{}
	s.snapshotKnownErr = map[string]error{}
	s.typeRowsByPath = map[string]semdb.OntologyNoteTypeRow{}
	s.typeRowsKnown = map[string]bool{}
	s.assessmentByPath = map[string]*ontology.NoteAssessment{}
	s.assessmentKnown = map[string]bool{}
	s.neighborhoodRowsByKey = map[neighborhoodRowCacheKey][]semdb.OntologyEdgeRow{}
	s.graphByKey = map[string]GraphResult{}
	s.graphFactsByKey = map[string]GraphFactsResult{}
	s.typeInstancesByID = map[string]ontology.TypeListResult{}
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
