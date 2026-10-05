package readmodel

import "context"

type GraphNodeRow struct {
	NodeID        string
	NotePath      string
	NodeKind      string
	TypeName      string
	Title         string
	DisplayLabel  string
	SourceLocator string
	Fragment      string
	BlockID       string
	ParentNodeID  string
	NodeRefJSON   string
	UpdatedAt     int64
}

type GraphTypedEdgeRow struct {
	SrcPath      string
	SrcNodeID    string
	RelationName string
	DstPath      string
	DstNodeID    string
	DstType      string
	Provenance   string
	Structural   bool
	UpdatedAt    int64
}

type GraphDocEdgeRow struct {
	SrcPath         string
	DstPath         string
	SourceKind      string // note|code; empty when persisted ownership is unknown
	TargetKind      string // note|code; empty when persisted ownership is unknown
	Kind            string
	Weight          int
	Confidence      string
	ConfidenceScore float64
}

type GraphPathRow struct {
	Path string
	Kind string
}

// GraphEndpointSelector narrows graph reads to a note, embedded node, section,
// or source locator.
//
// Endpoint selectors are what prevent an embedded-node graph request from
// widening into the parent note's full ambient neighborhood.
type GraphEndpointSelector struct {
	Path          string
	NodeID        string
	Kind          string
	Fragment      string
	Structural    string
	SourceLocator string
}

// GraphNodeQuery requests ontology-node catalog rows for graph assembly.
type GraphNodeQuery struct {
	Paths             []string
	PathPrefixes      []string
	EndpointSelectors []GraphEndpointSelector
	Limit             int
}

// GraphEdgeQuery requests typed ontology edges for graph assembly.
type GraphEdgeQuery struct {
	Paths             []string
	PathPrefixes      []string
	EndpointSelectors []GraphEndpointSelector
	IncludeAmbient    bool
	Limit             int
}

// GraphDocEdgeQuery requests fallback doc/code graph evidence.
type GraphDocEdgeQuery struct {
	Paths            []string
	PathPrefixes     []string
	IncludeCode      bool
	IncludeCodeEdges bool
	Limit            int
}

// GraphStore is the indexed graph read-model boundary between sqlite-owned
// ontology/doc/code rows and noderead's endpoint/profile merge logic.
// Docs: [[ontology-indexed-read-model-contract#^spec-0040-table-ownership-interface]].
type GraphStore interface {
	GraphOntologyNodes(context.Context, GraphNodeQuery) ([]GraphNodeRow, error)
	GraphOntologyEdges(context.Context, GraphEdgeQuery) ([]GraphTypedEdgeRow, error)
	GraphDocEdges(context.Context, GraphDocEdgeQuery) ([]GraphDocEdgeRow, error)
	GraphIndexedPaths(context.Context, bool) ([]GraphPathRow, error)
}
