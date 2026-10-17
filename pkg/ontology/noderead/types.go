package noderead

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// HydrateProfile controls how much note/node data Hydrate should materialize.
type HydrateProfile string

const (
	// HydrateSummary adds display metadata such as title, tags, type, and issues.
	HydrateSummary HydrateProfile = "summary"
	// HydrateContent includes authored note content when the caller needs it.
	HydrateContent HydrateProfile = "content"
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
	FieldValues            map[string][]readmodel.FieldValueRow
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
	// IncludeLocators opts into author-facing link rendering. Explicit plan/apply
	// requests also compute locators so fixes remain available.
	IncludeLocators bool
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
	Predicates []readmodel.FieldPredicate
	Sort       []readmodel.FieldSort
}
