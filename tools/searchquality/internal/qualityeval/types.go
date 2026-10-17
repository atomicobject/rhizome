// Package qualityeval evaluates frozen search result artifacts without running retrieval.
package qualityeval

import (
	"encoding/json"
)

type Corpus struct {
	Version        string `json:"version"`
	SourceRevision string `json:"sourceRevision"`
	Protocol       string `json:"protocol,omitempty"`
	FrozenAt       string `json:"frozenAt,omitempty"`
	Cases          []Case `json:"cases"`
}

type Case struct {
	ID              string              `json:"id"`
	Split           string              `json:"split"`
	Corpus          string              `json:"corpus"`
	Family          string              `json:"family"`
	Surface         string              `json:"surface"`
	Intent          string              `json:"intent"`
	Queries         []string            `json:"queries"`
	Facets          []QueryFacet        `json:"facets,omitempty"`
	Seeds           []string            `json:"seeds,omitempty"`
	Scope           string              `json:"scope,omitempty"`
	ExpectedTarget  string              `json:"expectedTarget,omitempty"`
	Relationships   []string            `json:"requiredRelationships,omitempty"`
	RequiredRoles   []string            `json:"requiredRoles,omitempty"`
	RequiredSources []string            `json:"requiredSources,omitempty"`
	Judgments       map[string]Judgment `json:"judgments"`
	Control         string              `json:"control,omitempty"`
	JudgmentMethod  string              `json:"judgmentMethod"`
	JudgedAt        string              `json:"judgedAt"`
	Uncertainty     string              `json:"uncertainty,omitempty"`
}

type QueryFacet struct {
	Text string `json:"text"`
	Mode string `json:"mode,omitempty"`
}

type Judgment struct {
	Grade                  int      `json:"grade"`
	Rationale              string   `json:"rationale"`
	Locator                string   `json:"locator"`
	SupportedRoles         []string `json:"supportedRoles,omitempty"`
	SupportedRelationships []string `json:"supportedRelationships,omitempty"`
	SupportedFacets        []string `json:"supportedFacets,omitempty"`
}

type Run struct {
	Revision             string                         `json:"revision"`
	CorpusSourceRevision string                         `json:"corpusSourceRevision,omitempty"`
	ExecutionFingerprint string                         `json:"executionFingerprint,omitempty"`
	CorpusFingerprint    string                         `json:"corpusFingerprint"`
	EmbeddingFingerprint string                         `json:"embeddingFingerprint,omitempty"`
	Mode                 string                         `json:"mode,omitempty"`
	Profile              string                         `json:"profile,omitempty"`
	AnswerSelector       string                         `json:"answerSelector,omitempty"`
	Vault                string                         `json:"vault,omitempty"`
	StartedAt            string                         `json:"startedAt,omitempty"`
	FailureCount         int                            `json:"failureCount,omitempty"`
	Results              []QueryResult                  `json:"results"`
	Corpora              map[string]CorpusRunProvenance `json:"corpora,omitempty"`
	AnswerTransform      *AnswerTransformProvenance     `json:"answerTransform,omitempty"`
}

type AnswerTransformProvenance struct {
	Method                          string  `json:"method"`
	SourceAnswerSelector            string  `json:"sourceAnswerSelector"`
	TargetAnswerSelector            string  `json:"targetAnswerSelector"`
	SourceRunSHA256                 string  `json:"sourceRunSha256"`
	SourceExecutionFingerprint      string  `json:"sourceExecutionFingerprint,omitempty"`
	TransformerExecutionFingerprint string  `json:"transformerExecutionFingerprint"`
	TransformerBinarySHA256         string  `json:"transformerBinarySha256"`
	TransformedAt                   string  `json:"transformedAt"`
	TransformDurationMS             float64 `json:"transformDurationMs"`
}

type CorpusRunProvenance struct {
	PhysicalVault            string `json:"physicalVault"`
	SourcePrefix             string `json:"sourcePrefix,omitempty"`
	SourceFingerprint        string `json:"sourceFingerprint"`
	IndexGeneration          string `json:"indexGeneration,omitempty"`
	EmbeddingProvider        string `json:"embeddingProvider,omitempty"`
	EmbeddingModel           string `json:"embeddingModel,omitempty"`
	EmbeddingDimensions      int    `json:"embeddingDimensions,omitempty"`
	EmbeddingFingerprint     string `json:"embeddingFingerprint,omitempty"`
	InventoryFingerprint     string `json:"inventoryFingerprint,omitempty"`
	IndexedFiles             int    `json:"indexedFiles,omitempty"`
	IndexedAnchors           int    `json:"indexedAnchors,omitempty"`
	IndexedDocSections       int    `json:"indexedDocSections,omitempty"`
	IndexedOntologyNodes     int    `json:"indexedOntologyNodes,omitempty"`
	IndexedChunks            int    `json:"indexedChunks,omitempty"`
	IndexedGraphEdges        int    `json:"indexedGraphEdges,omitempty"`
	IndexedIntelEdges        int    `json:"indexedIntelEdges,omitempty"`
	IndexerBinaryFingerprint string `json:"indexerBinaryFingerprint,omitempty"`
	UnresolvedGraphSources   int    `json:"unresolvedGraphSources,omitempty"`
	UnresolvedGraphTargets   int    `json:"unresolvedGraphTargets,omitempty"`
}

type QueryResult struct {
	ID                        string               `json:"id"`
	Sources                   []string             `json:"sources"`
	MustRead                  []string             `json:"mustRead,omitempty"`
	Roles                     []RoleAssignment     `json:"roles,omitempty"`
	Relationships             []EvidenceAssignment `json:"relationships,omitempty"`
	Facets                    []EvidenceAssignment `json:"facets,omitempty"`
	Confidence                string               `json:"confidence,omitempty"`
	Status                    string               `json:"status,omitempty"`
	TargetIdentities          []string             `json:"targetIdentities,omitempty"`
	Warnings                  []string             `json:"warnings,omitempty"`
	DurationMS                float64              `json:"durationMs,omitempty"`
	PageDurationsMS           []float64            `json:"pageDurationsMs,omitempty"`
	ContinuationError         string               `json:"continuationError,omitempty"`
	MissingRequiredIdentities []string             `json:"missingRequiredIdentities,omitempty"`
	Candidates                []CandidateTrace     `json:"candidates,omitempty"`
	Stages                    []StageTrace         `json:"stages,omitempty"`
	AnswerInputSnapshot       json.RawMessage      `json:"answerInputSnapshot,omitempty"`
}

type StageTrace struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind,omitempty"`
	DurationMS float64 `json:"durationMs"`
	Status     string  `json:"status,omitempty"`
}

type CandidateTrace struct {
	Source         string   `json:"source"`
	Identity       string   `json:"identity,omitempty"`
	Handle         string   `json:"handle,omitempty"`
	Page           int      `json:"page,omitempty"`
	Eligibility    string   `json:"eligibility,omitempty"`
	Relationship   string   `json:"relationship,omitempty"`
	TargetIdentity string   `json:"targetIdentity,omitempty"`
	Score          float64  `json:"score"`
	Evidence       []string `json:"evidence,omitempty"`
}

type RoleAssignment struct {
	Role   string `json:"role"`
	Source string `json:"source"`
}

type EvidenceAssignment struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

type Metrics struct {
	Cases                             int     `json:"cases"`
	NDCG10                            float64 `json:"ndcgAt10"`
	NDCG10Cases                       int     `json:"ndcgAt10Cases"`
	RequiredRecall20                  float64 `json:"requiredRecallAt20"`
	RequiredRecall20Cases             int     `json:"requiredRecallAt20Cases"`
	NavigationSuccess1                float64 `json:"navigationSuccessAt1"`
	NavigationSuccess3                float64 `json:"navigationSuccessAt3"`
	NavigationCases                   int     `json:"navigationCases"`
	MustReadPrecision                 float64 `json:"mustReadPrecision"`
	MustReadPrecisionCases            int     `json:"mustReadPrecisionCases"`
	RequiredRoleCoverage              float64 `json:"requiredRoleCoverage"`
	RequiredRoleCoverageCases         int     `json:"requiredRoleCoverageCases"`
	RequiredRelationshipCoverage      float64 `json:"requiredRelationshipCoverage"`
	RequiredRelationshipCoverageCases int     `json:"requiredRelationshipCoverageCases"`
	RequiredFacetCoverage             float64 `json:"requiredFacetCoverage"`
	RequiredFacetCoverageCases        int     `json:"requiredFacetCoverageCases"`
	PrimaryResultPurity               float64 `json:"primaryResultPurity"`
	PrimaryResultPurityCases          int     `json:"primaryResultPurityCases"`
	HighConfidencePrecision           float64 `json:"highConfidencePrecision"`
	HighConfidenceCases               int     `json:"highConfidenceCases"`
	HighConfidenceCoverage            float64 `json:"highConfidenceCoverage"`
	AnswerableCases                   int     `json:"answerableCases"`
	UnjudgedTop10                     int     `json:"unjudgedTop10"`
}

type Report struct {
	Revision                      string              `json:"revision"`
	CorpusFingerprint             string              `json:"corpusFingerprint"`
	RescoredFromCorpusFingerprint string              `json:"rescoredFromCorpusFingerprint,omitempty"`
	JudgmentIdentityProjection    *IdentityProjection `json:"judgmentIdentityProjection,omitempty"`
	Mode                          string              `json:"mode,omitempty"`
	Profile                       string              `json:"profile,omitempty"`
	Split                         string              `json:"split,omitempty"`
	RunFailureCount               int                 `json:"runFailureCount,omitempty"`
	Overall                       Metrics             `json:"overall"`
	ByCorpus                      map[string]Metrics  `json:"byCorpus"`
	ByFamily                      map[string]Metrics  `json:"byFamily"`
	Failures                      []Failure           `json:"failures,omitempty"`
}

// IdentityProjection records the judgment-only normalization applied to an
// older run that retained a file alias alongside one canonical candidate.
// The projection is deliberately unavailable to ordinary evaluation.
type IdentityProjection struct {
	Method          string `json:"method"`
	AliasesReplaced int    `json:"aliasesReplaced"`
}

type Selection struct {
	Split    string
	Corpora  []string
	Families []string
}

type Failure struct {
	ID     string `json:"id"`
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}
