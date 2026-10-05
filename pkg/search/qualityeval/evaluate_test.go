package qualityeval

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrozenExcellenceCorpusMeetsProtocol(t *testing.T) {
	for _, name := range []string{"corpus-v1.json", "corpus-v2.json"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + name)
			require.NoError(t, err)
			var corpus Corpus
			require.NoError(t, json.Unmarshal(b, &corpus))
			_, err = validateCorpus(corpus)
			require.NoError(t, err)
		})
	}
}

func testCorpus(cases ...Case) Corpus {
	return Corpus{Version: "1", SourceRevision: "source", Cases: cases}
}
func testRun(t *testing.T, corpus Corpus, results ...QueryResult) Run {
	t.Helper()
	fp, err := Fingerprint(corpus)
	require.NoError(t, err)
	return Run{Revision: "candidate", CorpusFingerprint: fp, Results: results}
}

func TestEvaluateMetricsHandChecked(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Split: "dev", Corpus: "repo", Family: "navigation", RequiredSources: []string{"a"}, RequiredRoles: []string{"implementation"}, Judgments: map[string]Judgment{"a": {Grade: 3, SupportedRoles: []string{"implementation"}}, "b": {Grade: 1}}})
	report, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"a", "b"}, MustRead: []string{"a"}, Roles: []RoleAssignment{{Role: "implementation", Source: "a"}}, Confidence: "high"}))
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.NDCG10)
	require.Equal(t, 1.0, report.Overall.NavigationSuccess1)
	require.Equal(t, 1.0, report.Overall.HighConfidencePrecision)
	require.Equal(t, 1, report.Overall.NDCG10Cases)

	// Reversed grades 1,3: DCG = 1/log2(2) + 7/log2(3); ideal = 7/log2(2) + 1/log2(3).
	reversed, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"b", "a"}}))
	require.NoError(t, err)
	want := (1 + 7/math.Log2(3)) / (7 + 1/math.Log2(3))
	require.InDelta(t, want, reversed.Overall.NDCG10, 1e-12)
	require.Less(t, reversed.Overall.NDCG10, 1.0)
	require.Greater(t, reversed.Overall.NDCG10, 0.0)
}

func TestEvaluateRejectsDuplicateSourcesBeforeNDCGCanInflate(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Judgments: map[string]Judgment{"a": {Grade: 3}}})
	_, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"a", "a"}}))
	require.ErrorContains(t, err, "repeats source")
}

func TestEvaluateMissingResultStaysInDenominators(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Corpus: "repo", Family: "navigation", RequiredSources: []string{"a"}, RequiredRoles: []string{"implementation"}, Judgments: map[string]Judgment{"a": {Grade: 3}}}, Case{ID: "Q2", Corpus: "repo", Family: "navigation", RequiredSources: []string{"b"}, RequiredRoles: []string{"implementation"}, Judgments: map[string]Judgment{"b": {Grade: 3}}})
	report, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"a"}, MustRead: []string{"a"}, Roles: []RoleAssignment{{Role: "implementation", Source: "a"}}}))
	require.NoError(t, err)
	require.Equal(t, 2, report.Overall.Cases)
	require.Equal(t, 0.5, report.Overall.NavigationSuccess1)
	require.Equal(t, 0.5, report.Overall.RequiredRecall20)
	require.Contains(t, report.Failures, Failure{ID: "Q2", Stage: "runner", Reason: "missing result"})
}

func TestEvaluateNoAnswerHighConfidenceCannotInflateCoverage(t *testing.T) {
	corpus := testCorpus(Case{ID: "answer", Control: "", Judgments: map[string]Judgment{"a": {Grade: 3}}}, Case{ID: "none", Control: "no_answer", Judgments: map[string]Judgment{}})
	run := testRun(t, corpus, QueryResult{ID: "answer", Sources: []string{"a"}, MustRead: []string{"a"}, Confidence: "high"}, QueryResult{ID: "none", Confidence: "high"})
	report, err := Evaluate(corpus, run)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.HighConfidenceCoverage)
	require.Equal(t, 1.0, report.Overall.HighConfidencePrecision)
	require.Equal(t, 1, report.Overall.HighConfidenceCases)
	require.Equal(t, "confidence", report.Failures[0].Stage)
}

func TestEvaluateRolesRequireRelevantBoundSource(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", RequiredRoles: []string{"implementation"}, Judgments: map[string]Judgment{"relevant": {Grade: 3, SupportedRoles: []string{"implementation"}}, "weak": {Grade: 1, SupportedRoles: []string{"implementation"}}}})
	report, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"relevant", "weak"}, MustRead: []string{"relevant"}, Roles: []RoleAssignment{{Role: "implementation", Source: "weak"}}, Confidence: "high"}))
	require.NoError(t, err)
	require.Zero(t, report.Overall.RequiredRoleCoverage)
	require.Zero(t, report.Overall.HighConfidencePrecision)
}

func TestEvaluateRejectsRoleLabelsUnsupportedBySourceJudgment(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", RequiredRoles: []string{"implementation", "test"}, Judgments: map[string]Judgment{
		"docs/guide.md": {Grade: 3, SupportedRoles: []string{"documentation"}},
	}})
	report, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"docs/guide.md"}, MustRead: []string{"docs/guide.md"}, Roles: []RoleAssignment{
		{Role: "implementation", Source: "docs/guide.md"}, {Role: "test", Source: "docs/guide.md"},
	}, Confidence: "high"}))
	require.NoError(t, err)
	require.Zero(t, report.Overall.RequiredRoleCoverage)
	require.Zero(t, report.Overall.HighConfidencePrecision)
}

func TestEvaluateRequiresJudgedRelationshipAndFacetBindings(t *testing.T) {
	corpus := testCorpus(Case{
		ID: "Q1", Family: "multi_facet", Relationships: []string{"tests"},
		Facets: []QueryFacet{{Text: "implementation"}, {Text: "proof"}},
		Judgments: map[string]Judgment{
			"pkg/code.go":      {Grade: 3, SupportedRoles: []string{"implementation"}, SupportedFacets: []string{"implementation"}},
			"pkg/code_test.go": {Grade: 3, SupportedRoles: []string{"test"}, SupportedRelationships: []string{"tests"}, SupportedFacets: []string{"proof"}},
		},
	})
	valid := QueryResult{ID: "Q1", Sources: []string{"pkg/code.go", "pkg/code_test.go"}, Relationships: []EvidenceAssignment{{Value: "tests", Source: "pkg/code_test.go"}}, Facets: []EvidenceAssignment{{Value: "implementation", Source: "pkg/code.go"}, {Value: "proof", Source: "pkg/code_test.go"}}}
	report, err := Evaluate(corpus, testRun(t, corpus, valid))
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.RequiredRelationshipCoverage)
	require.Equal(t, 1.0, report.Overall.RequiredFacetCoverage)

	invalid := valid
	invalid.Relationships = []EvidenceAssignment{{Value: "tests", Source: "pkg/code.go"}}
	invalid.Facets = []EvidenceAssignment{{Value: "implementation", Source: "pkg/code.go"}, {Value: "proof", Source: "pkg/code.go"}}
	report, err = Evaluate(corpus, testRun(t, corpus, invalid))
	require.NoError(t, err)
	require.Zero(t, report.Overall.RequiredRelationshipCoverage)
	require.Equal(t, 0.5, report.Overall.RequiredFacetCoverage)
}

func TestEvaluateRejectsFingerprintMismatch(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1"})
	_, err := Evaluate(corpus, Run{CorpusFingerprint: "wrong"})
	require.ErrorContains(t, err, "fingerprint mismatch")
}

func TestEvaluateJudgmentRescoreRequiresOriginalFingerprintAndUnchangedExecutionContract(t *testing.T) {
	original := testCorpus(Case{ID: "Q1", Split: "development", Corpus: "repo", Family: "conceptual", Queries: []string{"how does it work"}, Judgments: map[string]Judgment{"a": {Grade: 3}}, JudgmentMethod: "initial", JudgedAt: "2026-09-12"})
	run := testRun(t, original, QueryResult{ID: "Q1", Sources: []string{"a", "b"}, MustRead: []string{"a"}})
	updated := original
	updated.Cases = append([]Case(nil), original.Cases...)
	updated.Cases[0].Judgments = map[string]Judgment{"a": {Grade: 3}, "b": {Grade: 0}}
	updated.Cases[0].JudgmentMethod = "independent_source_review"
	updated.Cases[0].JudgedAt = "2026-09-13"

	report, err := EvaluateJudgmentRescore(original, updated, run, Selection{Split: "development"})
	require.NoError(t, err)
	originalFingerprint, err := Fingerprint(original)
	require.NoError(t, err)
	updatedFingerprint, err := Fingerprint(updated)
	require.NoError(t, err)
	require.Equal(t, originalFingerprint, report.RescoredFromCorpusFingerprint)
	require.Equal(t, updatedFingerprint, report.CorpusFingerprint)
	require.Zero(t, report.Overall.UnjudgedTop10)

	changedQuery := updated
	changedQuery.Cases = append([]Case(nil), updated.Cases...)
	changedQuery.Cases[0].Queries = []string{"different query"}
	_, err = EvaluateJudgmentRescore(original, changedQuery, run, Selection{Split: "development"})
	require.ErrorContains(t, err, "execution contract")

	wrongRun := run
	wrongRun.CorpusFingerprint = "wrong"
	_, err = EvaluateJudgmentRescore(original, updated, wrongRun, Selection{Split: "development"})
	require.ErrorContains(t, err, "original corpus fingerprint mismatch")
}

func TestEvaluateJudgmentRescoreProjectsUniqueCanonicalCandidateIdentity(t *testing.T) {
	const (
		alias    = "pkg/worker.go"
		identity = "fqn\x00example.Worker.Run\x00pkg/worker.go"
	)
	original := testCorpus(Case{ID: "Q1", Split: "development", Corpus: "repo", Family: "structural_precision", Queries: []string{"definition of Worker.Run"}, ExpectedTarget: "example.Worker.Run", Relationships: []string{"definition"}, Judgments: map[string]Judgment{}, JudgmentMethod: "initial", JudgedAt: "2026-09-12"})
	run := testRun(t, original, QueryResult{
		ID:               "Q1",
		Sources:          []string{alias},
		Status:           "inferred_symbol",
		TargetIdentities: []string{"example.Worker.Run"},
		MustRead:         []string{alias},
		Roles:            []RoleAssignment{{Role: "implementation", Source: alias}},
		Relationships:    []EvidenceAssignment{{Value: "definition", Source: alias}},
		Candidates:       []CandidateTrace{{Source: alias, Identity: identity, Eligibility: "primary", Relationship: "definition", TargetIdentity: "example.Worker.Run"}},
	})
	updated := original
	updated.Cases = append([]Case(nil), original.Cases...)
	updated.Cases[0].Judgments = map[string]Judgment{identity: {Grade: 3, SupportedRoles: []string{"implementation"}, SupportedRelationships: []string{"definition"}}}
	updated.Cases[0].JudgmentMethod = "independent_source_review"
	updated.Cases[0].JudgedAt = "2026-09-13"

	report, err := EvaluateJudgmentRescore(original, updated, run, Selection{Split: "development"})
	require.NoError(t, err)
	require.Equal(t, &IdentityProjection{Method: "unique-candidate-identity-v1", AliasesReplaced: 1}, report.JudgmentIdentityProjection)
	require.Zero(t, report.Overall.UnjudgedTop10)
	require.Equal(t, 1.0, report.Overall.PrimaryResultPurity)
	require.Equal(t, 1.0, report.Overall.MustReadPrecision)
}

func TestEvaluateJudgmentRescoreRejectsAmbiguousCandidateAlias(t *testing.T) {
	const alias = "pkg/worker.go"
	original := testCorpus(Case{ID: "Q1", Split: "development", Corpus: "repo", Family: "structural_precision", Queries: []string{"definition"}, Judgments: map[string]Judgment{}, JudgmentMethod: "initial", JudgedAt: "2026-09-12"})
	run := testRun(t, original, QueryResult{ID: "Q1", Sources: []string{alias}, Candidates: []CandidateTrace{
		{Source: alias, Identity: alias},
		{Source: alias, Identity: "fqn\x00example.Worker.Run\x00pkg/worker.go"},
	}})
	updated := original
	updated.Cases = append([]Case(nil), original.Cases...)
	updated.Cases[0].Judgments = map[string]Judgment{"fqn\x00example.Worker.Run\x00pkg/worker.go": {Grade: 3}}
	updated.Cases[0].JudgmentMethod = "independent_source_review"
	updated.Cases[0].JudgedAt = "2026-09-13"

	_, err := EvaluateJudgmentRescore(original, updated, run, Selection{Split: "development"})
	require.ErrorContains(t, err, "ambiguous judgment identity projection")
}

func TestEvaluateJudgmentRescoreRejectsUnknownIdentitySharingJudgedAlias(t *testing.T) {
	const alias = "pkg/worker.go"
	original := testCorpus(Case{ID: "Q1", Split: "development", Corpus: "repo", Family: "structural_precision", Queries: []string{"definition"}, Judgments: map[string]Judgment{}, JudgmentMethod: "initial", JudgedAt: "2026-09-12"})
	run := testRun(t, original, QueryResult{ID: "Q1", Sources: []string{alias}, Candidates: []CandidateTrace{
		{Source: alias},
		{Source: alias, Identity: "fqn\x00example.Worker.Run\x00pkg/worker.go"},
	}})
	updated := original
	updated.Cases = append([]Case(nil), original.Cases...)
	updated.Cases[0].Judgments = map[string]Judgment{"fqn\x00example.Worker.Run\x00pkg/worker.go": {Grade: 3}}
	updated.Cases[0].JudgmentMethod = "independent_source_review"
	updated.Cases[0].JudgedAt = "2026-09-13"

	_, err := EvaluateJudgmentRescore(original, updated, run, Selection{Split: "development"})
	require.ErrorContains(t, err, "unknown=true")
}

func TestEvaluateJudgmentRescoreKeepsExplicitFileJudgment(t *testing.T) {
	const alias = "pkg/worker.go"
	original := testCorpus(Case{ID: "Q1", Split: "development", Corpus: "repo", Family: "conceptual", Queries: []string{"worker behavior"}, Judgments: map[string]Judgment{alias: {Grade: 2}}, JudgmentMethod: "initial", JudgedAt: "2026-09-12"})
	run := testRun(t, original, QueryResult{ID: "Q1", Sources: []string{alias}, MustRead: []string{alias}, Candidates: []CandidateTrace{{Source: alias, Identity: "fqn\x00example.Worker.Run\x00pkg/worker.go"}}})
	updated := original
	updated.Cases = append([]Case(nil), original.Cases...)
	updated.Cases[0].Judgments = map[string]Judgment{alias: {Grade: 3}}
	updated.Cases[0].JudgmentMethod = "independent_source_review"
	updated.Cases[0].JudgedAt = "2026-09-13"

	report, err := EvaluateJudgmentRescore(original, updated, run, Selection{Split: "development"})
	require.NoError(t, err)
	require.Nil(t, report.JudgmentIdentityProjection)
	require.Equal(t, 1.0, report.Overall.MustReadPrecision)
}

func TestEvaluateRejectsUndersizedExcellenceCorpus(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", JudgmentMethod: "agent_source_inspection", JudgedAt: "2026-09-12"})
	corpus.Protocol = "search-engine-excellence-v1"
	_, err := Evaluate(corpus, Run{CorpusFingerprint: "irrelevant"})
	require.ErrorContains(t, err, "need at least 320")
}

func TestEvaluateSelectsSplitAndReportsEligibleDenominators(t *testing.T) {
	corpus := testCorpus(Case{ID: "dev", Split: "dev", Corpus: "repo", Family: "conceptual", Judgments: map[string]Judgment{"a": {Grade: 3}}}, Case{ID: "held", Split: "held-out", Corpus: "fixture", Family: "navigation", RequiredSources: []string{"b"}, Judgments: map[string]Judgment{"b": {Grade: 3}}})
	run := testRun(t, corpus, QueryResult{ID: "dev", Sources: []string{"a"}, MustRead: []string{"a"}}, QueryResult{ID: "held", Sources: []string{"b"}, MustRead: []string{"b"}})
	report, err := EvaluateSplit(corpus, run, "held-out")
	require.NoError(t, err)
	require.Equal(t, 1, report.Overall.Cases)
	require.Equal(t, 1, report.Overall.NavigationCases)
	require.Equal(t, 1, report.Overall.NDCG10Cases)
	require.Equal(t, 1, report.Overall.RequiredRecall20Cases)
}

func TestEvaluateSelectsCorpusWithoutDroppingMissingSelectedResults(t *testing.T) {
	corpus := testCorpus(
		Case{ID: "repo-a", Split: "dev", Corpus: "repo", Family: "navigation", RequiredSources: []string{"a"}, Judgments: map[string]Judgment{"a": {Grade: 3}}},
		Case{ID: "repo-b", Split: "dev", Corpus: "repo", Family: "navigation", RequiredSources: []string{"b"}, Judgments: map[string]Judgment{"b": {Grade: 3}}},
		Case{ID: "fixture", Split: "dev", Corpus: "fixture", Family: "navigation", RequiredSources: []string{"c"}, Judgments: map[string]Judgment{"c": {Grade: 3}}},
	)
	run := testRun(t, corpus, QueryResult{ID: "repo-a", Sources: []string{"a"}})
	report, err := EvaluateSelection(corpus, run, Selection{Split: "dev", Corpora: []string{"repo"}})
	require.NoError(t, err)
	require.Equal(t, 2, report.Overall.Cases)
	require.Equal(t, 0.5, report.Overall.NavigationSuccess1)
	require.Contains(t, report.Failures, Failure{ID: "repo-b", Stage: "runner", Reason: "missing result"})
}

func TestEvaluateReportsUnjudgedTopResult(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Corpus: "repo", Family: "conceptual", Judgments: map[string]Judgment{"a": {Grade: 3}}})
	report, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"unknown"}}))
	require.NoError(t, err)
	require.Equal(t, 1, report.Overall.UnjudgedTop10)
	require.Contains(t, report.Failures, Failure{ID: "Q1", Stage: "judgment", Reason: "1 unjudged sources in top 10"})
}

func TestEvaluateReportsSourceAnswerRoleRelationshipAndFacetFailures(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Family: "multi_facet", RequiredSources: []string{"code.go", "code_test.go"}, RequiredRoles: []string{"implementation", "test"}, Relationships: []string{"tests"}, Facets: []QueryFacet{{Text: "implementation"}, {Text: "proof"}}, Judgments: map[string]Judgment{
		"code.go":      {Grade: 3, SupportedRoles: []string{"implementation"}, SupportedFacets: []string{"implementation"}},
		"code_test.go": {Grade: 3, SupportedRoles: []string{"test"}, SupportedRelationships: []string{"tests"}, SupportedFacets: []string{"proof"}},
		"noise.md":     {Grade: 0},
	}})
	report, err := Evaluate(corpus, testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"code.go", "noise.md"}, MustRead: []string{"noise.md"}, Roles: []RoleAssignment{{Role: "implementation", Source: "code.go"}}, Facets: []EvidenceAssignment{{Value: "implementation", Source: "code.go"}}}))
	require.NoError(t, err)
	stages := map[string]bool{}
	for _, failure := range report.Failures {
		stages[failure.Stage] = true
	}
	for _, stage := range []string{"source_selection", "answer_selection", "role_coverage", "relationship_coverage", "facet_coverage"} {
		require.True(t, stages[stage], "%s missing from %+v", stage, report.Failures)
	}
}

func TestEvaluatePrimaryResultPurityRejectsUnrelatedPrimaryDespiteRelationshipCoverage(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Family: "structural_precision", ExpectedTarget: "pkg.Target", Relationships: []string{"caller"}, Judgments: map[string]Judgment{
		"valid.go": {Grade: 3, SupportedRelationships: []string{"caller"}},
		"noise.go": {Grade: 0},
	}})
	run := testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"valid.go", "noise.go"}, Status: "inferred_symbol", TargetIdentities: []string{"pkg.Target"}, Relationships: []EvidenceAssignment{{Value: "caller", Source: "valid.go"}}, Candidates: []CandidateTrace{
		{Source: "valid.go", Identity: "valid.go", Eligibility: "primary", Relationship: "caller", Page: 1},
		{Source: "noise.go", Identity: "noise.go", Eligibility: "primary", Relationship: "caller", Page: 1},
	}})
	report, err := Evaluate(corpus, run)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.RequiredRelationshipCoverage)
	require.Equal(t, 0.5, report.Overall.PrimaryResultPurity)
	require.Equal(t, 1, report.Overall.PrimaryResultPurityCases)
	require.Contains(t, report.Failures, Failure{ID: "Q1", Stage: "primary_result_purity", Reason: "correct target and relationship among primary results 0.500"})
}

func TestEvaluatePrimaryResultPurityExcludesSupportingEvidence(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Family: "structural_precision", ExpectedTarget: "pkg.Target", Relationships: []string{"caller"}, Judgments: map[string]Judgment{
		"valid.go":   {Grade: 3, SupportedRelationships: []string{"caller"}},
		"context.md": {Grade: 0},
	}})
	run := testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"valid.go", "context.md"}, Status: "inferred_symbol", TargetIdentities: []string{"pkg.Target"}, Candidates: []CandidateTrace{
		{Source: "valid.go", Identity: "valid.go", Eligibility: "primary", Relationship: "caller", Page: 1},
		{Source: "context.md", Identity: "context.md", Eligibility: "supporting", Page: 1},
	}})
	report, err := Evaluate(corpus, run)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.PrimaryResultPurity)
}

func TestEvaluatePrimaryResultPurityRequiresUniqueActualTarget(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Family: "structural_precision", ExpectedTarget: "pkg.Target", Relationships: []string{"caller"}, Judgments: map[string]Judgment{
		"valid.go": {Grade: 3, SupportedRelationships: []string{"caller"}},
	}})
	candidate := CandidateTrace{Source: "valid.go", Identity: "valid.go", Eligibility: "primary", Relationship: "caller", Page: 1}
	for _, result := range []QueryResult{
		{ID: "Q1", Sources: []string{"valid.go"}, Status: "ambiguous", TargetIdentities: []string{"pkg.Target"}, Candidates: []CandidateTrace{candidate}},
		{ID: "Q1", Sources: []string{"valid.go"}, Status: "inferred_symbol", TargetIdentities: []string{"pkg.Other"}, Candidates: []CandidateTrace{candidate}},
		{ID: "Q1", Sources: []string{"valid.go"}, Status: "inferred_symbol", TargetIdentities: []string{"pkg.target"}, Candidates: []CandidateTrace{candidate}},
	} {
		report, err := Evaluate(corpus, testRun(t, corpus, result))
		require.NoError(t, err)
		require.Zero(t, report.Overall.PrimaryResultPurity)
	}
}

func TestEvaluatePrimaryResultPurityDoesNotInheritFileRelationshipForWrongSymbol(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Family: "structural_precision", ExpectedTarget: "pkg.Target", Relationships: []string{"caller"}, Judgments: map[string]Judgment{
		"same.go": {Grade: 3, SupportedRelationships: []string{"caller"}},
	}})
	run := testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"same.go"}, Status: "inferred_symbol", TargetIdentities: []string{"pkg.Target"}, Relationships: []EvidenceAssignment{{Value: "caller", Source: "same.go"}}, Candidates: []CandidateTrace{
		{Source: "same.go", Identity: "fqn\x00pkg.Wrong\x00same.go", Eligibility: "primary", Relationship: "caller", Page: 1},
	}})
	report, err := Evaluate(corpus, run)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.RequiredRelationshipCoverage)
	require.Zero(t, report.Overall.PrimaryResultPurity)
}

func TestEvaluateRetainsContinuationFailureAndRunCount(t *testing.T) {
	corpus := testCorpus(Case{ID: "Q1", Family: "navigation", RequiredSources: []string{"first", "second"}, Judgments: map[string]Judgment{"first": {Grade: 2}, "second": {Grade: 3}}})
	run := testRun(t, corpus, QueryResult{ID: "Q1", Sources: []string{"first"}, ContinuationError: "stale continuation"})
	run.FailureCount = 1
	report, err := Evaluate(corpus, run)
	require.NoError(t, err)
	require.Equal(t, 1, report.RunFailureCount)
	require.Equal(t, 0.5, report.Overall.RequiredRecall20)
	require.Contains(t, report.Failures, Failure{ID: "Q1", Stage: "continuation", Reason: "stale continuation"})
}
