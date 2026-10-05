package mcp

import (
	"encoding/json"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
)

// Response types

type semanticQueryResponse struct {
	Profile              searchapplication.Profile          `json:"profile,omitempty"`
	Policy               *searchapplication.EffectivePolicy `json:"policy,omitempty"`
	SessionID            string                             `json:"sessionId,omitempty"`
	DedupeHits           int                                `json:"dedupeHits,omitempty"`
	Query                string                             `json:"query"`
	Queries              []string                           `json:"queries,omitempty"`
	QueryInputs          []SemanticQueryInput               `json:"queryInputs,omitempty"`
	ModeApplied          string                             `json:"modeApplied,omitempty"`
	ModeDetected         string                             `json:"modeDetected,omitempty"`
	ModeScore            float64                            `json:"modeScore,omitempty"`
	TargetStatus         search.TargetStatus                `json:"targetStatus,omitempty"`
	ResolutionConfidence float64                            `json:"resolutionConfidence,omitempty"`
	TargetCandidates     []search.TargetCandidate           `json:"targetCandidates,omitempty"`
	Warnings             []search.Warning                   `json:"warnings,omitempty"`
	Lanes                []search.LaneStatus                `json:"lanes,omitempty"`
	Returned             int                                `json:"returned,omitempty"`
	Total                int                                `json:"total,omitempty"`
	Count                int                                `json:"count"`
	Remaining            int                                `json:"remaining,omitempty"`
	TypeCounts           map[string]int                     `json:"typeCounts,omitempty"`
	ContinuationToken    string                             `json:"continuationToken,omitempty"`
	Summary              string                             `json:"summary,omitempty"`
	MustRead             []answer.Item                      `json:"mustRead,omitempty"`
	Supporting           []answer.Item                      `json:"supporting,omitempty"`
	Coverage             answer.CoverageReport              `json:"coverage,omitempty"`
	Confidence           answer.ConfidenceReport            `json:"confidence,omitempty"`
	NextQueries          []answer.SuggestedQuery            `json:"nextQueries,omitempty"`
	Text                 string                             `json:"text,omitempty"`
	Matches              []SemanticMatchPayload             `json:"matches,omitempty"`
	Compact              *SemanticCompactResponse           `json:"compact,omitempty"`
	Diagnostics          *SemanticQueryDiagnostics          `json:"diagnostics,omitempty"`
	// AssessedSources carries transport-neutral assessments to in-process
	// callers without expanding the public wire contract.
	AssessedSources []searchapplication.SourceAssessment `json:"-"`

	// searchTimings carries the application layer's retrieval timings to the
	// opt-in diagnostics envelope without entering the budgeted payload.
	searchTimings []search.TimingEvent

	// textBodies binds renderer-owned pieces to their final session identity.
	textBodies []semanticTextBody

	// Pack metadata for reproducibility (from intel spine).
	PackMeta *codeanchor.PackMetadata `json:"packMeta,omitempty"`
}

type SemanticQueryDiagnostics struct {
	MeasurementScope string                                          `json:"measurementScope"`
	Phases           []indexingperf.SemanticQueryPhaseDiagnostic     `json:"phases"`
	Operations       []indexingperf.SemanticQueryOperationDiagnostic `json:"operations"`
	Search           []SemanticQueryTimingDiagnostic                 `json:"search"`
	Deadline         SemanticQueryDeadlineDiagnostic                 `json:"deadline"`
}

type SemanticQueryTimingDiagnostic struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	DurationMs int64  `json:"durationMs"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

type SemanticQueryDeadlineDiagnostic struct {
	Set         bool  `json:"set"`
	RemainingMs int64 `json:"remainingMs,omitempty"`
}

// SemanticQueryResponse is the exported alias for semantic query results.
type SemanticQueryResponse = semanticQueryResponse

// SemanticCompactResponse is the opt-in compact representation of a semantic
// query result. Sources own selected body text once; answer roles refer to
// those sources by stable reference.
type SemanticCompactResponse struct {
	Sources        []SemanticCompactSource `json:"sources"`
	Roles          SemanticCompactRoles    `json:"roles,omitempty"`
	OmittedSources int                     `json:"omittedSources,omitempty"`
}

// SemanticCompactSource is one actionable search result in compact mode.
type SemanticCompactSource struct {
	Ref         string                     `json:"ref"`
	Type        string                     `json:"type"`
	Path        string                     `json:"path,omitempty"`
	Title       string                     `json:"title,omitempty"`
	Symbol      string                     `json:"symbol,omitempty"`
	FQN         string                     `json:"fqn,omitempty"`
	Kind        string                     `json:"kind,omitempty"`
	Granularity string                     `json:"granularity,omitempty"`
	StartLine   int                        `json:"startLine,omitempty"`
	EndLine     int                        `json:"endLine,omitempty"`
	Score       float64                    `json:"score,omitempty"`
	Evidence    map[string]float64         `json:"evidence,omitempty"`
	Included    bool                       `json:"included,omitempty"`
	Body        *SemanticCompactSourceBody `json:"body,omitempty"`
	LinkTarget  *ontology.NodeLinkTarget   `json:"linkTarget,omitempty"`
}

// SemanticCompactSourceBody preserves the selected body representation and
// whether packing trimmed or session dedupe suppressed it.
type SemanticCompactSourceBody struct {
	Kind      string `json:"kind"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Deduped   bool   `json:"deduped,omitempty"`
}

// SemanticCompactRoles points answer selections at SemanticCompactSource.Ref.
type SemanticCompactRoles struct {
	MustRead   []string `json:"mustRead,omitempty"`
	Supporting []string `json:"supporting,omitempty"`
}

// SemanticQueryInput captures a query with an optional mode override.
type SemanticQueryInput struct {
	Text string `json:"text"`
	Mode string `json:"mode,omitempty"`
}

func marshalSemanticQueryResponse(resp semanticQueryResponse, budgetChars int) ([]byte, error) {
	if budgetChars <= 0 {
		return json.Marshal(resp)
	}
	if resp.Compact != nil {
		return marshalCompactSemanticQueryResponse(resp, budgetChars)
	}
	enc, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	// Budget trimming is ordered by agent usefulness: keep the answer packet shape
	// and source identities as long as possible, then progressively shorten previews,
	// text, secondary evidence, and diagnostics. This is a presentation boundary
	// only; ranking and answer role selection have already happened.
	resp = trimSemanticAnswerPreviews(resp, 180)
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	overflow := len(enc) - budgetChars
	allowed := len(resp.Text) - overflow - 256
	resp.Text = contextpack.TrimToBudget(resp.Text, allowed)
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	resp.Supporting = nil
	resp = trimSemanticAnswerPreviews(resp, 0)
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	overflow = len(enc) - budgetChars
	allowed = len(resp.Text) - overflow - 256
	resp.Text = contextpack.TrimToBudget(resp.Text, allowed)
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	resp.Text = ""
	resp.Matches = skeletalMatches(resp.Matches)
	resp.TypeCounts = nil
	resp.PackMeta = nil
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	resp.NextQueries = nil
	resp.TargetCandidates = nil
	resp.Warnings = nil
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	resp.MustRead = nil
	resp.Coverage = answer.CoverageReport{}
	resp.Confidence = answer.ConfidenceReport{}
	if enc, err = json.Marshal(resp); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	structuredMinimal := semanticQueryResponse{
		Query:   strings.TrimSpace(resp.Query),
		Count:   resp.Count,
		Matches: skeletalMatches(resp.Matches),
	}
	if enc, err = json.Marshal(structuredMinimal); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	structuredMinimal.Matches = compactActionableMatches(resp.Matches)
	if enc, err = json.Marshal(structuredMinimal); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}

	minimal := semanticQueryResponse{
		Query: strings.TrimSpace(resp.Query),
		Count: resp.Count,
		Text:  contextpack.TrimToBudget(resp.Text, max(0, budgetChars-128)),
	}
	if enc, err = json.Marshal(minimal); err != nil {
		return nil, err
	}
	if len(enc) <= budgetChars {
		return enc, nil
	}
	minimal.Text = ""
	return json.Marshal(minimal)
}

func compactActionableMatches(matches []SemanticMatchPayload) []SemanticMatchPayload {
	if len(matches) == 0 {
		return nil
	}
	out := make([]SemanticMatchPayload, 0, len(matches))
	for _, match := range matches {
		out = append(out, SemanticMatchPayload{
			Type:      match.Type,
			Path:      match.Path,
			Role:      match.Role,
			Symbol:    firstNonEmpty(match.Symbol, match.Title),
			StartLine: match.StartLine,
			EndLine:   match.EndLine,
			Included:  match.Included,
		})
	}
	return out
}

func skeletalMatches(matches []SemanticMatchPayload) []SemanticMatchPayload {
	if len(matches) == 0 {
		return nil
	}
	out := make([]SemanticMatchPayload, 0, len(matches))
	for _, match := range matches {
		out = append(out, SemanticMatchPayload{
			Type:          match.Type,
			Path:          match.Path,
			Title:         match.Title,
			Role:          match.Role,
			Symbol:        match.Symbol,
			FQN:           match.FQN,
			AnchorID:      match.AnchorID,
			NodeID:        match.NodeID,
			NodeRefJSON:   match.NodeRefJSON,
			SourceLocator: match.SourceLocator,
			NodeKind:      match.NodeKind,
			NodeType:      match.NodeType,
			Kind:          match.Kind,
			Granularity:   match.Granularity,
			ChunkIndex:    match.ChunkIndex,
			StartLine:     match.StartLine,
			EndLine:       match.EndLine,
			Specificity:   match.Specificity,
			Score:         match.Score,
			Symbols:       match.Symbols,
			Included:      match.Included,
			LinkTarget:    match.LinkTarget,
		})
	}
	return out
}

func trimSemanticAnswerPreviews(resp semanticQueryResponse, maxChars int) semanticQueryResponse {
	trim := func(items []answer.Item) {
		for i := range items {
			items[i].Preview = contextpack.TrimToBudget(items[i].Preview, maxChars)
		}
	}
	trim(resp.MustRead)
	trim(resp.Supporting)
	return resp
}

// Internal working types

type semanticQueryGroup struct {
	key       string
	match     SemanticMatchPayload
	score     float64
	hits      []search.RankedResult
	spread    int
	diversity int
}

type semanticQueryIntent struct {
	Overview   bool
	Tests      bool
	PreferDocs bool
	PreferCode bool
	Fetch      bool
}

type contentPlan string

const (
	planFull      contentPlan = "full"
	planExcerpt   contentPlan = "excerpt"
	planOutline   contentPlan = "outline"
	planSignature contentPlan = "signature"
	planStub      contentPlan = "stub"
)

type semanticContentOption struct {
	plan    contentPlan
	cost    int
	utility float64
	ratio   float64
}

type semanticSelection struct {
	plan contentPlan
	cost int
}

type semanticSelectionContext struct {
	selectedSummaries []string
	selectedGroups    map[string]int
	selectedCode      int
}

type semanticGroup struct {
	key     string
	label   string
	indices []int
	score   float64
	path    string
}

type semanticBody struct {
	plan        contentPlan
	body        string
	fingerprint string
	truncated   bool
}
