package graphpresentation

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

// GraphResponse provides nodes/edges for visualization.
type GraphResponse struct {
	OmittedNodeIDs []string                   `json:"omittedNodeIDs,omitempty"`
	OmittedNodes   int                        `json:"omittedNodes,omitempty"`
	OmittedEdges   int                        `json:"omittedEdges,omitempty"`
	Nodes          []GraphNode                `json:"nodes"`
	Edges          []GraphEdge                `json:"edges"`
	Truncated      bool                       `json:"truncated,omitempty"`
	CenterID       string                     `json:"centerID,omitempty"`
	NeedsIndex     bool                       `json:"needsIndex,omitempty"`
	Diagnostics    *noderead.GraphDiagnostics `json:"diagnostics,omitempty"`
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
