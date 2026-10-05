package codeanchor

// Docs: [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)

import "strings"

// Lang represents a programming language identifier.
type Lang string

const (
	LangTS     Lang = "ts"
	LangPy     Lang = "py"
	LangGo     Lang = "go"
	LangCs     Lang = "cs"
	LangJava   Lang = "java"
	LangSwift  Lang = "swift"
	LangKotlin Lang = "kotlin"
	LangPhp    Lang = "php"
)

// SymbolKind describes the category of a symbol.
type SymbolKind string

const (
	SymClass     SymbolKind = "class"
	SymInterface SymbolKind = "interface"
	SymStruct    SymbolKind = "struct"
	// SymType represents a type-alias style symbol (TypeScript, etc.).
	// Note: symbol kind does not affect anchor matching; it is metadata.
	SymType   SymbolKind = "type"
	SymFunc   SymbolKind = "func"
	SymMethod SymbolKind = "method"
	SymEnum   SymbolKind = "enum"
	SymField  SymbolKind = "field"
)

// Symbol represents a declared code entity.
//
// File is stored as a vault-root-relative code path once it crosses the service
// boundary. FQN is the stable retrieval and anchor-authoring handle for symbol
// anchors; language indexers define its exact shape, and NormalizeFQN only fills
// the common pkg.name fallback when an indexer did not set it explicitly.
type Symbol struct {
	Lang Lang
	Kind SymbolKind
	File string
	Pkg  string
	Name string
	FQN  string
	// Exported is best-effort and may be false even for exported declarations depending on indexer.
	Exported bool
	// Optional source metadata (best-effort; may be zero/empty depending on language indexer).
	Signature  string
	DocComment string
	StartByte  int64
	EndByte    int64
	StartLine  int64
	EndLine    int64
}

// SymbolRef references a symbol without requiring its database identifier.
// Member indicates the symbol is a class/struct member (method, property, constant)
// rather than a top-level symbol in its package/namespace. For PHP this picks the
// `::` separator over `\` in normalized FQNs; for other languages it is informational
// (they always join Pkg and Name with `.`).
type SymbolRef struct {
	Lang   Lang   `yaml:"lang" json:"lang"`
	Pkg    string `yaml:"pkg" json:"pkg"`
	Name   string `yaml:"name" json:"name"`
	Member bool   `yaml:"member,omitempty" json:"member,omitempty"`
}

// NormalizeFQN builds a fully qualified name for the symbol.
func (s Symbol) NormalizeFQN() string {
	if s.FQN != "" {
		return s.FQN
	}
	if s.Pkg == "" {
		return s.Name
	}
	return strings.TrimPrefix(s.Pkg+"."+s.Name, ".")
}

// AnchorKind determines how an anchor matches code.
type AnchorKind string

const (
	AnchorBaseClass  AnchorKind = "baseClass"
	AnchorAnnotation AnchorKind = "annotation"
	// AnchorFunc matches both the definition of a function symbol and files that reference it (call sites).
	AnchorFunc AnchorKind = "function"
	// AnchorPath matches files by path prefix (directory-scoped anchors).
	AnchorPath AnchorKind = "path"
	// AnchorGlob matches files by doublestar glob patterns (language-agnostic).
	AnchorGlob AnchorKind = "glob"
)

// AnnotationSelector matches annotations / decorators and optional args.
type AnnotationSelector struct {
	Symbol     SymbolRef         `yaml:"symbol" json:"symbol"`
	ArgFilters map[string]string `yaml:"argFilters" json:"argFilters"`
}

// Anchor represents a semantic selector tied to notes.
//
// Anchors are author-facing frontmatter selectors, but stored scopes are
// retrieval-facing. Keep scope-defining fields stable and normalized: path/glob
// anchors use normalized vault-relative paths, symbol anchors use SymbolRef
// triples, and Label remains presentation/grouping metadata.
type Anchor struct {
	ID      int64
	Kind    AnchorKind
	Lang    Lang
	BaseSym *SymbolRef
	Ann     *AnnotationSelector
	Label   string
	// PathPrefix is a vault-relative (normalized) path prefix for AnchorPath matching.
	// When non-empty, it matches a file or directory when the target equals PathPrefix
	// or is nested under it (directory-boundary aware).
	PathPrefix string
	// Globs are doublestar patterns used for AnchorGlob matching.
	// These are stored in resolved (typically absolute) form so matching can be done
	// against normalized file paths without language indexer support.
	Globs []string
}

// ScopeEqual returns true if two anchors match the same code (same Kind, Lang, BaseSym, Ann, PathPrefix, Globs).
// ID and Label are not compared.
func (a Anchor) ScopeEqual(other Anchor) bool {
	if a.Kind != other.Kind || a.Lang != other.Lang || a.PathPrefix != other.PathPrefix {
		return false
	}
	// Compare BaseSym
	if (a.BaseSym == nil) != (other.BaseSym == nil) {
		return false
	}
	if a.BaseSym != nil && *a.BaseSym != *other.BaseSym {
		return false
	}
	// Compare Ann
	if (a.Ann == nil) != (other.Ann == nil) {
		return false
	}
	if a.Ann != nil {
		if a.Ann.Symbol != other.Ann.Symbol {
			return false
		}
		if len(a.Ann.ArgFilters) != len(other.Ann.ArgFilters) {
			return false
		}
		for k, v := range a.Ann.ArgFilters {
			if other.Ann.ArgFilters[k] != v {
				return false
			}
		}
	}
	// Compare Globs as a set (order doesn't affect matching behavior)
	if len(a.Globs) != len(other.Globs) {
		return false
	}
	if len(a.Globs) > 0 {
		globSet := make(map[string]bool, len(a.Globs))
		for _, g := range a.Globs {
			globSet[g] = true
		}
		for _, g := range other.Globs {
			if !globSet[g] {
				return false
			}
		}
	}
	return true
}

// AnchorUpsertResult contains the IDs of anchors affected by an upsert operation.
//
// Scope recompute is only required for new/changed selectors. Unchanged anchors
// keep their materialized scope, while deleted anchors are already cleaned up by
// note upsert.
type AnchorUpsertResult struct {
	NewIDs       []int64 // Anchors that didn't exist before
	ChangedIDs   []int64 // Anchors whose scope-defining fields changed
	UnchangedIDs []int64 // Anchors that exist with identical scope
	DeletedIDs   []int64 // Anchors removed (not in keepLabels)
}

// NeedsScopeRecompute returns the anchor IDs that need scope recomputation (new + changed).
func (r AnchorUpsertResult) NeedsScopeRecompute() []int64 {
	result := make([]int64, 0, len(r.NewIDs)+len(r.ChangedIDs))
	result = append(result, r.NewIDs...)
	result = append(result, r.ChangedIDs...)
	return result
}

// ScopeReadyFootprint captures the in-memory code facts that can make an anchor
// ready for exact scope recomputation once the corresponding file batch is durable.
type ScopeReadyFootprint struct {
	Path           string
	SymbolKeys     []string
	AnnotationKeys []string
}

// AnchorMatchTrace captures why an anchor was returned for a file.
type AnchorMatchTrace struct {
	AnchorLabel string `json:"anchorLabel"`
	Reason      string `json:"reason"`
	Target      string `json:"target"`
}

// Note represents a note with optional anchor definitions.
type Note struct {
	ID             int64
	Path           string
	Title          string
	DefinedAnchors []Anchor
}

// FileContext represents the resolved anchors and notes for a file.
type FileContext struct {
	File        string                `json:"file"`
	Anchors     []string              `json:"anchors"`
	Notes       []Note                `json:"notes"`
	AnchorNotes []AnchorNotes         `json:"anchorNotes,omitempty"`
	AnchorKinds map[string]AnchorKind `json:"anchorKinds,omitempty"`
	Trace       []AnchorMatchTrace    `json:"trace,omitempty"`
	Annotations []AnnotationUse       `json:"annotations,omitempty"`
	Calls       []CallSite            `json:"calls,omitempty"`
	Symbols     []string              `json:"symbols,omitempty"`
}

// AnchorNotes groups notes linked to a specific anchor label.
// This preserves which anchors caused which notes to be returned.
type AnchorNotes struct {
	Label string `json:"label"`
	Notes []Note `json:"notes"`
}

// AnnotationUse captures a decorator/attribute usage on a symbol.
type AnnotationUse struct {
	OwnerFQN  string
	AnnSymbol SymbolRef
	Args      map[string]string
}

// CallSite represents a resolved call in a file.
type CallSite struct {
	File         string
	OwnerFQN     string
	CalleeSymbol SymbolRef
}

// ImportEdge represents a file-level import dependency.
// Module is the imported module name (e.g., "charm.misc.colors" for "from charm.misc.colors import Colors").
type ImportEdge struct {
	Module string
}

// TypeRef represents a type reference in code (e.g., type annotation, variable type).
type TypeRef struct {
	File     string
	OwnerFQN string    // FQN of the function/method containing this type reference
	TypeSym  SymbolRef // The referenced type
}

// MemberRef represents a resolved non-call symbol usage (for example a static field read).
type MemberRef struct {
	File     string
	OwnerFQN string
	Sym      SymbolRef
}

// AnchorScope holds symbols and calls associated with an anchor.
type AnchorScope struct {
	Symbols []string
	Calls   []string
}

// ParseStatus reports whether a file parsed cleanly enough for downstream indexing.
type ParseStatus string

const (
	ParseOK        ParseStatus = "ok"
	ParseRecovered ParseStatus = "recovered"
	ParseErrored   ParseStatus = "error"
	ParseTimeout   ParseStatus = "timeout"
)

func (s ParseStatus) TrustedForIndexing() bool {
	return s == ParseOK || s == ParseRecovered
}

// FileMeta is lightweight per-file index metadata persisted alongside symbols.
// Used to decide whether a file is safe to skip.
type FileMeta struct {
	Path        string
	Lang        Lang
	Hash        string
	ParseStatus ParseStatus
}

// NoteIndexMeta stores persisted incremental-index metadata for notes.
type NoteIndexMeta struct {
	ContentHash    string
	IndexerVersion string
	Mtime          int64
}

// FileSummary holds parsed data from a language indexer.
//
// This is the single handoff object between language-specific parsing and shared
// persistence. Keep it syntax-derived and deterministic; resolver-backed edges
// and retrieval rows are built later so batch indexing can avoid per-file store
// lookups on the hot path.
type FileSummary struct {
	FilePath    string
	Lang        Lang
	Hash        string
	PackageDoc  string // Package-level/file-level doc comment (e.g., Go package comment)
	Symbols     []Symbol
	Supers      []SuperEdge
	Annotations []AnnotationUse
	Calls       []CallSite
	Imports     []ImportEdge // File-level import dependencies
	TypeRefs    []TypeRef    // Type references (annotations, variable types)
	MemberRefs  []MemberRef  // Resolved symbol uses that are not calls/type refs
	// ExternalEvidence carries sparse, syntax-derived natural-key associations.
	// Empty is meaningful for a trusted replacement because it clears stale evidence.
	ExternalEvidence ExternalEvidenceBatch
	// ExternalEvidenceReady distinguishes a classifier-produced empty batch from
	// a language indexer that does not participate in external classification.
	ExternalEvidenceReady bool
	ParseStatus           ParseStatus
	// GoPackage is syntax-derived package membership used by the deferred,
	// package-level Go relationship analyzer. It is nil for other languages.
	GoPackage *GoPackageKey
}

// DocStats represents documentation signals for a code symbol anchor (intel_code_anchors).
// Mentions come from doc-section edges (intel_edges(kind=mentions)); Links come from coderef/doc link extraction (doc_links).
type DocStats struct {
	Kind     string
	Path     string
	Mentions int
	Links    int
	Resolved bool
}

// SymbolMeta is lightweight symbol metadata for reporting/debugging (no source ranges).
type SymbolMeta struct {
	Lang     Lang
	Kind     SymbolKind
	File     string
	Pkg      string
	Name     string
	FQN      string
	Exported bool
}

// SuperEdge links a child symbol to its parent (extends/implements).
type SuperEdge struct {
	ChildFQN  string
	ParentFQN string
}
