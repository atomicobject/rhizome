package store

// EmbeddingSearchFilters specifies filters for embedding search.
type EmbeddingSearchFilters struct {
	PathPrefixes       []string // Filter by path prefix (directory-aware)
	Kinds              []string // Filter by anchor kind (e.g., "function", "type")
	Granularity        []string // Filter by chunk granularity (e.g., "symbol", "section")
	ExcludeGranularity []string // Exclude granularities before the nearest-neighbor limit.
	OwnerTypes         []string // Filter by owner type ("anchor", "doc_section", or "ontology_node")
	OntologyTypeNames  []string // Filter ontology_node chunks by ontology type name.
	// NoteTypes filter the resolved type of the note owning a chunk. This
	// includes doc sections and embedded ontology nodes from that note.
	NoteTypes    []string
	ExactSymbols []string
	TestsOnly    bool
	ExcludeTests bool
}

// ScoredChunk represents a chunk with its similarity score from embedding search.
type ScoredChunk struct {
	ChunkID     string
	OwnerID     string
	OwnerType   string
	Ord         int
	Path        string
	Breadcrumb  string
	Heading     string
	Granularity string
	Score       float64
}
