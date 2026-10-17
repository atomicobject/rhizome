package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
)

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
	sourceIDs   map[string]struct{}
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
