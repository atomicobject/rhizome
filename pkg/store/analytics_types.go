package store

type HotspotPackageRow struct {
	Lang              string
	Pkg               string
	Calls             int
	Callers           int
	UsedSymbols       int
	DocumentedSymbols int
	// Fallback marks rows surfaced via the sparse-graph fallback path. See
	// SparseCallGraphThreshold for the gating mechanism.
	Fallback bool
}

type ComplexityRow struct {
	Lang      string
	Kind      string
	FQN       string
	Path      string
	SpanLines int
	CallsIn   int
	Callers   int
	CallsOut  int
	Callees   int
	Score     float64
}

type ComplexityOptions struct {
	Limit int
	// PathPrefixes applies a prefix match against intel_code_anchors.path (relative or absolute).
	PathPrefixes []string
	// MinLines filters out anchors with spans shorter than this value.
	MinLines int
}

// HotspotPackagesOptions tunes HotspotPackages behavior.
type HotspotPackagesOptions struct {
	Limit int
	// Langs restricts results to the named language codes. Empty = all.
	Langs []string
	// FallbackThreshold overrides SparseCallGraphThreshold when non-nil.
	FallbackThreshold *float64
}

// RationaleAttentionOptions controls the rationale review report.
type RationaleAttentionOptions struct {
	Limit int
	// PathPrefixes applies a prefix match against rationale paths (vault-relative).
	PathPrefixes []string
}

// RationaleAttentionRow is a deterministic finding for comments/symbols that
// need human or research-backed documentation attention.
type RationaleAttentionRow struct {
	Path      string
	SymbolFQN string
	Kind      string
	Line      int64
	Content   string
	Calls     int
	Callers   int
	Mentions  int
	Links     int
	Attention int
	Reasons   []string
}
