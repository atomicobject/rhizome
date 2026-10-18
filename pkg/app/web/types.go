package web

import (
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"

	"github.com/atomicobject/rhizome/pkg/search"

	"time"
)

// StatusResponse reports basic server and indexing state.
//
// IndexState is "initializing" until the runtime gate opens, then flips to
// "ready" exactly once. Clients should treat the transition as a refetch
// trigger for index-dependent reads.
type StatusResponse struct {
	VaultName   string                `json:"vaultName"`
	VaultPath   string                `json:"vaultPath"`
	IndexPath   string                `json:"indexPath,omitempty"`
	Embeddings  bool                  `json:"embeddings"`
	CodeIndex   bool                  `json:"codeIndex"`
	Validation  ValidationStatusInfo  `json:"validation"`
	UpdatedAt   time.Time             `json:"updatedAt"`
	Ready       bool                  `json:"ready"`
	ReadyReason string                `json:"readyReason,omitempty"`
	IndexState  string                `json:"indexState"`
	Live        *bootstrap.LiveHealth `json:"live,omitempty"`
}

type ValidationStatusInfo struct {
	Status              string     `json:"status"`
	Health              string     `json:"health"`
	Generation          int64      `json:"generation"`
	PublishedGeneration int64      `json:"publishedGeneration"`
	ComputedAt          *time.Time `json:"computedAt,omitempty"`
	DurationMs          int64      `json:"durationMs,omitempty"`
	Error               string     `json:"error,omitempty"`
}

// PublicCapabilitiesResponse describes the stable v1 API contract available to
// custom applications. It is intentionally denormalized so clients can make one
// startup call before deciding which REST, SSE, and GraphQL surfaces to use.
type PublicCapabilitiesResponse struct {
	RhizomeVersion   string                 `json:"rhizomeVersion"`
	APIVersion       string                 `json:"apiVersion"`
	Vault            PublicVaultInfo        `json:"vault"`
	Status           StatusResponse         `json:"status"`
	GraphQL          PublicGraphQLInfo      `json:"graphql"`
	REST             PublicRESTInfo         `json:"rest"`
	Events           PublicEventsInfo       `json:"events"`
	QueryRecipes     PublicQueryRecipeInfo  `json:"queryRecipes"`
	Views            PublicViewsInfo        `json:"views"`
	Validation       PublicValidationInfo   `json:"validation"`
	Index            PublicIndexInfo        `json:"index"`
	Providers        PublicProviderInfo     `json:"providers"`
	EditSessions     PublicEditSessionsInfo `json:"editSessions"`
	Errors           PublicErrorsInfo       `json:"errors"`
	DegradedReasons  []string               `json:"degradedReasons,omitempty"`
	GeneratedAt      time.Time              `json:"generatedAt"`
	CompatibilityURL string                 `json:"compatibilityUrl,omitempty"`
}

type PublicVaultInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type PublicGraphQLInfo struct {
	Available      bool     `json:"available"`
	Endpoint       string   `json:"endpoint"`
	SchemaEndpoint string   `json:"schemaEndpoint"`
	ExplorerURL    string   `json:"explorerUrl,omitempty"`
	SchemaHash     string   `json:"schemaHash,omitempty"`
	OntologyHash   string   `json:"ontologyHash,omitempty"`
	Introspection  bool     `json:"introspection"`
	Operations     []string `json:"operations,omitempty"`
}

type PublicRESTInfo struct {
	BasePath  string   `json:"basePath"`
	OpenAPI   string   `json:"openapi"`
	Resources []string `json:"resources"`
}

type PublicEventsInfo struct {
	Endpoint          string   `json:"endpoint"`
	NodeEndpoint      string   `json:"nodeEndpoint,omitempty"`
	Format            string   `json:"format"`
	Replay            string   `json:"replay"`
	Heartbeat         string   `json:"heartbeat"`
	Reconnect         string   `json:"reconnect"`
	Invalidations     []string `json:"invalidations"`
	StreamedKinds     []string `json:"streamedKinds"`
	NodeStreamedKinds []string `json:"nodeStreamedKinds,omitempty"`
	SupportedKinds    []string `json:"supportedKinds"`
}

type PublicQueryRecipeInfo struct {
	Available  bool     `json:"available"`
	Generation string   `json:"generation,omitempty"`
	Count      int      `json:"count"`
	Issues     int      `json:"issues"`
	Endpoints  []string `json:"endpoints"`
}

type PublicViewsInfo struct {
	Available  bool     `json:"available"`
	Generation string   `json:"generation,omitempty"`
	Count      int      `json:"count"`
	Issues     int      `json:"issues"`
	Endpoints  []string `json:"endpoints"`
}

type PublicValidationInfo struct {
	Available       bool     `json:"available"`
	Endpoint        string   `json:"endpoint"`
	Endpoints       []string `json:"endpoints,omitempty"`
	RepairAvailable bool     `json:"repairAvailable"`
	RepairEndpoints []string `json:"repairEndpoints,omitempty"`
	Generation      string   `json:"generation,omitempty"`
	Status          string   `json:"status,omitempty"`
}

type PublicIndexInfo struct {
	Available  bool   `json:"available"`
	Generation string `json:"generation,omitempty"`
}

type PublicProviderInfo struct {
	Embeddings PublicProviderState `json:"embeddings"`
	CodeIndex  PublicProviderState `json:"codeIndex"`
}

type PublicProviderState struct {
	Available bool   `json:"available"`
	State     string `json:"state"`
}

type PublicEditSessionsInfo struct {
	Available bool     `json:"available"`
	Endpoints []string `json:"endpoints"`
}

type PublicErrorsInfo struct {
	Format string   `json:"format"`
	Codes  []string `json:"codes"`
}

type PublicQueryRecipeListResponse struct {
	Recipes []queryrecipe.Recipe `json:"recipes"`
	Issues  []queryrecipe.Issue  `json:"issues,omitempty"`
}

type PublicQueryRecipeExecuteRequest struct {
	Inputs map[string]string `json:"inputs,omitempty"`
}

// SuggestResponse returns fuzzy file suggestions.
type SuggestResponse struct {
	Query   string      `json:"query"`
	Matches []FileMatch `json:"matches"`
}

// FileMatch is a lightweight file match for suggestion/search UIs.
type FileMatch struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
	Kind  string `json:"kind"` // note|code|file
	Lang  string `json:"lang,omitempty"`
	Score int    `json:"score,omitempty"`
}

// SearchResponse mirrors semantic query results with lightweight matches.
type SearchResponse struct {
	Profile           searchapplication.Profile         `json:"profile,omitempty"`
	Policy            searchapplication.EffectivePolicy `json:"policy,omitempty"`
	Query             string                            `json:"query"`
	Returned          int                               `json:"returned,omitempty"`
	Total             int                               `json:"total,omitempty"`
	Count             int                               `json:"count"`
	ContinuationToken string                            `json:"continuationToken,omitempty"`
	Text              string                            `json:"text,omitempty"`
	Matches           []SearchMatch                     `json:"matches,omitempty"`
	Warnings          []search.Warning                  `json:"warnings,omitempty"`
	Lanes             []search.LaneStatus               `json:"lanes,omitempty"`
	TargetStatus      search.TargetStatus               `json:"targetStatus,omitempty"`
	Confidence        answer.ConfidenceReport           `json:"confidence,omitempty"`
	Coverage          answer.CoverageReport             `json:"coverage,omitempty"`
}

// SearchMatch is a trimmed semantic match payload for UI use.
type SearchMatch struct {
	Type             string                   `json:"type"`
	Path             string                   `json:"path,omitempty"`
	Title            string                   `json:"title,omitempty"`
	Symbol           string                   `json:"symbol,omitempty"`
	FQN              string                   `json:"fqn,omitempty"`
	AnchorID         string                   `json:"anchorID,omitempty"`
	NodeID           string                   `json:"nodeID,omitempty"`
	NodeRefJSON      string                   `json:"nodeRefJson,omitempty"`
	SourceLocator    string                   `json:"sourceLocator,omitempty"`
	NodeKind         string                   `json:"nodeKind,omitempty"`
	ParentNodeID     string                   `json:"parentNodeID,omitempty"`
	NodeRef          *ontology.NodeRef        `json:"nodeRef,omitempty"`
	LinkTarget       *ontology.NodeLinkTarget `json:"linkTarget,omitempty"`
	Kind             string                   `json:"kind,omitempty"`
	ChunkIndex       int                      `json:"chunkIndex,omitempty"`
	StartLine        int                      `json:"startLine,omitempty"`
	EndLine          int                      `json:"endLine,omitempty"`
	Score            float64                  `json:"score,omitempty"`
	Heading          string                   `json:"heading,omitempty"`
	Snippet          string                   `json:"snippet,omitempty"`
	SnippetKind      string                   `json:"snippetKind,omitempty"`
	SnippetStatus    string                   `json:"snippetStatus,omitempty"`
	SnippetTruncated bool                     `json:"snippetTruncated,omitempty"`
	NoteType         string                   `json:"noteType,omitempty"`
	Tags             []string                 `json:"tags,omitempty"`
}

// GraphResponse provides nodes/edges for visualization.
type GraphResponse struct {
	Nodes       []GraphNode                `json:"nodes"`
	Edges       []GraphEdge                `json:"edges"`
	Truncated   bool                       `json:"truncated,omitempty"`
	CenterID    string                     `json:"centerID,omitempty"`
	NeedsIndex  bool                       `json:"needsIndex,omitempty"`
	Diagnostics *noderead.GraphDiagnostics `json:"diagnostics,omitempty"`
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

// TreeResponse provides children for a directory.
type TreeResponse struct {
	Path      string      `json:"path"`
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// TreeEntry represents a file or directory in the tree.
type TreeEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"` // dir|note|code|file
	Lang        string `json:"lang,omitempty"`
	HasChildren bool   `json:"hasChildren"`
}

// FileViewResponse returns content + metadata for a file.
type FileViewResponse struct {
	Path         string                 `json:"path"`
	Kind         string                 `json:"kind"` // note|code|file
	Title        string                 `json:"title,omitempty"`
	Lang         string                 `json:"lang,omitempty"`
	Frontmatter  map[string]interface{} `json:"frontmatter,omitempty"`
	Content      string                 `json:"content,omitempty"`
	Links        []ResolvedLink         `json:"links,omitempty"`
	CodeRefs     []CodeRefLink          `json:"codeRefs,omitempty"`
	RelatedNotes []RelatedNote          `json:"relatedNotes,omitempty"`
}

// ErrorResponse is returned on API failures.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}
