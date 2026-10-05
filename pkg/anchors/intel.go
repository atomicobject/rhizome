package codeanchor

import "github.com/atomicobject/rhizome/pkg/ontology/readmodel"

// IntelAnchor represents an addressable code entity persisted in the intel tables.
type IntelAnchor struct {
	AnchorID   string
	Lang       Lang
	Kind       string
	Path       string
	Symbol     string
	FQN        string
	Signature  string
	DocComment string
	// Calls is an optional, derived list of callees used during embedding synthesis.
	// It is not persisted in intel_code_anchors (yet).
	Calls []string
	// RelatedDocs is an optional, derived list of note/spec labels used during embedding synthesis.
	// It is not persisted in intel_code_anchors.
	RelatedDocs []string
	StartByte   int64
	EndByte     int64
	StartLine   int64
	EndLine     int64
	Fingerprint string
	UpdatedAt   int64 // unix seconds
}

// IntelAnchorMeta is a lightweight view of IntelAnchor for fast sync planning.
type IntelAnchorMeta struct {
	AnchorID    string
	Lang        Lang
	Kind        string
	Path        string
	Symbol      string
	FQN         string
	Fingerprint string
	UpdatedAt   int64 // unix seconds
}

// IntelDocSection represents a section of a markdown note with a stable-ish ID.
type IntelDocSection struct {
	SectionID   string
	Path        string
	Title       string
	Level       int64
	StartByte   int64
	EndByte     int64
	Content     string
	Fingerprint string
	UpdatedAt   int64 // unix seconds
}

// IntelDocSectionMeta is a lightweight view of IntelDocSection for fast sync planning.
type IntelDocSectionMeta struct {
	SectionID   string
	Path        string
	Title       string
	Level       int64
	StartByte   int64
	EndByte     int64
	Fingerprint string
	UpdatedAt   int64 // unix seconds
}

// IntelOntologyNode aliases the storage-neutral ontology read-model row.
type IntelOntologyNode = readmodel.NodeRow

// IntelOntologyNodeFieldValue aliases the storage-neutral ontology field row.
type IntelOntologyNodeFieldValue = readmodel.FieldValueRow

// IntelOntologyNodeLinkDependency aliases the storage-neutral link dependency row.
type IntelOntologyNodeLinkDependency = readmodel.LinkDependencyRow

// IntelOntologyNodeReadModel aliases the storage-neutral ontology read-model write set.
type IntelOntologyNodeReadModel = readmodel.NodeReadModel

type OntologyFieldOperator = readmodel.FieldOperator

const (
	OntologyFieldOpEq     = readmodel.FieldOpEq
	OntologyFieldOpIn     = readmodel.FieldOpIn
	OntologyFieldOpExists = readmodel.FieldOpExists
	OntologyFieldOpGT     = readmodel.FieldOpGT
	OntologyFieldOpGTE    = readmodel.FieldOpGTE
	OntologyFieldOpLT     = readmodel.FieldOpLT
	OntologyFieldOpLTE    = readmodel.FieldOpLTE
)

type OntologyFieldPredicate = readmodel.FieldPredicate
type OntologyFieldSort = readmodel.FieldSort
type OntologyNodeQueryPlan = readmodel.NodeQueryPlan

// IntelOntologyNodeEmbeddingState tracks ontology-only embedding dependencies.
// It is keyed by chunk_id from intel_chunks and intentionally sidecars the vector
// table: most embeddings are code or untyped-note chunks and do not need these fields.
type IntelOntologyNodeEmbeddingState struct {
	ChunkID                  string
	NodeID                   string
	NotePath                 string
	TypeName                 string
	NodeKind                 string
	EmbeddingSchemaSignature string
	NodeStructureFingerprint string
	SourceContentHash        string
	ChunkTextHash            string
	ChunkGranularity         string
	Provider                 string
	Model                    string
	UpdatedAt                int64
}

// IntelEdge is a typed directed edge between intel items (anchors or doc sections).
type IntelEdge struct {
	SrcID    string
	DstID    string
	Kind     string
	MetaJSON string
}

// IntelFTSRow represents a single row to be inserted into the intel_fts virtual table.
// item_type is "anchor", "doc_section", or a provider root-region lane.
type IntelFTSRow struct {
	ItemType string
	ItemID   string
	Path     string
	Title    string
	Body     string
}

// IntelChunk represents a semantic chunk derived from an anchor or doc section.
// Chunks are first-class records in the intel spine, enabling deterministic ordering
// and queryable chunk provenance before embeddings are generated.
type IntelChunk struct {
	ChunkID     string // stable ID: hash of (owner_id, ord, granularity)
	OwnerID     string // anchor_id or section_id that owns this chunk
	OwnerType   string // "anchor", "doc_section", or "ontology_node"
	ChunkFamily string // producer family used for scoped replacement/pruning
	Ord         int    // 0-indexed ordering within the owner
	Granularity string // e.g. "symbol", "module", "body", "section"
	Breadcrumb  string // hierarchical path (e.g. "pkg > file > Func")
	Heading     string // section/symbol heading
	ContentHash string // sha256 of chunk text (for cache hit tracking)
	StartByte   int64
	EndByte     int64
	UpdatedAt   int64 // unix seconds
}

const (
	IntelChunkFamilyDefault         = "default"
	IntelChunkFamilyAuthoredSection = "authored_section"
)

// PackMetadata holds index-time metadata used for reproducible context packs.
// These values are stored in index_metadata and retrieved when building packs.
type PackMetadata struct {
	ConfigHash     string `json:"configHash,omitempty"`     // hash of embedding/chunking config
	ModelHash      string `json:"modelHash,omitempty"`      // hash of the embedding model ID
	AlgoVersion    string `json:"algoVersion,omitempty"`    // version of packing/search algorithm
	IndexerVersion string `json:"indexerVersion,omitempty"` // code indexer version
}
