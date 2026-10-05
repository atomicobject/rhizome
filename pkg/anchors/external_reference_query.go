package codeanchor

// ExternalReferenceQuery selects one canonical external target either by exact
// handle or by deterministic identity candidates. Candidate lookup never uses
// fuzzy matching.
type ExternalReferenceQuery struct {
	Handle       string
	Ecosystem    ExternalEcosystem
	Module       string
	SymbolPrefix string
	Limit        int
}

// ExternalReferenceTarget describes a pathless, unindexed external endpoint.
type ExternalReferenceTarget struct {
	ExternalTargetRow
	External        bool
	Pathless        bool
	Indexed         bool
	SourceBacked    bool
	SourceAvailable bool
}

// ExternalReferenceUse is one local source association with an external target.
type ExternalReferenceUse struct {
	OwnerFQN      string
	Path          string
	Evidence      ExternalEvidenceKind
	Confidence    ExternalConfidence
	ImportedName  string
	LocalName     string
	ManifestPath  string
	DeclaredRange string
	VersionScope  ExternalVersionScope
}

// ExternalReferenceQueryResult is a stable, bounded query-time projection.
type ExternalReferenceQueryResult struct {
	Status     string
	Target     *ExternalReferenceTarget
	Candidates []ExternalReferenceTarget
	Calls      []ExternalReferenceUse
	Types      []ExternalReferenceUse
	Members    []ExternalReferenceUse
	Imports    []ExternalReferenceUse
	Truncated  bool
}
