package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

type NodeReadPlan struct {
	Roots       []NodeReadRoot
	Fields      FieldSelection
	Relations   []RelationSelection
	Hydrate     HydrateOptions
	Limits      TraverseLimits
	Budget      TraverseBudget
	Diagnostics bool
}

type NodeReadRoot struct {
	Ref      ontology.NodeRef
	TypeName string
}

type FieldSelection struct {
	Names           []string
	IncludeBuiltin  bool
	IncludeIssues   bool
	IncludeContent  bool
	IncludeLocators bool
}

type RelationSelection struct {
	Name              string
	SourceTypes       []string
	Sources           []ontology.NodeRef
	Direction         TraversalDirection
	RelationNames     []string
	Provenance        map[string]struct{}
	IncludeStructural bool
	IncludeAmbient    bool
	TargetTypes       []string
	TargetInterfaces  []string
	NeighborScope     ontoschema.NeighborScope
	FirstPerSource    int
	FirstTotal        int
	MaxDepth          int
	Fields            FieldSelection
	Hydrate           HydrateOptions
	Children          []RelationSelection
}

type NodeReadResult struct {
	Roots       []NodeView
	Nodes       []NodeView
	Groups      []RelationGroup
	Edges       []NeighborhoodEdge
	Diagnostics NodeReadDiagnostics
	Truncated   bool
}

type NodeView = NodeRecord

type RelationGroup struct {
	Name      string
	Source    ontology.NodeRef
	Edges     []NeighborhoodEdge
	Truncated bool
}

type NodeReadDiagnostics struct {
	RecordLoads         int
	RecordHits          int
	ContentLoads        int
	ContentHits         int
	TypeLoads           int
	TypeHits            int
	AssessmentLoads     int
	AssessmentHits      int
	EdgeLoads           int
	EdgeHits            int
	ProjectionFallbacks int
	Truncations         int
}

type relationWork struct {
	selection RelationSelection
	sources   []ontology.NodeRef
	depth     int
}
