package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
)

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
