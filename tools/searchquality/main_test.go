package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/qualityeval"
	"github.com/stretchr/testify/require"
)

func TestDevelopmentCorpusFamiliesUseDistinctPhysicalCollections(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(repoRoot, "pkg/search/qualityeval/testdata/corpus-v2.json"))
	require.NoError(t, err)
	var corpus qualityeval.Corpus
	require.NoError(t, json.Unmarshal(b, &corpus))

	roots := map[string]string{}
	for _, c := range corpus.Cases {
		if c.Split != "development" {
			continue
		}
		root, _, err := corpusVault(repoRoot, c.Corpus, nil)
		require.NoError(t, err)
		roots[c.Corpus] = filepath.Clean(root)
		for source := range c.Judgments {
			physicalSource := judgmentPhysicalSource(source)
			_, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(physicalSource)))
			require.NoError(t, err, "%s: %s", c.ID, source)
		}
	}
	require.Len(t, roots, 5)
	unique := map[string]struct{}{}
	for _, root := range roots {
		unique[root] = struct{}{}
	}
	require.Len(t, unique, 5)
}

func TestCorpusVaultSupportsExplicitHeldOutRootsAndExactSourcePrefixes(t *testing.T) {
	base := t.TempDir()
	root, prefix, err := corpusVault(base, "blind-typed", map[string]corpusRoot{
		"blind-typed": {Root: "heldout/typed", SourcePrefix: "testdata/search-quality/heldout/typed"},
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(base, "heldout", "typed"), root)
	require.Equal(t, "testdata/search-quality/heldout/typed", prefix)
	require.Equal(t, "testdata/search-quality/heldout/typed/notes/example.md", prefixedSource(prefix, "notes/example.md"))
}

func TestCorpusVaultRejectsUnknownCorpusWithoutMapping(t *testing.T) {
	_, _, err := corpusVault(t.TempDir(), "blind-unknown", nil)
	require.ErrorContains(t, err, "no physical root mapping")
}

func TestEvaluationProfileRejectsUnknownValue(t *testing.T) {
	profile, err := evaluationProfile("interactive")
	require.NoError(t, err)
	require.Equal(t, unifiedsearch.ProfileInteractive, profile)
	_, err = evaluationProfile("batch")
	require.ErrorContains(t, err, "unknown evaluation profile")
}

func TestEvaluationEmbeddingOverrideRequiresIsolatedLiveMode(t *testing.T) {
	for _, test := range []struct {
		name     string
		isolated bool
		fast     bool
	}{
		{name: "shared index", isolated: false},
		{name: "deterministic", isolated: true, fast: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := evaluationEmbeddingOverride("voyage", "voyage-4-lite", "", 1024, test.isolated, test.fast)
			require.ErrorContains(t, err, "require -isolate and -fast=false")
		})
	}

	cfg, err := evaluationEmbeddingOverride("Voyage", "voyage-4-lite", "", 1024, true, false)
	require.NoError(t, err)
	require.Equal(t, "voyage", cfg.Provider)
	require.Equal(t, "voyage-4-lite", cfg.Model)
	require.Equal(t, 1024, cfg.Dimensions)
}

func TestEvaluationRolePreservesJudgedTestEvidence(t *testing.T) {
	corpus := qualityeval.Corpus{Version: "test", SourceRevision: "fixture", Cases: []qualityeval.Case{{
		ID: "Q1", Corpus: "repo", Split: "development", Family: "multi_facet", Queries: []string{"implementation and proof"},
		RequiredRoles:  []string{"test"},
		JudgmentMethod: "source-inspected", JudgedAt: "2026-09-13",
		Judgments: map[string]qualityeval.Judgment{
			"pkg/service_test.go": {Grade: 3, SupportedRoles: []string{"test"}},
		},
	}}}
	fingerprint, err := qualityeval.Fingerprint(corpus)
	require.NoError(t, err)
	run := qualityeval.Run{Revision: "test", CorpusFingerprint: fingerprint, Results: []qualityeval.QueryResult{{
		ID: "Q1", Sources: []string{"pkg/service_test.go"},
		Roles: []qualityeval.RoleAssignment{{Role: evaluationRole("test", "pkg/service_test.go"), Source: "pkg/service_test.go"}},
	}}}
	report, err := qualityeval.Evaluate(corpus, run)
	require.NoError(t, err)
	require.Equal(t, 1.0, report.Overall.RequiredRoleCoverage)

	require.Equal(t, "documentation", evaluationRole("doc", "docs/guide.md"))
	require.Equal(t, "implementation", evaluationRole("entry_point", "pkg/service.go"))
	require.Equal(t, "implementation", evaluationRole("impl", "pkg/service.go"))
}

var sha256Fingerprint = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// requireCurrentTransformProvenance checks provenance against independently
// hashed inputs: the source run bytes and the running test executable.
func requireCurrentTransformProvenance(t *testing.T, provenance *qualityeval.AnswerTransformProvenance, sourceRun []byte, sourceExecution string) {
	t.Helper()
	require.NotNil(t, provenance)
	sourceDigest := sha256.Sum256(sourceRun)
	require.Equal(t, hex.EncodeToString(sourceDigest[:]), provenance.SourceRunSHA256)
	require.Equal(t, sourceExecution, provenance.SourceExecutionFingerprint)
	require.Regexp(t, sha256Fingerprint, provenance.TransformerExecutionFingerprint)
	executable, err := os.Executable()
	require.NoError(t, err)
	binary, err := os.ReadFile(executable)
	require.NoError(t, err)
	binaryDigest := sha256.Sum256(binary)
	require.Equal(t, hex.EncodeToString(binaryDigest[:]), provenance.TransformerBinarySHA256)
}

func TestReplayCapturedAnswersUsesSnapshotAndPreservesRetrievalEvidence(t *testing.T) {
	corpus := qualityeval.Corpus{Version: "test", SourceRevision: "fixture", Cases: []qualityeval.Case{{
		ID: "Q1", Corpus: "repo", Split: "development", Family: "conceptual_discovery", Intent: string(search.IntentSearch), Queries: []string{"policy"}, JudgmentMethod: "source-inspected", JudgedAt: "2026-09-13",
		Judgments: map[string]qualityeval.Judgment{"policy.md": {Grade: 3, SupportedRoles: []string{"documentation"}}},
	}}}
	fingerprint, err := qualityeval.Fingerprint(corpus)
	require.NoError(t, err)
	snapshot, err := json.Marshal(answerInputSnapshot{
		Version: answerInputSnapshotVersion, Intent: search.IntentSearch, Query: "policy", Queries: []unifiedsearch.QueryInput{{Text: "policy", Mode: string(search.IntentSearch)}}, Availability: unifiedsearch.AvailabilityComplete,
		Sources: []unifiedsearch.SourceAssessment{{Result: search.RankedResult{Candidate: search.Candidate{Path: "policy.md", Title: "Policy", Type: "note"}, FinalScore: .9}, Eligibility: unifiedsearch.EvidenceSupporting, Relevance: unifiedsearch.RelevanceStrong, SupportedRoles: []string{"documentation"}}},
	})
	require.NoError(t, err)
	run := qualityeval.Run{Revision: "rev", ExecutionFingerprint: "original-source", CorpusFingerprint: fingerprint, Results: []qualityeval.QueryResult{{
		ID: "Q1", Sources: []string{"policy.md"}, MustRead: []string{"stale.md"}, DurationMS: 123, Candidates: []qualityeval.CandidateTrace{{Source: "policy.md", Identity: "path\x00policy.md", Page: 1, Eligibility: "supporting", Score: .9}}, AnswerInputSnapshot: snapshot,
	}}}
	sourceRun, err := json.Marshal(run)
	require.NoError(t, err)

	replayed, err := replayCapturedAnswers(corpus, run, sourceRun)
	require.NoError(t, err)
	require.Equal(t, []string{"policy.md"}, replayed.Results[0].MustRead)
	require.Equal(t, 123.0, replayed.Results[0].DurationMS)
	require.Equal(t, run.Results[0].Candidates, replayed.Results[0].Candidates)
	require.Equal(t, "captured-assessment-answer-replay-v1", replayed.AnswerTransform.Method)
	requireCurrentTransformProvenance(t, replayed.AnswerTransform, sourceRun, "original-source")

	missing := run
	missing.Results[0].AnswerInputSnapshot = nil
	_, err = replayCapturedAnswers(corpus, missing, sourceRun)
	require.ErrorContains(t, err, "lacks searchquality-answer-input-v1 snapshot")

	wrongVersion := run
	wrongVersion.Results = append([]qualityeval.QueryResult(nil), run.Results...)
	wrongVersion.Results[0].AnswerInputSnapshot = json.RawMessage(`{"version":"future"}`)
	_, err = replayCapturedAnswers(corpus, wrongVersion, sourceRun)
	require.ErrorContains(t, err, "snapshot version")
}

func TestReplayCapturedAnswersMatchesCurrentMultifacetAndEvidenceLimitPath(t *testing.T) {
	queries := []unifiedsearch.QueryInput{{Text: "owner", Mode: string(search.IntentSearch)}, {Text: "outage", Mode: string(search.IntentSearch)}}
	corpus := qualityeval.Corpus{Version: "test", SourceRevision: "fixture", Cases: []qualityeval.Case{{
		ID: "Q1", Corpus: "repo", Split: "development", Family: "multi_facet", Intent: string(search.IntentSearch), Queries: []string{"owner", "outage"},
		Facets: []qualityeval.QueryFacet{{Text: "owner", Mode: string(search.IntentSearch)}, {Text: "outage", Mode: string(search.IntentSearch)}}, JudgmentMethod: "source-inspected", JudgedAt: "2026-09-13",
		Judgments: map[string]qualityeval.Judgment{"owner.md": {Grade: 3}, "outage.md": {Grade: 3}},
	}}}
	// Each source directly identifies one facet and documents it strongly.
	sources := []unifiedsearch.SourceAssessment{
		{Result: search.RankedResult{Candidate: search.Candidate{Path: "owner.md", Title: "owner", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: .9}, Eligibility: unifiedsearch.EvidenceSupporting, Relevance: unifiedsearch.RelevanceStrong, SupportedRoles: []string{"documentation"}, FacetSupport: []unifiedsearch.FacetSupport{{Text: "owner", Mode: search.IntentSearch, Relevance: unifiedsearch.RelevanceStrong, Eligibility: unifiedsearch.EvidenceSupporting}}},
		{Result: search.RankedResult{Candidate: search.Candidate{Path: "outage.md", Title: "outage", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: .8}, Eligibility: unifiedsearch.EvidenceSupporting, Relevance: unifiedsearch.RelevanceStrong, SupportedRoles: []string{"documentation"}, FacetSupport: []unifiedsearch.FacetSupport{{Text: "outage", Mode: search.IntentSearch, Relevance: unifiedsearch.RelevanceStrong, Eligibility: unifiedsearch.EvidenceSupporting}}},
	}
	fingerprint, err := qualityeval.Fingerprint(corpus)
	require.NoError(t, err)

	for _, test := range []struct {
		name           string
		remaining      []string
		wantConfidence string
	}{
		{name: "no evidence limit", wantConfidence: "high"},
		{name: "owner facet has more results", remaining: []string{"facet:owner:more_results"}, wantConfidence: "medium"},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshotJSON, err := json.Marshal(answerInputSnapshot{Version: answerInputSnapshotVersion, Intent: search.IntentSearch, Query: unifiedsearch.JoinQueryInputs(queries), Queries: queries, Availability: unifiedsearch.AvailabilityComplete, Sources: sources, RemainingEvidenceLimit: test.remaining})
			require.NoError(t, err)
			run := qualityeval.Run{CorpusFingerprint: fingerprint, Results: []qualityeval.QueryResult{{ID: "Q1", Sources: []string{"owner.md", "outage.md"}, Candidates: []qualityeval.CandidateTrace{
				{Source: "owner.md", Identity: "path\x00owner.md", Page: 1, Eligibility: "supporting", Score: .9, Evidence: []string{"note_title_exact"}},
				{Source: "outage.md", Identity: "path\x00outage.md", Page: 1, Eligibility: "supporting", Score: .8, Evidence: []string{"note_title_exact"}},
			}, AnswerInputSnapshot: snapshotJSON}}}
			original := run.Results[0]
			sourceRun, err := json.Marshal(run)
			require.NoError(t, err)

			replayed, err := replayCapturedAnswers(corpus, run, sourceRun)
			require.NoError(t, err)
			got := replayed.Results[0]
			require.Equal(t, []string{"owner.md", "outage.md"}, got.MustRead)
			require.ElementsMatch(t, []qualityeval.RoleAssignment{{Role: "documentation", Source: "owner.md"}, {Role: "documentation", Source: "outage.md"}}, got.Roles)
			require.ElementsMatch(t, []qualityeval.EvidenceAssignment{{Value: "owner", Source: "owner.md"}, {Value: "outage", Source: "outage.md"}}, got.Facets)
			require.Empty(t, got.Relationships)
			require.Equal(t, test.wantConfidence, got.Confidence)
			require.Equal(t, original.Sources, got.Sources)
			require.Equal(t, original.Candidates, got.Candidates)
		})
	}
}

func TestReassessCapturedAnswersRepairsFacetObservationWithoutChangingRetrieval(t *testing.T) {
	queries := []unifiedsearch.QueryInput{{Text: "cold threshold", Mode: string(search.IntentSearch)}, {Text: "missed delivery", Mode: string(search.IntentSearch)}}
	caseDef := qualityeval.Case{ID: "Q1", Corpus: "repo", Split: "development", Family: "multi_facet", Intent: string(search.IntentSearch), Queries: []string{"cold threshold", "missed delivery"}, Facets: []qualityeval.QueryFacet{{Text: "cold threshold", Mode: string(search.IntentSearch)}, {Text: "missed delivery", Mode: string(search.IntentSearch)}}, JudgmentMethod: "source-inspected", JudgedAt: "2026-09-13"}
	corpus := qualityeval.Corpus{Version: "test", SourceRevision: "fixture", Cases: []qualityeval.Case{caseDef}}
	fingerprint, err := qualityeval.Fingerprint(corpus)
	require.NoError(t, err)
	result := search.RankedResult{Candidate: search.Candidate{Path: "delivery.md", Type: "note", Evidence: []search.Evidence{
		{Type: "query_match", Source: "multi_query", Details: map[string]string{"query": "missed delivery", "mode": "search", "relevance": "strong", "eligibility": "supporting"}},
		{Type: "query_match", Source: "multi_query", Details: map[string]string{"query": "missed delivery", "mode": "search", "relevance": "useful", "eligibility": "supporting"}},
	}}, FinalScore: .8}
	stale := unifiedsearch.SourceAssessment{Result: result, Relevance: unifiedsearch.RelevanceStrong, SupportedRoles: []string{"documentation"}, FacetSupport: []unifiedsearch.FacetSupport{{Text: "missed delivery", Mode: search.IntentSearch, Relevance: unifiedsearch.RelevanceUseful}}}
	snapshot, err := json.Marshal(answerInputSnapshot{Version: answerInputSnapshotVersion, Intent: search.IntentSearch, Query: unifiedsearch.JoinQueryInputs(queries), Queries: queries, Availability: unifiedsearch.AvailabilityComplete, Sources: []unifiedsearch.SourceAssessment{stale}})
	require.NoError(t, err)
	run := qualityeval.Run{ExecutionFingerprint: "original", CorpusFingerprint: fingerprint, AnswerSelector: "current", Results: []qualityeval.QueryResult{{ID: "Q1", Sources: []string{"delivery.md"}, Candidates: []qualityeval.CandidateTrace{{Source: "delivery.md", Identity: "path\x00delivery.md", Page: 1, Score: .8, Evidence: []string{"query_match", "query_match"}}}, AnswerInputSnapshot: snapshot}}}
	sourceRun, err := json.Marshal(run)
	require.NoError(t, err)

	reassessed, err := reassessCapturedAnswers(corpus, run, sourceRun)
	require.NoError(t, err)
	require.Equal(t, run.Results[0].Sources, reassessed.Results[0].Sources)
	require.Equal(t, run.Results[0].Candidates, reassessed.Results[0].Candidates)
	require.Equal(t, "captured-ranked-evidence-query-reassessment-v1", reassessed.AnswerTransform.Method)
	requireCurrentTransformProvenance(t, reassessed.AnswerTransform, sourceRun, "original")
	var updated answerInputSnapshot
	require.NoError(t, json.Unmarshal(reassessed.Results[0].AnswerInputSnapshot, &updated))
	require.Equal(t, unifiedsearch.RelevanceStrong, updated.Sources[0].FacetSupport[0].Relevance)

	for _, test := range []struct {
		name   string
		mutate func(*qualityeval.QueryResult)
		want   string
	}{
		{name: "ranked identity", mutate: func(result *qualityeval.QueryResult) { result.Candidates[0].Identity = "path\x00other.md" }, want: "first-page candidate trace"},
		{name: "ranked handle", mutate: func(result *qualityeval.QueryResult) { result.Candidates[0].Handle = "file:other.md" }, want: "first-page candidate trace"},
		{name: "ranked score", mutate: func(result *qualityeval.QueryResult) { result.Candidates[0].Score = .7 }, want: "first-page candidate trace"},
		{name: "evidence order", mutate: func(result *qualityeval.QueryResult) {
			result.Candidates[0].Evidence = []string{"other", "query_match"}
		}, want: "first-page candidate trace"},
		{name: "deduplicated sources", mutate: func(result *qualityeval.QueryResult) { result.Sources = []string{"other.md"} }, want: "deduplicated candidate page trace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tampered := run
			tampered.Results = append([]qualityeval.QueryResult(nil), run.Results...)
			tampered.Results[0].Candidates = append([]qualityeval.CandidateTrace(nil), run.Results[0].Candidates...)
			tampered.Results[0].Sources = append([]string(nil), run.Results[0].Sources...)
			test.mutate(&tampered.Results[0])
			_, err := reassessCapturedAnswers(corpus, tampered, sourceRun)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestReplayCapturedAnswersRejectsMismatchedRequest(t *testing.T) {
	corpus := qualityeval.Corpus{Version: "test", SourceRevision: "fixture", Cases: []qualityeval.Case{{ID: "Q1", Corpus: "repo", Split: "development", Family: "conceptual", Intent: string(search.IntentSearch), Queries: []string{"expected"}, JudgmentMethod: "source-inspected", JudgedAt: "2026-09-13"}}}
	fingerprint, err := qualityeval.Fingerprint(corpus)
	require.NoError(t, err)
	snapshot, err := json.Marshal(answerInputSnapshot{Version: answerInputSnapshotVersion, Intent: search.IntentSearch, Query: "different", Queries: []unifiedsearch.QueryInput{{Text: "different", Mode: string(search.IntentSearch)}}})
	require.NoError(t, err)
	run := qualityeval.Run{CorpusFingerprint: fingerprint, Results: []qualityeval.QueryResult{{ID: "Q1", AnswerInputSnapshot: snapshot}}}
	_, err = replayCapturedAnswers(corpus, run, []byte("source"))
	require.ErrorContains(t, err, "captured request does not match corpus execution contract")
	run.CorpusFingerprint = "wrong"
	_, err = replayCapturedAnswers(corpus, run, []byte("source"))
	require.ErrorContains(t, err, "answer replay corpus fingerprint mismatch")
}

func TestSameResolvedPathDetectsSymlinkAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	require.NoError(t, os.WriteFile(input, []byte("{}"), 0o600))
	alias := filepath.Join(dir, "alias.json")
	require.NoError(t, os.Symlink(input, alias))
	same, err := sameResolvedPath(input, alias)
	require.NoError(t, err)
	require.True(t, same)
}

func TestSameResolvedPathDetectsHardLinkAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	require.NoError(t, os.WriteFile(input, []byte("{}"), 0o600))
	alias := filepath.Join(dir, "alias.json")
	require.NoError(t, os.Link(input, alias))
	same, err := sameResolvedPath(input, alias)
	require.NoError(t, err)
	require.True(t, same)
}

func TestEvaluationRankedSourceRetainsCanonicalSymbolIdentityWithFileFallback(t *testing.T) {
	const wantIdentity = "fqn\x00example.Worker.Run\x00fixture/src/worker.go"
	ranked := search.RankedResult{Candidate: search.Candidate{Path: "src/worker.go", FQN: "example.Worker.Run"}}

	fileSource, fileIdentity := evaluationRankedSource(qualityeval.Case{Judgments: map[string]qualityeval.Judgment{
		"fixture/src/worker.go": {Grade: 2},
	}}, "fixture", ranked)
	require.Equal(t, "fixture/src/worker.go", fileSource)
	require.Equal(t, wantIdentity, fileIdentity)

	identitySource, identity := evaluationRankedSource(qualityeval.Case{Judgments: map[string]qualityeval.Judgment{
		wantIdentity: {Grade: 3},
	}}, "fixture", ranked)
	require.Equal(t, wantIdentity, identitySource)
	require.Equal(t, wantIdentity, identity)
}

func TestExecuteEvaluationPagesKeepsInteractiveFirstPageContractAndCollectsTenMore(t *testing.T) {
	makeSources := func(prefix string) []unifiedsearch.SourceAssessment {
		out := make([]unifiedsearch.SourceAssessment, 10)
		for i := range out {
			out[i].Result.Candidate.Path = prefix + string(rune('a'+i)) + ".md"
		}
		return out
	}
	first := unifiedsearch.ApplicationResult{Sources: makeSources("first-"), Continuation: "next"}
	first.Answer.Confidence.Level = "high"
	second := unifiedsearch.ApplicationResult{Sources: makeSources("second-")}
	pages := executeEvaluationPages(context.Background(), unifiedsearch.ProfileInteractive, unifiedsearch.ApplicationOptions{}, func(_ context.Context, opts unifiedsearch.ApplicationOptions) (unifiedsearch.ApplicationResult, error) {
		if opts.Continuation == "" {
			return first, nil
		}
		require.Equal(t, "next", opts.Continuation)
		return second, nil
	})
	require.Len(t, pages, 2)
	require.Len(t, pages[0].result.Sources, 10)
	require.Len(t, pages[1].result.Sources, 10)
	require.Equal(t, "high", pages[0].result.Answer.Confidence.Level)
}

func TestExecuteEvaluationPagesRetainsFirstPageWhenContinuationFails(t *testing.T) {
	first := unifiedsearch.ApplicationResult{Sources: make([]unifiedsearch.SourceAssessment, 10), Continuation: "next"}
	pages := executeEvaluationPages(context.Background(), unifiedsearch.ProfileInteractive, unifiedsearch.ApplicationOptions{}, func(_ context.Context, opts unifiedsearch.ApplicationOptions) (unifiedsearch.ApplicationResult, error) {
		if opts.Continuation == "" {
			return first, nil
		}
		return unifiedsearch.ApplicationResult{}, errors.New("stale continuation")
	})
	require.Len(t, pages, 2)
	require.Len(t, pages[0].result.Sources, 10)
	require.ErrorContains(t, pages[1].err, "stale continuation")
}
