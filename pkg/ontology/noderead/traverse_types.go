package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

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
// It preserves embedded-node source identity and can hydrate related target
// records in the same request-scoped cache.
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
