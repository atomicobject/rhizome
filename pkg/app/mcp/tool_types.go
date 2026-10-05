package mcp

import (
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// FileEntry is the structured payload returned by the files tool
type FileEntry struct {
	Path                 string                 `json:"path"`
	AbsolutePath         string                 `json:"absolutePath,omitempty"`
	FileType             string                 `json:"fileType,omitempty"`
	Tags                 []string               `json:"tags,omitempty"`
	Frontmatter          map[string]interface{} `json:"frontmatter,omitempty"`
	Content              string                 `json:"content,omitempty"`
	ContentTruncated     bool                   `json:"contentTruncated,omitempty"`
	ContentOmittedReason string                 `json:"contentOmittedReason,omitempty"`
	Backlinks            []obsidian.Backlink    `json:"backlinks,omitempty"`
	CodeLinks            []CodeLink             `json:"codeLinks,omitempty"` // References from source code
}

// FilesResponse wraps the full files response
type FilesResponse struct {
	SessionID         string      `json:"sessionId,omitempty"`
	DedupeHits        int         `json:"dedupeHits,omitempty"`
	Vault             string      `json:"vault"`
	Offset            int         `json:"offset,omitempty"`
	Returned          int         `json:"returned,omitempty"`
	Total             int         `json:"total,omitempty"`
	Count             int         `json:"count"`
	Remaining         int         `json:"remaining,omitempty"`
	ContinuationToken string      `json:"continuationToken,omitempty"`
	Files             []FileEntry `json:"files"`
	// Text is populated when includeContent="compress" is used.
	// When set, Files will have empty content and this field contains
	// the LLM-compressed summary of all file contents.
	Text       string `json:"text,omitempty"`
	Compressed bool   `json:"compressed,omitempty"`
}

// TagListResponse describes the JSON shape for listing tags
type TagListResponse struct {
	Tags []actions.TagSummary `json:"tags"`
}

// PropertyListResponse describes the JSON shape for listing properties
type PropertyListResponse struct {
	Properties []actions.PropertySummary `json:"properties"`
}

// BridgePayload captures cross-community bridge strength for a node.
type BridgePayload struct {
	Path                string `json:"path"`
	CrossCommunityEdges int    `json:"crossCommunityEdges,omitempty"`
}

// GraphNodePayload captures node-level metrics for MCP clients.
type GraphNodePayload struct {
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	Inbound     int      `json:"inbound"`
	Outbound    int      `json:"outbound"`
	Hub         float64  `json:"hub"`       // HITS hub score: measures how well this note curates/aggregates links
	Authority   float64  `json:"authority"` // HITS authority score: measures how often this note is referenced
	Community   string   `json:"community,omitempty"`
	SCC         string   `json:"scc"`
	Neighbors   []string `json:"neighbors,omitempty"`
	LinksOut    []string `json:"linksOut,omitempty"`
	LinksIn     []string `json:"linksIn,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	WeakComp    string   `json:"weakComponent,omitempty"`
	IsBridge    bool     `json:"isBridge,omitempty"`
	BridgeEdges int      `json:"bridgeEdges,omitempty"`
}

// AuthorityScorePayload carries a note and its hub/authority scores.
type AuthorityScorePayload struct {
	Path        string                 `json:"path"`
	Title       string                 `json:"title,omitempty"`
	Frontmatter map[string]interface{} `json:"frontmatter,omitempty"`
	Kind        string                 `json:"kind,omitempty"`
	Authority   float64                `json:"authority"`
	Hub         float64                `json:"hub,omitempty"`
}

// AuthorityBucketPayload summarizes authority distribution for a community.
type AuthorityBucketPayload struct {
	Low     float64 `json:"low,omitempty"`
	High    float64 `json:"high,omitempty"`
	Count   int     `json:"count,omitempty"`
	Example string  `json:"example,omitempty"`
}

// GraphRecencyPayload summarizes modification recency for a community.
type GraphRecencyPayload struct {
	LatestPath    string  `json:"latestPath,omitempty"`
	LatestAgeDays float64 `json:"latestAgeDays,omitempty"`
	RecentCount   int     `json:"recentCount,omitempty"`
	WindowDays    int     `json:"windowDays,omitempty"`
}

// AuthorityStatsPayload captures coarse percentiles/mean for authority scores.
type AuthorityStatsPayload struct {
	Mean float64 `json:"mean,omitempty"`
	P50  float64 `json:"p50,omitempty"`
	P75  float64 `json:"p75,omitempty"`
	P90  float64 `json:"p90,omitempty"`
	P95  float64 `json:"p95,omitempty"`
	P99  float64 `json:"p99,omitempty"`
	Max  float64 `json:"max,omitempty"`
}

// GraphCommunityPayload summarizes a community.
type GraphCommunityPayload struct {
	ID               string                   `json:"id"`
	Size             int                      `json:"size"`
	FractionOfVault  float64                  `json:"fractionOfVault,omitempty"`
	Nodes            []string                 `json:"nodes,omitempty"`
	TopTags          []obsidian.TagCount      `json:"topTags,omitempty"`
	TopAuthority     []AuthorityScorePayload  `json:"topAuthority,omitempty"`
	AuthorityBuckets []AuthorityBucketPayload `json:"authorityBuckets,omitempty"`
	AuthorityStats   *AuthorityStatsPayload   `json:"authorityStats,omitempty"`
	Recency          *GraphRecencyPayload     `json:"recency,omitempty"`
	Anchor           string                   `json:"anchor,omitempty"`
	Density          float64                  `json:"density,omitempty"`
	Bridges          []string                 `json:"bridges,omitempty"`
	BridgesDetailed  []BridgePayload          `json:"bridgesDetailed,omitempty"`
}

// CommunityListResponse summarizes communities.
type CommunityListResponse struct {
	Communities []GraphCommunityPayload    `json:"communities"`
	Stats       obsidian.GraphStatsSummary `json:"stats"`
	OrphanCount int                        `json:"orphanCount,omitempty"`
	Orphans     []string                   `json:"orphans,omitempty"`
	Components  []ComponentSummary         `json:"components,omitempty"`
}

// ComponentSummary captures weak component sizes for global structure awareness.
type ComponentSummary struct {
	ID              string  `json:"id"`
	Size            int     `json:"size"`
	FractionOfVault float64 `json:"fractionOfVault,omitempty"`
}

// OrphansResponse describes orphaned note paths.
type OrphansResponse struct {
	Orphans []string `json:"orphans"`
}

// NeighborRef captures a neighbor path with its community for richer context.
type NeighborRef struct {
	Path      string `json:"path"`
	Community string `json:"community,omitempty"`
}

// NoteGraphContext summarizes graph metrics for a single note.
type NoteGraphContext struct {
	Inbound             int     `json:"inbound"`
	Outbound            int     `json:"outbound"`
	Hub                 float64 `json:"hub"`                           // HITS hub score
	HubPercentile       float64 `json:"hubPercentile,omitempty"`       // Percentile rank for hub score
	Authority           float64 `json:"authority"`                     // HITS authority score
	AuthorityPercentile float64 `json:"authorityPercentile,omitempty"` // Percentile rank for authority score
	IsOrphan            bool    `json:"isOrphan"`
	WeakComponent       string  `json:"weakComponent,omitempty"`
	StrongComponent     string  `json:"strongComponent,omitempty"`
}

// NoteCommunityContext captures the community around a note.
type NoteCommunityContext struct {
	ID               string                   `json:"id"`
	Size             int                      `json:"size"`
	FractionOfVault  float64                  `json:"fractionOfVault,omitempty"`
	Density          float64                  `json:"density,omitempty"`
	Anchor           string                   `json:"anchor,omitempty"`
	TopTags          []obsidian.TagCount      `json:"topTags,omitempty"`
	TopAuthority     []AuthorityScorePayload  `json:"topAuthority,omitempty"`
	AuthorityBuckets []AuthorityBucketPayload `json:"authorityBuckets,omitempty"`
	AuthorityStats   *AuthorityStatsPayload   `json:"authorityStats,omitempty"`
	Recency          *GraphRecencyPayload     `json:"recency,omitempty"`
	Bridges          []BridgePayload          `json:"bridges,omitempty"`
	IsBridge         bool                     `json:"isBridge,omitempty"`
}

// NoteNeighbors distinguishes inbound/outbound and community boundaries.
type NoteNeighbors struct {
	LinksOut       []NeighborRef `json:"linksOut,omitempty"`
	LinksIn        []NeighborRef `json:"linksIn,omitempty"`
	SameCommunity  []string      `json:"sameCommunity,omitempty"`
	CrossCommunity []NeighborRef `json:"crossCommunity,omitempty"`
}

// RelatedNotePayload captures semantic neighbors for note/vault context.
type RelatedNotePayload struct {
	Path       string  `json:"path"`
	Title      string  `json:"title,omitempty"`
	Score      float64 `json:"score,omitempty"`
	GraphScore float64 `json:"graphScore,omitempty"`
	FinalScore float64 `json:"finalScore,omitempty"`
	Breadcrumb string  `json:"breadcrumb,omitempty"`
	Heading    string  `json:"heading,omitempty"`
	ChunkIndex int     `json:"chunkIndex,omitempty"`
}

// SemanticMatchPayload captures note or code matches for semantic_query.
type SemanticMatchPayload struct {
	Type             string                   `json:"type"`
	Path             string                   `json:"path,omitempty"`
	Title            string                   `json:"title,omitempty"`
	Role             string                   `json:"role,omitempty"`
	Symbol           string                   `json:"symbol,omitempty"`
	FQN              string                   `json:"fqn,omitempty"`
	AnchorID         string                   `json:"anchorID,omitempty"`
	NodeID           string                   `json:"nodeID,omitempty"`
	NodeRefJSON      string                   `json:"nodeRefJson,omitempty"`
	SourceLocator    string                   `json:"sourceLocator,omitempty"`
	NodeKind         string                   `json:"nodeKind,omitempty"`
	NodeType         string                   `json:"nodeType,omitempty"`
	NoteType         string                   `json:"noteType,omitempty"`
	ParentNodeID     string                   `json:"parentNodeID,omitempty"`
	Kind             string                   `json:"kind,omitempty"`
	Granularity      string                   `json:"granularity,omitempty"`
	ChunkIndex       int                      `json:"chunkIndex,omitempty"`
	StartLine        int                      `json:"startLine,omitempty"`
	EndLine          int                      `json:"endLine,omitempty"`
	Specificity      float64                  `json:"specificity,omitempty"`
	Score            float64                  `json:"score,omitempty"`
	Symbols          []SymbolHit              `json:"symbols,omitempty"`
	Breadcrumb       string                   `json:"breadcrumb,omitempty"`
	Heading          string                   `json:"heading,omitempty"`
	Text             string                   `json:"text,omitempty"`
	Included         bool                     `json:"included,omitempty"`
	Evidence         map[string]float64       `json:"evidence,omitempty"`
	Preview          string                   `json:"preview,omitempty"`
	FullFileContent  string                   `json:"fullFileContent,omitempty"`
	ExcerptContent   string                   `json:"excerptContent,omitempty"`
	OutlineContent   string                   `json:"outlineContent,omitempty"`
	SignatureContent string                   `json:"signatureContent,omitempty"`
	StubContent      string                   `json:"stubContent,omitempty"`
	NodeRef          *ontology.NodeRef        `json:"nodeRef,omitempty"`
	LinkTarget       *ontology.NodeLinkTarget `json:"linkTarget,omitempty"`
	ReferenceHint    string                   `json:"referenceHint,omitempty"`
	ContentKind      string                   `json:"contentKind,omitempty"`
	ContentTruncated bool                     `json:"contentTruncated,omitempty"`
	ContentDeduped   bool                     `json:"contentDeduped,omitempty"`
}

// SymbolHit captures actionable code identities inside a semantic result.
type SymbolHit struct {
	Symbol    string  `json:"symbol"`
	FQN       string  `json:"fqn,omitempty"`
	Kind      string  `json:"kind,omitempty"`
	StartLine int     `json:"startLine,omitempty"`
	EndLine   int     `json:"endLine,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Signature string  `json:"signature,omitempty"`
}

// ContextTextResponse wraps text-based context tools with session metadata.
type ContextTextResponse struct {
	SessionID         string                     `json:"sessionId,omitempty"`
	DedupeHits        int                        `json:"dedupeHits,omitempty"`
	Text              string                     `json:"text"`
	IndexedEnrichment *IndexedEnrichmentResponse `json:"indexedEnrichment,omitempty"`
}

type IndexedEnrichmentResponse struct {
	Status   actions.IndexedContextState     `json:"status"`
	Reads    int                             `json:"reads"`
	Results  int                             `json:"results"`
	Warnings []actions.IndexedContextWarning `json:"warnings,omitempty"`
}

// CodeLink captures a code reference from code to a note (or inverse).
type CodeLink struct {
	File    string `json:"file"`              // Source file (vault-relative)
	Line    int    `json:"line,omitempty"`    // 1-based line number
	Source  string `json:"source,omitempty"`  // e.g., wikilink, mention
	Snippet string `json:"snippet,omitempty"` // Context around the reference
}

// NoteLink represents a note linked from a code file (merged coderefs + anchors).
type NoteLink struct {
	Path        string                 `json:"path"`
	Title       string                 `json:"title,omitempty"`
	Content     string                 `json:"content,omitempty"`
	Frontmatter map[string]interface{} `json:"frontmatter,omitempty"`
	Source      string                 `json:"source,omitempty"` // reason/trace
	Kind        string                 `json:"kind,omitempty"`
	Line        int                    `json:"line,omitempty"`
	Snippet     string                 `json:"snippet,omitempty"`
}

// AncestorDocPayload represents a nearby documentation file (e.g., CONTEXT.md).
type AncestorDocPayload struct {
	Dir       string `json:"dir"`
	Pattern   string `json:"pattern"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

// InferredCommunityPayload represents a best-effort community guess for code.
type InferredCommunityPayload struct {
	ID     string `json:"id,omitempty"`
	Anchor string `json:"anchor,omitempty"`
	Size   int    `json:"size,omitempty"`
}

// NoteContextResponse is returned by file_context and vault_context.
type NoteContextResponse struct {
	Path                  string                    `json:"path"`
	FileType              string                    `json:"fileType,omitempty"`
	Title                 string                    `json:"title,omitempty"`
	Error                 string                    `json:"error,omitempty"`
	Tags                  []string                  `json:"tags,omitempty"`
	Frontmatter           map[string]interface{}    `json:"frontmatter,omitempty"`
	Graph                 NoteGraphContext          `json:"graph,omitempty"`
	Community             NoteCommunityContext      `json:"community,omitempty"`
	Neighbors             NoteNeighbors             `json:"neighbors,omitempty"`
	Backlinks             []obsidian.Backlink       `json:"backlinks,omitempty"`
	CodeLinks             []CodeLink                `json:"codeLinks,omitempty"` // References from source code
	Notes                 []NoteLink                `json:"notes,omitempty"`     // Notes linked from code
	AncestorDocs          []AncestorDocPayload      `json:"ancestorDocs,omitempty"`
	ReturnedNotePaths     []string                  `json:"returnedNotePaths,omitempty"`
	ReturnedDocPaths      []string                  `json:"returnedDocPaths,omitempty"`
	InferredCommunity     *InferredCommunityPayload `json:"inferredCommunity,omitempty"`
	CommunityTopAuthority []AuthorityScorePayload   `json:"communityTopAuthority,omitempty"`
	Truncated             bool                      `json:"truncated,omitempty"`
	NeighborsTruncated    bool                      `json:"neighborsTruncated,omitempty"`
	NeighborsLimit        int                       `json:"neighborsLimit,omitempty"`
	BacklinksTruncated    bool                      `json:"backlinksTruncated,omitempty"`
	BacklinksLimit        int                       `json:"backlinksLimit,omitempty"`
	Similarity            *float64                  `json:"similarity,omitempty"`
}

// VaultContextResponse summarizes the vault for agents.
type VaultContextResponse struct {
	Stats            obsidian.GraphStatsSummary `json:"stats"`
	OrphanCount      int                        `json:"orphanCount"`
	TopOrphans       []string                   `json:"topOrphans,omitempty"`
	Components       []ComponentSummary         `json:"components,omitempty"`
	Communities      []CommunityOverview        `json:"communities"`
	KeyNotes         []string                   `json:"keyNotes,omitempty"`
	MOCs             []KeyNoteMatch             `json:"mocs,omitempty"`
	KeyPatterns      []string                   `json:"keyPatterns,omitempty"`
	NoteContexts     []NoteContextResponse      `json:"noteContexts,omitempty"`
	CodeAnchorStatus *CodeAnchorStatus          `json:"codeAnchorStatus,omitempty"`
}

// CodeAnchorStatus reports the state of the codeanchor subsystem.
type CodeAnchorStatus struct {
	Enabled      bool              `json:"enabled"`
	WatcherAlive bool              `json:"watcherAlive"`
	FailedFiles  map[string]string `json:"failedFiles,omitempty"`
}

// CodeSymbolPayload captures an indexed symbol plus an optional source snippet.
type CodeSymbolPayload struct {
	AnchorID     string `json:"anchorId,omitempty"`
	Language     string `json:"language,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Path         string `json:"path,omitempty"`
	Symbol       string `json:"symbol,omitempty"`
	FQN          string `json:"fqn,omitempty"`
	Signature    string `json:"signature,omitempty"`
	DocComment   string `json:"docComment,omitempty"`
	StartLine    int64  `json:"startLine,omitempty"`
	EndLine      int64  `json:"endLine,omitempty"`
	Content      string `json:"content,omitempty"`
	ContentStart int    `json:"contentStartLine,omitempty"`
	ContentEnd   int    `json:"contentEndLine,omitempty"`
	TotalLines   int    `json:"totalLines,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
	Continuation string `json:"continuation,omitempty"`
}

// CodeSymbolResponse describes code_symbol lookup results.
type CodeSymbolResponse struct {
	Symbol       string              `json:"symbol"`
	Status       string              `json:"status"`
	Confidence   string              `json:"confidence"`
	Coverage     string              `json:"coverage"`
	EvidenceKind string              `json:"evidenceKind"`
	Definition   *CodeSymbolPayload  `json:"definition,omitempty"`
	Candidates   []CodeSymbolPayload `json:"candidates,omitempty"`
	Truncated    bool                `json:"truncated,omitempty"`
	NextQueries  []string            `json:"nextQueries,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
}

// CodeReferencesResponse describes callers/callees for an indexed symbol.
type CodeReferencesResponse struct {
	Symbol       string              `json:"symbol"`
	Status       string              `json:"status"`
	Confidence   string              `json:"confidence"`
	Coverage     string              `json:"coverage"`
	EvidenceKind string              `json:"evidenceKind"`
	Definition   *CodeSymbolPayload  `json:"definition,omitempty"`
	Definitions  []CodeSymbolPayload `json:"definitions,omitempty"`
	Callers      []CodeSymbolPayload `json:"callers,omitempty"`
	Callees      []CodeSymbolPayload `json:"callees,omitempty"`
	Truncated    bool                `json:"truncated,omitempty"`
	NextQueries  []string            `json:"nextQueries,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
}

// ExternalReferenceTargetPayload is an explicitly pathless, unindexed target.
type ExternalReferenceTargetPayload struct {
	Handle          string `json:"handle"`
	Ecosystem       string `json:"ecosystem"`
	Module          string `json:"module"`
	SymbolPath      string `json:"symbolPath,omitempty"`
	Kind            string `json:"kind"`
	External        bool   `json:"external"`
	Pathless        bool   `json:"pathless"`
	Indexed         bool   `json:"indexed"`
	SourceBacked    bool   `json:"sourceBacked"`
	SourceAvailable bool   `json:"sourceAvailable"`
}

// ExternalReferenceUsePayload is one local owner/path association.
type ExternalReferenceUsePayload struct {
	OwnerFQN      string `json:"ownerFqn,omitempty"`
	Path          string `json:"path"`
	Evidence      string `json:"evidence"`
	Confidence    string `json:"confidence"`
	ImportedName  string `json:"importedName,omitempty"`
	LocalName     string `json:"localName,omitempty"`
	ManifestPath  string `json:"manifestPath,omitempty"`
	DeclaredRange string `json:"declaredRange,omitempty"`
	VersionScope  string `json:"versionScope,omitempty"`
}

// ExternalReferencesResponse is the bounded query-time external impact view.
type ExternalReferencesResponse struct {
	Status     string                           `json:"status"`
	Target     *ExternalReferenceTargetPayload  `json:"target,omitempty"`
	Candidates []ExternalReferenceTargetPayload `json:"candidates,omitempty"`
	Calls      []ExternalReferenceUsePayload    `json:"calls,omitempty"`
	Types      []ExternalReferenceUsePayload    `json:"types,omitempty"`
	Members    []ExternalReferenceUsePayload    `json:"members,omitempty"`
	Imports    []ExternalReferenceUsePayload    `json:"imports,omitempty"`
	Truncated  bool                             `json:"truncated,omitempty"`
	Warnings   []string                         `json:"warnings,omitempty"`
}

// CodeSymbolContextResponse is a compact evidence packet centered on one symbol.
type CodeSymbolContextResponse struct {
	Symbol       string              `json:"symbol"`
	Status       string              `json:"status"`
	Confidence   string              `json:"confidence"`
	Coverage     string              `json:"coverage"`
	EvidenceKind string              `json:"evidenceKind"`
	Text         string              `json:"text,omitempty"`
	Definition   *CodeSymbolPayload  `json:"definition,omitempty"`
	Callers      []CodeSymbolPayload `json:"callers,omitempty"`
	Callees      []CodeSymbolPayload `json:"callees,omitempty"`
	Tests        []CodeSymbolPayload `json:"tests,omitempty"`
	Truncated    bool                `json:"truncated,omitempty"`
	NextQueries  []string            `json:"nextQueries,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
}

// CapabilitiesResponse reports server/tool readiness for agents.
type CapabilitiesResponse struct {
	Server   CapabilitiesServer   `json:"server"`
	Vault    CapabilitiesVault    `json:"vault"`
	Tools    CapabilitiesTools    `json:"tools"`
	Features CapabilitiesFeatures `json:"features,omitempty"`
	Langs    []string             `json:"languages,omitempty"`
	Limits   CapabilitiesLimits   `json:"limits,omitempty"`
	Reports  CapabilitiesReports  `json:"reports,omitempty"`
}

// CapabilitiesServer reports server version and state.
type CapabilitiesServer struct {
	Version   string `json:"version"`
	ReadWrite bool   `json:"readWrite"`
	Ready     bool   `json:"ready"`
}

// CapabilitiesVault reports vault info.
type CapabilitiesVault struct {
	Name         string `json:"name,omitempty"`
	Path         string `json:"path,omitempty"`
	IsCollection bool   `json:"isCollection,omitempty"`
}

// CapabilitiesTools reports available tool names.
type CapabilitiesTools struct {
	Available []string `json:"available"`
	Mutating  []string `json:"mutating,omitempty"`
}

// CapabilitiesFeatures reports feature flags.
type CapabilitiesFeatures struct {
	NoteEmbeddings bool `json:"noteEmbeddings,omitempty"`
	CodeEmbeddings bool `json:"codeEmbeddings,omitempty"`
	CodeAnchors    bool `json:"codeAnchors,omitempty"`
	CodeIndex      bool `json:"codeIndex,omitempty"`
}

// CapabilitiesLimits reports server limits.
type CapabilitiesLimits struct {
	BudgetChars int `json:"budgetChars,omitempty"`
}

// CapabilitiesReports reports available report operations.
type CapabilitiesReports struct {
	Ops []string `json:"ops,omitempty"`
}

// ReportResponse wraps analytics output for report tool.
type ReportResponse struct {
	Op          string      `json:"op"`
	GeneratedAt string      `json:"generatedAt"`
	Data        interface{} `json:"data"`
	Warnings    []string    `json:"warnings,omitempty"`
}

// DocCoverageRowPayload is a row in the doc_coverage report.
type DocCoverageRowPayload struct {
	Lang     string `json:"lang"`
	Kind     string `json:"kind"`
	FQN      string `json:"fqn"`
	Path     string `json:"path,omitempty"`
	Calls    int    `json:"calls"`
	Callers  int    `json:"callers"`
	Mentions int    `json:"mentions"`
	Links    int    `json:"links"`
	Resolved bool   `json:"resolved"`
}

// HotspotPackageRowPayload is a row in the hotspots packages report.
type HotspotPackageRowPayload struct {
	Lang              string `json:"lang"`
	Pkg               string `json:"pkg"`
	Calls             int    `json:"calls"`
	Callers           int    `json:"callers"`
	UsedSymbols       int    `json:"usedSymbols"`
	DocumentedSymbols int    `json:"documentedSymbols"`
}

// HotspotFileRowPayload is a row in the hotspots files report.
type HotspotFileRowPayload struct {
	File        string `json:"file"`
	Calls       int    `json:"calls"`
	Deps        int    `json:"deps"`
	UsedSymbols int    `json:"usedSymbols"`
}

// ComplexityRowPayload is a row in the complexity report.
type ComplexityRowPayload struct {
	Lang      string  `json:"lang"`
	Kind      string  `json:"kind"`
	FQN       string  `json:"fqn"`
	Path      string  `json:"path,omitempty"`
	SpanLines int     `json:"spanLines"`
	CallsIn   int     `json:"callsIn"`
	Callers   int     `json:"callers"`
	CallsOut  int     `json:"callsOut"`
	Callees   int     `json:"callees"`
	Score     float64 `json:"score"`
}

// RationaleAttentionRowPayload is a deterministic rationale review finding.
type RationaleAttentionRowPayload struct {
	Path      string   `json:"path"`
	SymbolFQN string   `json:"symbolFqn,omitempty"`
	Kind      string   `json:"kind"`
	Line      int64    `json:"line"`
	Content   string   `json:"content"`
	Calls     int      `json:"calls,omitempty"`
	Callers   int      `json:"callers,omitempty"`
	Mentions  int      `json:"mentions,omitempty"`
	Links     int      `json:"links,omitempty"`
	Attention int      `json:"attention"`
	Reasons   []string `json:"reasons"`
}

// CodeSimilarityReportPayload is the code_similarity report payload.
type CodeSimilarityReportPayload = actions.CodeSimilarityReport

// CodeSimilarityRowPayload is a ranked similar/overlapping code pair.
type CodeSimilarityRowPayload = actions.CodeSimilarityRow

// CodeSimilarityAnchorPayload identifies a source or matched code anchor.
type CodeSimilarityAnchorPayload = actions.CodeSimilarityAnchor

// CommunityOverview is a lightweight community summary for vault_context.
type CommunityOverview struct {
	ID               string                   `json:"id"`
	Size             int                      `json:"size"`
	FractionOfVault  float64                  `json:"fractionOfVault,omitempty"`
	Anchor           string                   `json:"anchor,omitempty"`
	Density          float64                  `json:"density,omitempty"`
	TopTags          []obsidian.TagCount      `json:"topTags,omitempty"`
	TopAuthority     []AuthorityScorePayload  `json:"topAuthority,omitempty"`
	AuthorityBuckets []AuthorityBucketPayload `json:"authorityBuckets,omitempty"`
	AuthorityStats   *AuthorityStatsPayload   `json:"authorityStats,omitempty"`
	Recency          *GraphRecencyPayload     `json:"recency,omitempty"`
	BridgesDetailed  []BridgePayload          `json:"bridgesDetailed,omitempty"`
}

// KeyNoteMatch captures a key/MOC note and which pattern matched.
type KeyNoteMatch struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern,omitempty"`
}

// VaultHealthResponse wraps the vault health report for JSON response.
type VaultHealthResponse struct {
	Vault                 string                               `json:"vault"`
	VaultPath             string                               `json:"vaultPath"`
	AnalyzedAt            time.Time                            `json:"analyzedAt"`
	Stats                 obsidian.HealthStats                 `json:"stats"`
	BrokenLinks           []obsidian.BrokenLink                `json:"brokenLinks,omitempty"`
	StaleNotes            []obsidian.StaleNote                 `json:"staleNotes,omitempty"`
	DeadEnds              []obsidian.DeadEndNote               `json:"deadEnds,omitempty"`
	SuggestedMerges       []obsidian.MergeSuggestion           `json:"suggestedMerges,omitempty"`
	SurprisingConnections []obsidian.SurprisingConnectionEntry `json:"surprisingConnections,omitempty"` // Enabled via include:["surprises"]
	NotesWithCodeRef      int                                  `json:"notesWithCodeRef,omitempty"`      // Notes referenced from source code
	TrulyOrphanCount      int                                  `json:"trulyOrphanCount,omitempty"`      // Notes with no backlinks AND no code refs
}
