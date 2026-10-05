// Package search provides the unified search pipeline: retrieval → ranking →
// rollup/shaping → packing.
//
// Planning happens in pkg/search/planner. This package owns the execution
// contracts shared by CLI, MCP, and tests: stable handles, bounded evidence
// merging, deadline-aware retrieval, warning propagation, and deterministic
// ranked results.
//
// Docs:
// - [Search (Hub)](docs/hubs/Search (Hub).md)
// - [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md)
package search

import (
	"context"
	"path/filepath"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// Intent declares the user's goal for retrieval/ranking weight tuning.
// See [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md) for how each intent affects weights.
type Intent string

const (
	IntentSearch            Intent = "search"
	IntentRelatedToSeed     Intent = "related_to_seed"
	IntentDocsForCode       Intent = "docs_for_code"
	IntentCodeForDocs       Intent = "code_for_docs"
	IntentConsolidation     Intent = "consolidation"
	IntentOverview          Intent = "overview"
	IntentSubsystemOverview Intent = "subsystem_overview"
	IntentFindUsages        Intent = "find_usages"
	IntentGoToDef           Intent = "go_to_def"
	IntentExplainSymbol     Intent = "explain_symbol"
	IntentTestsForCode      Intent = "tests_for_code"
	IntentRefactorImpact    Intent = "refactor_impact"
	IntentCallers           Intent = "callers"
	IntentCallees           Intent = "callees"
	IntentImplementers      Intent = "implementers"
	IntentOverrides         Intent = "overrides"
	IntentImports           Intent = "imports"
	IntentDataFlow          Intent = "data_flow"
	IntentSecurityAudit     Intent = "security_audit"
)

// Filters reuse the existing boolean expression DSL used by list/prompt matching.
// This starts as an incremental bridge; we can extend predicates over time.
type Filters struct {
	Inputs     []actions.ListInput
	Expression *actions.InputExpression
	// Types limits unified retrieval to code and/or note entities. These fields
	// are deliberately kept beside the existing expression filter so every
	// retriever receives the same immutable eligibility contract.
	Types        []string
	PathPrefixes []string
	// TestsOnly and ExcludeTests are mutually exclusive eligibility controls.
	// They are applied before ranking for every retriever result.
	TestsOnly    bool
	ExcludeTests bool
	// NoteTypes are resolved types of the owning note. They apply to note
	// sections and embedded nodes through their note path, rather than to the
	// type of an individual embedded node.
	NoteTypes []string
	// ExactSymbols restricts code candidates to those whose symbol or FQN equals
	// one of the values or whose FQN ends with `.` + value; any non-empty list
	// also excludes candidates without a symbol.
	ExactSymbols []string
}

// AllowsSymbol reports whether a candidate symbol/FQN pair satisfies the
// ExactSymbols restriction. Values are compared literally; callers normalize.
func (f Filters) AllowsSymbol(symbol, fqn string) bool {
	if len(f.ExactSymbols) == 0 {
		return true
	}
	for _, needle := range f.ExactSymbols {
		if needle == "" {
			continue
		}
		if fqn == needle || symbol == needle || strings.HasSuffix(fqn, "."+needle) {
			return true
		}
	}
	return false
}

func (f Filters) AllowsTestPath(path string) bool {
	isTest := IsTestPath(path)
	if f.TestsOnly {
		return isTest
	}
	if f.ExcludeTests {
		return !isTest
	}
	return true
}

// AllowsType reports whether a unified result type is eligible under an
// optional type restriction. An empty restriction allows every type.
func (f Filters) AllowsType(typeName string) bool {
	if len(f.Types) == 0 {
		return true
	}
	typeName = strings.ToLower(strings.TrimSpace(typeName))
	switch typeName {
	case "notes", "doc_section", "note_chunk":
		typeName = "note"
	case "anchor", "code_chunk":
		typeName = "code"
	}
	for _, allowed := range f.Types {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		switch allowed {
		case "notes", "doc_section", "note_chunk":
			allowed = "note"
		case "anchor", "code_chunk":
			allowed = "code"
		}
		if allowed == typeName {
			return true
		}
	}
	return false
}

func (f Filters) AllowsCandidateType(typeName string) bool {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "doc_section", "ontology_node", "note_chunk":
		return f.AllowsType("note")
	case "anchor", "code_chunk":
		return f.AllowsType("code")
	default:
		return f.AllowsType(typeName)
	}
}

// AllowsPath applies literal exact-or-descendant path-prefix matching. Empty
// prefixes mean unrestricted results, and path separators are normalized so
// callers can pass platform-native paths safely.
func (f Filters) AllowsPath(value string) bool {
	if len(f.PathPrefixes) == 0 {
		return true
	}
	value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	for _, prefix := range f.PathPrefixes {
		prefix = filepath.ToSlash(filepath.Clean(strings.TrimSpace(prefix)))
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" || prefix == "." || prefix == "/" {
			return true
		}
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}

type Limits struct {
	Total int
}

type Budget struct {
	Chars int
}

// PathKind is caller-supplied durable ownership for a vault-relative path.
// Search consumes this fact when it needs to form a note or code identity. It
// must not infer ownership from an extension or the local filesystem.
type PathKind string

const (
	PathKindNote PathKind = "note"
	PathKindCode PathKind = "code"
)

// QuerySpec is the unified request shape for retrieval + ranking + presentation.
//
// Text and Seeds are the primary inputs; Intent tunes retriever selection and
// ranking weights; Limits and Budget control result count and optional context
// rendering. Target* fields are filled by repair/target-resolution before
// planning and must be preserved so warnings and answer confidence reflect the
// actual run.
type QuerySpec struct {
	Text              string
	Seeds             []knowledge.Handle
	Intent            Intent
	ExplicitSeedPaths []string
	// PathKinds is an optional, immutable path-kind snapshot supplied by the
	// caller from configured or persisted ownership. Keys preserve canonical
	// authored path spelling.
	PathKinds            map[string]PathKind
	HasExplicitSeeds     bool
	TargetStatus         TargetStatus
	ResolutionConfidence float64
	TargetCandidates     []TargetCandidate
	ResolvedTarget       *TargetCandidate
	Filters              Filters
	Limits               Limits
	Budget               Budget

	repaired        bool
	targetsResolved bool
}

type TargetStatus string

const (
	TargetStatusNone           TargetStatus = ""
	TargetStatusExplicitPath   TargetStatus = "explicit_path"
	TargetStatusInferredPath   TargetStatus = "inferred_path"
	TargetStatusInferredSymbol TargetStatus = "inferred_symbol"
	TargetStatusAmbiguous      TargetStatus = "ambiguous"
	TargetStatusDowngraded     TargetStatus = "downgraded"
	TargetStatusUnresolved     TargetStatus = "unresolved"
)

type TargetCandidate struct {
	Path   string `json:"path,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	FQN    string `json:"fqn,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Retriever produces candidates from a QuerySpec.
//
// Multiple retrievers run in parallel, or progressively when the caller gives a
// deadline. Evidence from different retrievers merges on the same Handle; a
// retriever should not return multiple identities for the same underlying
// source just to preserve its own ranking.
type Retriever interface {
	Name() string
	Retrieve(ctx context.Context, spec QuerySpec) ([]Candidate, error)
}

// NestedRetriever exposes retrievers hidden behind composite/planning helpers so
// diagnostics can report skipped/empty/degraded lanes without changing ranking.
type NestedRetriever interface {
	NestedRetrievers() []Retriever
}

type RetrieverCostClass int

const (
	RetrieverCostUnknown RetrieverCostClass = iota
	RetrieverCostCPUHeavy
	RetrieverCostIOBound
)

// CostedRetriever lets a retriever declare a coarse cost class to tune concurrency.
type CostedRetriever interface {
	Retriever
	CostClass() RetrieverCostClass
}

// Ranker scores and orders candidates. The planner configures weights based on intent.
type Ranker interface {
	Rank(ctx context.Context, spec QuerySpec, candidates []Candidate) ([]RankedResult, error)
}

// ApproxScoreProvider lets rankers expose a cheaper but semantically aligned scoring
// configuration for pruning and timeout fallback paths.
type ApproxScoreProvider interface {
	ApproxChannelWeights(spec QuerySpec) map[EvidenceChannel]float64
	ApproxMaxPerOwner(spec QuerySpec) int
}

// Packer renders ranked results into a budgeted context string for LLM consumption.
type Packer interface {
	Pack(ctx context.Context, spec QuerySpec, results []RankedResult) (PackedContext, error)
}

type Evidence struct {
	Type     string
	Channel  EvidenceChannel
	Score    float64
	RawScore float64
	Source   string
	Details  map[string]string
}

// Candidate is the merged evidence record for an entity (note, anchor, chunk,
// file, or ontology node owner).
// Owner enables diversity limiting (MaxPerOwner); multiple candidates can share an owner.
//
// Handle identity: Candidates with the same Handle are considered the same entity.
// Evidence from multiple retrievers merges into a single Candidate via MergeCandidate().
//
// Metadata fields: Retrievers should populate what they know; the ranker/packer uses
// these for display. Not all fields apply to all handle types (e.g., Symbol/FQN only
// apply to code chunks/anchors).
type Candidate struct {
	Handle   knowledge.Handle // Unique identifier for this entity
	Owner    knowledge.Handle // Parent entity for MaxPerOwner diversity limiting
	Evidence []Evidence       // Signals from retrievers (vector similarity, lexical match, etc.)
	// SupportOnly candidates enrich an entity found by another retriever but do
	// not stand alone in final results. Use this for rationale/provenance lanes
	// that should boost anchors/files without making comments a result class.
	SupportOnly bool

	// Metadata for packing/rendering (retrievers fill what they know)
	Type        string // Entity type: "note", "code", "anchor"
	Path        string // Vault-relative path
	Title       string // Note title or code file name
	Symbol      string // Short symbol name (e.g., "Service")
	FQN         string // Fully qualified name (e.g., "pkg/search.Service")
	Kind        string // Symbol kind: "function", "type", "method", etc.
	Granularity string // Chunk granularity: "symbol", "body", "module"
	ChunkIndex  int    // Chunk index within parent (-1 if N/A)
	Breadcrumb  string // Navigation path for display
	Heading     string // Markdown heading this chunk belongs to

	AnchorID string // Code anchor ID (for anchor/codechunk handles)
	NoteID   string // Note path (for notechunk handles)

	NodeID        string // Ontology node ID (for nodechunk handles)
	NodeRefJSON   string // Serialized ontology NodeRef
	SourceLocator string // Canonical ontology lookup/display locator
	NodeKind      string // note_root, section, embedded, etc.
	NodeType      string // Resolved ontology type
	ParentNodeID  string // Parent ontology node ID, when present

	DocClass   DocClass // Stable doc classification for ranking/packing
	PrimaryDoc bool     // True when this is the primary local module doc

	NodeRef *ontology.NodeRef // Ontology navigation identity when available

	handleKey string `json:"-"` // Cached Handle.String() for fast map lookups
}

type RankedResult struct {
	Candidate
	FinalScore float64
}

type PackedPiece struct {
	Key      string
	Priority int
	Score    float64
	Text     string
}

type PackedContext struct {
	Text  string
	Meta  map[string]any
	Items []PackedPiece
}

type Response struct {
	Query          QuerySpec
	Results        []RankedResult
	Packed         *PackedContext
	DeferredPacker Packer `json:"-"`
	Warnings       []Warning
	Lanes          []LaneStatus
}

type DocClass string

const (
	DocClassNone       DocClass = "none"
	DocClassModule     DocClass = "module"
	DocClassHub        DocClass = "hub"
	DocClassSpec       DocClass = "spec"
	DocClassReference  DocClass = "reference"
	DocClassAnalysis   DocClass = "analysis"
	DocClassEffort     DocClass = "effort"
	DocClassRepoGlobal DocClass = "repo_global"
	DocClassGenerated  DocClass = "generated"
)

type Shaper interface {
	Shape(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error)
}

func IsBroadIntent(intent Intent) bool {
	switch intent {
	case IntentSearch,
		IntentRelatedToSeed,
		IntentDocsForCode,
		IntentCodeForDocs,
		IntentConsolidation,
		IntentOverview,
		IntentSubsystemOverview,
		IntentExplainSymbol,
		IntentDataFlow,
		IntentSecurityAudit:
		return true
	default:
		return false
	}
}

func IsPrecisionIntent(intent Intent) bool {
	switch intent {
	case IntentGoToDef,
		IntentFindUsages,
		IntentCallers,
		IntentCallees,
		IntentTestsForCode,
		IntentRefactorImpact,
		IntentImplementers,
		IntentOverrides,
		IntentImports:
		return true
	default:
		return false
	}
}
