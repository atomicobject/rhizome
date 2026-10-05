// Package qualityeval evaluates frozen search result artifacts without running retrieval.
package qualityeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
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

func Fingerprint(c Corpus) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]), nil
}
func Evaluate(c Corpus, r Run) (Report, error) { return EvaluateSplit(c, r, "") }
func EvaluateSplit(corpus Corpus, run Run, split string) (Report, error) {
	return EvaluateSelection(corpus, run, Selection{Split: split})
}

// EvaluateJudgmentRescore evaluates preserved retrieval results against updated
// source judgments while proving the executed query contract did not change.
// The original corpus must match the run fingerprint exactly. Only judgments
// and their assessment metadata may differ in the updated corpus.
func EvaluateJudgmentRescore(original, updated Corpus, run Run, selection Selection) (Report, error) {
	originalFingerprint, err := validateCorpus(original)
	if err != nil {
		return Report{}, fmt.Errorf("validate original corpus: %w", err)
	}
	if run.CorpusFingerprint == "" || run.CorpusFingerprint != originalFingerprint {
		return Report{}, fmt.Errorf("original corpus fingerprint mismatch: run=%q corpus=%q", run.CorpusFingerprint, originalFingerprint)
	}
	if !sameExecutionCorpusContract(original, updated) {
		return Report{}, fmt.Errorf("judgment rescore changed the corpus execution contract")
	}
	updatedFingerprint, err := validateCorpus(updated)
	if err != nil {
		return Report{}, fmt.Errorf("validate updated corpus: %w", err)
	}
	rescoredRun, projected, err := projectUniqueJudgmentIdentities(updated, run, selection)
	if err != nil {
		return Report{}, err
	}
	rescoredRun.CorpusFingerprint = updatedFingerprint
	report, err := EvaluateSelection(updated, rescoredRun, selection)
	if err != nil {
		return Report{}, err
	}
	report.RescoredFromCorpusFingerprint = originalFingerprint
	if projected > 0 {
		report.JudgmentIdentityProjection = &IdentityProjection{
			Method:          "unique-candidate-identity-v1",
			AliasesReplaced: projected,
		}
	}
	return report, nil
}

func projectUniqueJudgmentIdentities(corpus Corpus, run Run, selection Selection) (Run, int, error) {
	cases := make(map[string]Case, len(corpus.Cases))
	for _, c := range corpus.Cases {
		if (selection.Split == "" || c.Split == selection.Split) && selectedValue(c.Corpus, selection.Corpora) && selectedValue(c.Family, selection.Families) {
			cases[c.ID] = c
		}
	}
	out := run
	out.Results = append([]QueryResult(nil), run.Results...)
	projected := 0
	for i := range out.Results {
		c, selected := cases[out.Results[i].ID]
		if !selected {
			continue
		}
		result, count, err := projectResultJudgmentIdentities(c, out.Results[i])
		if err != nil {
			return Run{}, 0, fmt.Errorf("result %s: %w", out.Results[i].ID, err)
		}
		out.Results[i] = result
		projected += count
	}
	return out, projected, nil
}

func projectResultJudgmentIdentities(c Case, result QueryResult) (QueryResult, int, error) {
	identitiesByAlias := map[string]map[string]struct{}{}
	unknownIdentityByAlias := map[string]bool{}
	for _, candidate := range result.Candidates {
		identity := strings.TrimSpace(candidate.Identity)
		if identity == "" {
			unknownIdentityByAlias[candidate.Source] = true
			continue
		}
		if identitiesByAlias[candidate.Source] == nil {
			identitiesByAlias[candidate.Source] = map[string]struct{}{}
		}
		identitiesByAlias[candidate.Source][identity] = struct{}{}
	}
	replacements := map[string]string{}
	for alias, identities := range identitiesByAlias {
		if _, fileJudged := c.Judgments[alias]; fileJudged {
			continue
		}
		var judged []string
		for identity := range identities {
			if _, ok := c.Judgments[identity]; ok {
				judged = append(judged, identity)
			}
		}
		if len(judged) == 0 {
			continue
		}
		if unknownIdentityByAlias[alias] || len(identities) != 1 || len(judged) != 1 {
			sort.Strings(judged)
			return QueryResult{}, 0, fmt.Errorf("ambiguous judgment identity projection for source %q across %d candidate identities (unknown=%t)", alias, len(identities), unknownIdentityByAlias[alias])
		}
		replacements[alias] = judged[0]
	}
	if len(replacements) == 0 {
		return result, 0, nil
	}
	replace := func(source string) string {
		if identity := replacements[source]; identity != "" {
			return identity
		}
		return source
	}
	out := result
	out.Sources = append([]string(nil), result.Sources...)
	out.MustRead = append([]string(nil), result.MustRead...)
	out.Roles = append([]RoleAssignment(nil), result.Roles...)
	out.Relationships = append([]EvidenceAssignment(nil), result.Relationships...)
	out.Facets = append([]EvidenceAssignment(nil), result.Facets...)
	out.Candidates = append([]CandidateTrace(nil), result.Candidates...)
	for i := range out.Sources {
		out.Sources[i] = replace(out.Sources[i])
	}
	for i := range out.MustRead {
		out.MustRead[i] = replace(out.MustRead[i])
	}
	for i := range out.Roles {
		out.Roles[i].Source = replace(out.Roles[i].Source)
	}
	for _, assignments := range [][]EvidenceAssignment{out.Relationships, out.Facets} {
		for i := range assignments {
			assignments[i].Source = replace(assignments[i].Source)
		}
	}
	for i := range out.Candidates {
		out.Candidates[i].Source = replace(out.Candidates[i].Source)
	}
	if duplicate := firstDuplicate(out.Sources); duplicate != "" {
		return QueryResult{}, 0, fmt.Errorf("judgment identity projection repeats source %q", duplicate)
	}
	if duplicate := firstDuplicate(out.MustRead); duplicate != "" {
		return QueryResult{}, 0, fmt.Errorf("judgment identity projection repeats must-read source %q", duplicate)
	}
	return out, len(replacements), nil
}

func sameExecutionCorpusContract(a, b Corpus) bool {
	strip := func(c Corpus) Corpus {
		out := c
		out.Cases = append([]Case(nil), c.Cases...)
		for i := range out.Cases {
			out.Cases[i].Judgments = nil
			out.Cases[i].JudgmentMethod = ""
			out.Cases[i].JudgedAt = ""
		}
		return out
	}
	aJSON, aErr := json.Marshal(strip(a))
	bJSON, bErr := json.Marshal(strip(b))
	return aErr == nil && bErr == nil && string(aJSON) == string(bJSON)
}

func EvaluateSelection(corpus Corpus, run Run, selection Selection) (Report, error) {
	fingerprint, err := validateCorpus(corpus)
	if err != nil {
		return Report{}, err
	}
	if run.CorpusFingerprint == "" || run.CorpusFingerprint != fingerprint {
		return Report{}, fmt.Errorf("corpus fingerprint mismatch: run=%q corpus=%q", run.CorpusFingerprint, fingerprint)
	}
	selected := make([]Case, 0, len(corpus.Cases))
	for _, c := range corpus.Cases {
		if (selection.Split == "" || c.Split == selection.Split) && selectedValue(c.Corpus, selection.Corpora) && selectedValue(c.Family, selection.Families) {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return Report{}, fmt.Errorf("evaluation selection has no cases")
	}
	results, err := validateRun(run, selected)
	if err != nil {
		return Report{}, err
	}
	report := Report{Revision: run.Revision, CorpusFingerprint: fingerprint, Mode: run.Mode, Profile: run.Profile, Split: selection.Split, RunFailureCount: run.FailureCount, ByCorpus: map[string]Metrics{}, ByFamily: map[string]Metrics{}}
	all := make([]scoredCase, 0, len(selected))
	byCorpus := map[string][]scoredCase{}
	byFamily := map[string][]scoredCase{}
	for _, c := range selected {
		result, ok := results[c.ID]
		if !ok {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "runner", Reason: "missing result"})
		}
		s := scoreCase(c, result)
		all = append(all, s)
		byCorpus[c.Corpus] = append(byCorpus[c.Corpus], s)
		byFamily[c.Family] = append(byFamily[c.Family], s)
		if s.unjudged > 0 {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "judgment", Reason: fmt.Sprintf("%d unjudged sources in top 10", s.unjudged)})
		}
		if c.Control == "no_answer" && result.Confidence == "high" {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "confidence", Reason: "no-answer control reported high confidence"})
		}
		if result.ContinuationError != "" {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "continuation", Reason: result.ContinuationError})
		}
		if ok {
			report.Failures = append(report.Failures, caseFailures(c, s)...)
		}
	}
	report.Overall = aggregate(all)
	for k, v := range byCorpus {
		report.ByCorpus[k] = aggregate(v)
	}
	for k, v := range byFamily {
		report.ByFamily[k] = aggregate(v)
	}
	sort.Slice(report.Failures, func(i, j int) bool {
		if report.Failures[i].ID != report.Failures[j].ID {
			return report.Failures[i].ID < report.Failures[j].ID
		}
		return report.Failures[i].Stage < report.Failures[j].Stage
	})
	return report, nil
}

func caseFailures(c Case, scored scoredCase) []Failure {
	var failures []Failure
	add := func(stage, reason string) {
		failures = append(failures, Failure{ID: c.ID, Stage: stage, Reason: reason})
	}
	if scored.recallEligible && scored.recall < 1 {
		add("source_selection", fmt.Sprintf("required source recall@20 %.3f", scored.recall))
	}
	if scored.navEligible && scored.nav1 < 1 {
		add("navigation", fmt.Sprintf("required source first appears after rank 1; success@3 %.3f", scored.nav3))
	}
	if scored.mustReadEligible && scored.mustRead < 1 {
		add("answer_selection", fmt.Sprintf("must-read precision %.3f", scored.mustRead))
	}
	if scored.rolesEligible && scored.roles < 1 {
		add("role_coverage", fmt.Sprintf("required role coverage %.3f", scored.roles))
	}
	if scored.relationshipsEligible && scored.relationships < 1 {
		add("relationship_coverage", fmt.Sprintf("required relationship coverage %.3f", scored.relationships))
	}
	if scored.facetsEligible && scored.facets < 1 {
		add("facet_coverage", fmt.Sprintf("required facet coverage %.3f", scored.facets))
	}
	if scored.primaryEligible && scored.primaryPurity < 1 {
		add("primary_result_purity", fmt.Sprintf("correct target and relationship among primary results %.3f", scored.primaryPurity))
	}
	if scored.high && !scored.highCorrect && c.Control != "no_answer" {
		add("confidence", "high confidence lacks fully relevant, covered, and resolved evidence")
	}
	return failures
}

func selectedValue(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func validateCorpus(c Corpus) (string, error) {
	if c.Version == "" || c.SourceRevision == "" {
		return "", fmt.Errorf("corpus version and sourceRevision are required")
	}
	seen := map[string]struct{}{}
	for _, x := range c.Cases {
		if x.ID == "" {
			return "", fmt.Errorf("corpus case id is required")
		}
		if _, ok := seen[x.ID]; ok {
			return "", fmt.Errorf("duplicate corpus case id %q", x.ID)
		}
		seen[x.ID] = struct{}{}
		for source, j := range x.Judgments {
			if source == "" || j.Grade < 0 || j.Grade > 3 {
				return "", fmt.Errorf("case %s has invalid judgment for %q", x.ID, source)
			}
		}
	}
	if c.Protocol == "search-engine-excellence-v1" {
		if err := validateExcellenceComposition(c.Cases); err != nil {
			return "", err
		}
	}
	return Fingerprint(c)
}

func validateExcellenceComposition(cases []Case) error {
	wantFamilies := map[string]int{
		"navigation":           64,
		"structural_precision": 64,
		"conceptual_discovery": 96,
		"multi_facet":          48,
		"control":              48,
	}
	if len(cases) < 320 {
		return fmt.Errorf("excellence corpus has %d cases; need at least 320", len(cases))
	}
	byCorpus, byFamily, bySplit := map[string]int{}, map[string]int{}, map[string]int{}
	noAnswer := 0
	for _, c := range cases {
		byCorpus[c.Corpus]++
		byFamily[c.Family]++
		bySplit[c.Split]++
		if c.Control == "no_answer" {
			noAnswer++
		}
		if c.JudgmentMethod == "" || c.JudgedAt == "" {
			return fmt.Errorf("case %s lacks judgment provenance", c.ID)
		}
	}
	if len(byCorpus) != 4 {
		return fmt.Errorf("excellence corpus has %d corpus families; need 4", len(byCorpus))
	}
	for corpus, count := range byCorpus {
		if count < 80 {
			return fmt.Errorf("corpus %s has %d cases; need at least 80", corpus, count)
		}
	}
	for family, want := range wantFamilies {
		if byFamily[family] < want {
			return fmt.Errorf("family %s has %d cases; need at least %d", family, byFamily[family], want)
		}
	}
	if noAnswer < 24 {
		return fmt.Errorf("excellence corpus has %d no-answer controls; need at least 24", noAnswer)
	}
	if bySplit["development"] == 0 || bySplit["held-out"] == 0 {
		return fmt.Errorf("excellence corpus requires development and held-out splits")
	}
	return nil
}
func validateRun(run Run, selected []Case) (map[string]QueryResult, error) {
	allowed := map[string]struct{}{}
	for _, c := range selected {
		allowed[c.ID] = struct{}{}
	}
	out := map[string]QueryResult{}
	for _, r := range run.Results {
		if _, ok := allowed[r.ID]; !ok {
			continue
		}
		if _, ok := out[r.ID]; ok {
			return nil, fmt.Errorf("duplicate result id %q", r.ID)
		}
		if d := firstDuplicate(r.Sources); d != "" {
			return nil, fmt.Errorf("result %s repeats source %q", r.ID, d)
		}
		if d := firstDuplicate(r.MustRead); d != "" {
			return nil, fmt.Errorf("result %s repeats must-read source %q", r.ID, d)
		}
		sources := map[string]struct{}{}
		for _, s := range r.Sources {
			sources[s] = struct{}{}
		}
		for _, s := range r.MustRead {
			if _, ok := sources[s]; !ok {
				return nil, fmt.Errorf("result %s must-read source %q is absent from sources", r.ID, s)
			}
		}
		for _, a := range r.Roles {
			if a.Role == "" || a.Source == "" {
				return nil, fmt.Errorf("result %s has incomplete role assignment", r.ID)
			}
			if _, ok := sources[a.Source]; !ok {
				return nil, fmt.Errorf("result %s role source %q is absent from sources", r.ID, a.Source)
			}
		}
		for _, assignments := range [][]EvidenceAssignment{r.Relationships, r.Facets} {
			for _, assignment := range assignments {
				if assignment.Value == "" || assignment.Source == "" {
					return nil, fmt.Errorf("result %s has incomplete evidence assignment", r.ID)
				}
				if _, ok := sources[assignment.Source]; !ok {
					return nil, fmt.Errorf("result %s evidence source %q is absent from sources", r.ID, assignment.Source)
				}
			}
		}
		for _, candidate := range r.Candidates {
			if candidate.Source == "" {
				return nil, fmt.Errorf("result %s has candidate without source", r.ID)
			}
			if _, ok := sources[candidate.Source]; !ok {
				return nil, fmt.Errorf("result %s candidate source %q is absent from sources", r.ID, candidate.Source)
			}
			if candidate.Eligibility != "" && candidate.Eligibility != "primary" && candidate.Eligibility != "supporting" {
				return nil, fmt.Errorf("result %s candidate source %q has invalid eligibility %q", r.ID, candidate.Source, candidate.Eligibility)
			}
		}
		out[r.ID] = r
	}
	return out, nil
}
func firstDuplicate(v []string) string {
	seen := map[string]struct{}{}
	for _, x := range v {
		if _, ok := seen[x]; ok {
			return x
		}
		seen[x] = struct{}{}
	}
	return ""
}

type scoredCase struct {
	ndcg, recall, nav1, nav3, mustRead, roles, relationships, facets, primaryPurity                                                    float64
	ndcgEligible, recallEligible, navEligible, mustReadEligible, rolesEligible, relationshipsEligible, facetsEligible, primaryEligible bool
	high, highCorrect, highAnswerable, answerable                                                                                      bool
	unjudged                                                                                                                           int
}

func scoreCase(c Case, r QueryResult) scoredCase {
	s := scoredCase{answerable: c.Control != "no_answer"}
	s.ndcg, s.unjudged, s.ndcgEligible = ndcg(c.Judgments, r.Sources, 10)
	s.recallEligible = len(c.RequiredSources) > 0
	if s.recallEligible {
		s.recall = recall(c.RequiredSources, r.Sources, 20)
	}
	if c.Family == "navigation" && len(c.RequiredSources) > 0 && c.Control == "" {
		s.navEligible = true
		s.nav1 = containsAny(r.Sources, c.RequiredSources, 1)
		s.nav3 = containsAny(r.Sources, c.RequiredSources, 3)
	}
	s.mustReadEligible = s.answerable
	if s.mustReadEligible {
		s.mustRead = precision(c.Judgments, r.MustRead)
	}
	s.rolesEligible = len(c.RequiredRoles) > 0
	if s.rolesEligible {
		s.roles = roleCoverage(c, r.Roles)
	}
	s.relationshipsEligible = len(c.Relationships) > 0
	if s.relationshipsEligible {
		s.relationships = evidenceCoverage(c.Relationships, c.Judgments, r.Relationships, func(j Judgment) []string { return j.SupportedRelationships })
	}
	s.facetsEligible = len(c.Facets) > 1
	if s.facetsEligible {
		requiredFacets := make([]string, 0, len(c.Facets))
		for _, facet := range c.Facets {
			requiredFacets = append(requiredFacets, facet.Text)
		}
		s.facets = evidenceCoverage(requiredFacets, c.Judgments, r.Facets, func(j Judgment) []string { return j.SupportedFacets })
	}
	s.primaryEligible = c.Family == "structural_precision" && len(c.Relationships) > 0
	if s.primaryEligible {
		s.primaryPurity = primaryResultPurity(c, r)
	}
	s.high = r.Confidence == "high"
	s.highAnswerable = s.high && s.answerable
	relevant := !s.mustReadEligible || s.mustRead == 1
	covered := (!s.rolesEligible || s.roles == 1) && (!s.relationshipsEligible || s.relationships == 1) && (!s.facetsEligible || s.facets == 1)
	s.highCorrect = s.high && s.answerable && relevant && covered && r.Status != "ambiguous" && r.Status != "unresolved"
	return s
}
func aggregate(cases []scoredCase) Metrics {
	m := Metrics{Cases: len(cases)}
	for _, c := range cases {
		m.UnjudgedTop10 += c.unjudged
		if c.ndcgEligible {
			m.NDCG10 += c.ndcg
			m.NDCG10Cases++
		}
		if c.recallEligible {
			m.RequiredRecall20 += c.recall
			m.RequiredRecall20Cases++
		}
		if c.navEligible {
			m.NavigationSuccess1 += c.nav1
			m.NavigationSuccess3 += c.nav3
			m.NavigationCases++
		}
		if c.mustReadEligible {
			m.MustReadPrecision += c.mustRead
			m.MustReadPrecisionCases++
		}
		if c.rolesEligible {
			m.RequiredRoleCoverage += c.roles
			m.RequiredRoleCoverageCases++
		}
		if c.relationshipsEligible {
			m.RequiredRelationshipCoverage += c.relationships
			m.RequiredRelationshipCoverageCases++
		}
		if c.facetsEligible {
			m.RequiredFacetCoverage += c.facets
			m.RequiredFacetCoverageCases++
		}
		if c.primaryEligible {
			m.PrimaryResultPurity += c.primaryPurity
			m.PrimaryResultPurityCases++
		}
		if c.highAnswerable {
			m.HighConfidenceCases++
			if c.highCorrect {
				m.HighConfidencePrecision++
			}
		}
		if c.answerable {
			m.AnswerableCases++
			if c.highAnswerable {
				m.HighConfidenceCoverage++
			}
		}
	}
	if m.NDCG10Cases > 0 {
		m.NDCG10 /= float64(m.NDCG10Cases)
	}
	if m.RequiredRecall20Cases > 0 {
		m.RequiredRecall20 /= float64(m.RequiredRecall20Cases)
	}
	if m.NavigationCases > 0 {
		m.NavigationSuccess1 /= float64(m.NavigationCases)
		m.NavigationSuccess3 /= float64(m.NavigationCases)
	}
	if m.MustReadPrecisionCases > 0 {
		m.MustReadPrecision /= float64(m.MustReadPrecisionCases)
	}
	if m.RequiredRoleCoverageCases > 0 {
		m.RequiredRoleCoverage /= float64(m.RequiredRoleCoverageCases)
	}
	if m.RequiredRelationshipCoverageCases > 0 {
		m.RequiredRelationshipCoverage /= float64(m.RequiredRelationshipCoverageCases)
	}
	if m.RequiredFacetCoverageCases > 0 {
		m.RequiredFacetCoverage /= float64(m.RequiredFacetCoverageCases)
	}
	if m.PrimaryResultPurityCases > 0 {
		m.PrimaryResultPurity /= float64(m.PrimaryResultPurityCases)
	}
	if m.HighConfidenceCases > 0 {
		m.HighConfidencePrecision /= float64(m.HighConfidenceCases)
	}
	if m.AnswerableCases > 0 {
		m.HighConfidenceCoverage /= float64(m.AnswerableCases)
	}
	return m
}
func ndcg(j map[string]Judgment, sources []string, k int) (float64, int, bool) {
	grades := []int{}
	for _, x := range j {
		if x.Grade > 0 {
			grades = append(grades, x.Grade)
		}
	}
	if len(grades) == 0 {
		return 0, 0, false
	}
	dcg, u := 0.0, 0
	for i, s := range sources {
		if i >= k {
			break
		}
		x, ok := j[s]
		if !ok {
			u++
			continue
		}
		dcg += (math.Pow(2, float64(x.Grade)) - 1) / math.Log2(float64(i)+2)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))
	ideal := 0.0
	for i, g := range grades {
		if i >= k {
			break
		}
		ideal += (math.Pow(2, float64(g)) - 1) / math.Log2(float64(i)+2)
	}
	return dcg / ideal, u, true
}
func recall(required, got []string, limit int) float64 {
	if limit <= 0 || limit > len(got) {
		limit = len(got)
	}
	seen := map[string]struct{}{}
	for _, v := range got[:limit] {
		seen[v] = struct{}{}
	}
	n := 0
	for _, v := range required {
		if _, ok := seen[v]; ok {
			n++
		}
	}
	return float64(n) / float64(len(required))
}
func precision(j map[string]Judgment, selected []string) float64 {
	if len(selected) == 0 {
		return 0
	}
	n := 0
	for _, s := range selected {
		if j[s].Grade >= 2 {
			n++
		}
	}
	return float64(n) / float64(len(selected))
}
func roleCoverage(c Case, a []RoleAssignment) float64 {
	covered := map[string]struct{}{}
	for _, x := range a {
		judgment := c.Judgments[x.Source]
		if judgment.Grade >= 2 && containsStringFold(judgment.SupportedRoles, x.Role) {
			covered[x.Role] = struct{}{}
		}
	}
	n := 0
	for _, role := range c.RequiredRoles {
		if _, ok := covered[role]; ok {
			n++
		}
	}
	return float64(n) / float64(len(c.RequiredRoles))
}

func evidenceCoverage(required []string, judgments map[string]Judgment, assignments []EvidenceAssignment, supported func(Judgment) []string) float64 {
	if len(required) == 0 {
		return 0
	}
	covered := map[string]struct{}{}
	for _, assignment := range assignments {
		judgment := judgments[assignment.Source]
		if judgment.Grade >= 2 && containsStringFold(supported(judgment), assignment.Value) {
			covered[strings.ToLower(strings.TrimSpace(assignment.Value))] = struct{}{}
		}
	}
	n := 0
	for _, value := range required {
		if _, ok := covered[strings.ToLower(strings.TrimSpace(value))]; ok {
			n++
		}
	}
	return float64(n) / float64(len(required))
}

func primaryResultPurity(c Case, r QueryResult) float64 {
	resolved := r.Status == "explicit_path" || r.Status == "inferred_path" || r.Status == "inferred_symbol"
	targetCorrect := resolved && (strings.TrimSpace(c.ExpectedTarget) == "" || containsExactIdentity(r.TargetIdentities, c.ExpectedTarget))
	primary, correct := 0, 0
	for _, candidate := range r.Candidates {
		if candidate.Eligibility != "primary" || candidate.Page > 1 {
			continue
		}
		primary++
		judgment, ok := c.Judgments[candidate.Identity]
		if !ok || judgment.Grade < 2 || !targetCorrect || !containsStringFold(c.Relationships, candidate.Relationship) || !containsStringFold(judgment.SupportedRelationships, candidate.Relationship) {
			continue
		}
		correct++
	}
	if primary == 0 {
		return 0
	}
	return float64(correct) / float64(primary)
}

func containsExactIdentity(values []string, target string) bool {
	target = filepath.ToSlash(strings.TrimSpace(target))
	for _, value := range values {
		if filepath.ToSlash(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
func containsAny(got, required []string, limit int) float64 {
	if limit > len(got) {
		limit = len(got)
	}
	for _, v := range got[:limit] {
		for _, w := range required {
			if v == w {
				return 1
			}
		}
	}
	return 0
}
