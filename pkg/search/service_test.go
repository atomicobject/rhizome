package search

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type stubRetriever struct{ name string }

func (s stubRetriever) Name() string { return s.name }
func (s stubRetriever) Retrieve(context.Context, QuerySpec) ([]Candidate, error) {
	return nil, nil
}

type staticRetriever struct {
	name    string
	results []Candidate
}

func (s staticRetriever) Name() string { return s.name }
func (s staticRetriever) Retrieve(_ context.Context, _ QuerySpec) ([]Candidate, error) {
	return append([]Candidate(nil), s.results...), nil
}

type timedRetriever struct {
	name   string
	delay  time.Duration
	result []Candidate
}

func (t timedRetriever) Name() string { return t.name }
func (t timedRetriever) Retrieve(ctx context.Context, _ QuerySpec) ([]Candidate, error) {
	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return t.result, nil
}

type stubRanker struct{}

func (s stubRanker) Rank(_ context.Context, _ QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	out := make([]RankedResult, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, RankedResult{Candidate: c, FinalScore: 1})
	}
	return out, nil
}

// reverseTieRanker returns equal-score ties in a fixed reverse order so the
// service comparator, not the ranker or map iteration, owns the final order.
type reverseTieRanker struct{}

func (reverseTieRanker) Rank(_ context.Context, _ QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	byPath := map[string]Candidate{}
	for _, candidate := range candidates {
		byPath[candidate.Path] = candidate
	}
	return []RankedResult{
		{Candidate: byPath["Notes/B.md"], FinalScore: 1},
		{Candidate: byPath["Notes/A.md"], FinalScore: 1},
	}, nil
}

// topOneRanker keeps only the strongest raw-evidence candidate, so a filter
// applied after ranking would observe the wrong population.
type topOneRanker struct{}

func (topOneRanker) Rank(ctx context.Context, spec QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	ranked, _ := rawEvidenceRanker{}.Rank(ctx, spec, candidates)
	if len(ranked) == 0 {
		return nil, nil
	}
	best := ranked[0]
	for _, result := range ranked[1:] {
		if result.FinalScore > best.FinalScore {
			best = result
		}
	}
	return []RankedResult{best}, nil
}

type tieredRanker struct{}

func (tieredRanker) Rank(_ context.Context, _ QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	byPath := map[string]Candidate{}
	for _, candidate := range candidates {
		byPath[candidate.Path] = candidate
	}
	return []RankedResult{
		{Candidate: byPath["exact.md"], FinalScore: 1},
		{Candidate: byPath["broad.md"], FinalScore: 10},
	}, nil
}

func TestSearchPreservesRankerEligibilityOrder(t *testing.T) {
	retriever := staticRetriever{name: "lexical", results: []Candidate{
		{SourceRef: SourceRef{Path: "exact.md"}, Handle: knowledge.NoteHandle("exact.md"), Owner: knowledge.NoteHandle("exact.md"), Evidence: []Evidence{{Type: "note_title_exact", RawScore: 1}}},
		{SourceRef: SourceRef{Path: "broad.md"}, Handle: knowledge.NoteHandle("broad.md"), Owner: knowledge.NoteHandle("broad.md")},
	}}
	response, err := (&Service{Retrievers: []Retriever{retriever}, Ranker: tieredRanker{}}).Search(context.Background(), QuerySpec{Text: "exact"})
	require.NoError(t, err)
	require.Equal(t, []string{"exact.md", "broad.md"}, []string{response.Results[0].Path, response.Results[1].Path})
}

func TestSearchAppliesTestEligibilityBeforeRanking(t *testing.T) {
	eligibility := []Candidate{
		{SourceRef: SourceRef{Path: "pkg/search/service.go"}, Handle: knowledge.AnchorHandle("prod"), Type: "code"},
		{SourceRef: SourceRef{Path: "pkg/search/service_test.go"}, Handle: knowledge.AnchorHandle("test"), Type: "code"},
		{SourceRef: SourceRef{Path: "docs/search.md"}, Handle: knowledge.NoteHandle("docs/search.md"), Type: "note"},
	}
	// The strongest inputs fail the exact-symbol filter; a top-one ranker would
	// pick one of them if filtering were skipped or ran after ranking.
	symbols := []Candidate{
		{SourceRef: SourceRef{Path: "pkg/service.go", Symbol: "Run", FQN: "pkg.Service.Run"}, Handle: knowledge.AnchorHandle("run"), Type: "anchor", Evidence: []Evidence{{Type: "intel_fts_match", RawScore: 0.1}}},
		{SourceRef: SourceRef{Path: "pkg/runner.go", Symbol: "Runner", FQN: "pkg.Service.Runner"}, Handle: knowledge.AnchorHandle("runner"), Type: "anchor", Evidence: []Evidence{{Type: "intel_fts_match", RawScore: 0.9}}},
		{SourceRef: SourceRef{Path: "pkg/other.go", Symbol: "Other", FQN: "pkg.Other"}, Handle: knowledge.AnchorHandle("other"), Type: "anchor", Evidence: []Evidence{{Type: "intel_fts_match", RawScore: 0.8}}},
		{SourceRef: SourceRef{Path: "Notes/Run.md", Title: "Run"}, Handle: knowledge.NoteHandle("Notes/Run.md"), Type: "note", Evidence: []Evidence{{Type: "note_title_exact", RawScore: 1}}},
	}
	tests := []struct {
		name       string
		candidates []Candidate
		ranker     Ranker
		filters    Filters
		want       []string
	}{
		{name: "tests only", candidates: eligibility, ranker: stubRanker{}, filters: Filters{Types: []string{"code"}, TestsOnly: true}, want: []string{"pkg/search/service_test.go"}},
		{name: "exclude tests", candidates: eligibility, ranker: stubRanker{}, filters: Filters{ExcludeTests: true}, want: []string{"pkg/search/service.go", "docs/search.md"}},
		{name: "exact symbol", candidates: symbols, ranker: topOneRanker{}, filters: Filters{ExactSymbols: []string{"Run"}}, want: []string{"pkg/service.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retriever := staticRetriever{name: "all", results: tt.candidates}
			response, err := (&Service{Retrievers: []Retriever{retriever}, Ranker: tt.ranker}).Search(context.Background(), QuerySpec{Text: "search", Filters: tt.filters, Limits: Limits{Total: 10}})
			require.NoError(t, err)
			paths := make([]string, 0, len(response.Results))
			for _, result := range response.Results {
				paths = append(paths, result.Path)
			}
			require.ElementsMatch(t, tt.want, paths)
		})
	}
}

type rawEvidenceRanker struct{}

func (rawEvidenceRanker) Rank(_ context.Context, _ QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	out := make([]RankedResult, 0, len(candidates))
	for _, candidate := range candidates {
		score := 0.0
		for _, evidence := range candidate.Evidence {
			if evidence.RawScore > score {
				score = evidence.RawScore
			}
		}
		out = append(out, RankedResult{Candidate: candidate, FinalScore: score})
	}
	return out, nil
}

type failingRetriever struct {
	name string
	err  error
}

func (f failingRetriever) Name() string { return f.name }

func (f failingRetriever) Retrieve(_ context.Context, _ QuerySpec) ([]Candidate, error) {
	return nil, f.err
}

type deadlinePacker struct{}

// deadlinePacker outlives the request deadline. Waiting on Done keeps the
// fallback deterministic when a lane budget expires before the parent context,
// as it can with Windows timer granularity.
func (d deadlinePacker) Pack(ctx context.Context, spec QuerySpec, _ []RankedResult) (PackedContext, error) {
	<-ctx.Done()
	return PackedContext{}, ctx.Err()
}

func TestSearch_CodeFilterRemovesStrongNoteCandidatesBeforeRanking(t *testing.T) {
	svc := Service{
		Retrievers: []Retriever{timedRetriever{
			name: "mixed",
			result: []Candidate{
				{SourceRef: SourceRef{Path: "notes/strong.md"}, Handle: knowledge.NoteHandle("notes/strong.md"), Owner: knowledge.NoteHandle("notes/strong.md"), Type: "note", Evidence: []Evidence{{Type: "note_title_match", RawScore: 100}}},
				{SourceRef: SourceRef{Path: "pkg/search.go"}, Handle: knowledge.AnchorHandle("code-hit"), Owner: knowledge.AnchorHandle("code-hit"), Type: "anchor", Evidence: []Evidence{{Type: "intel_fts_match", RawScore: 1}}},
			},
		}},
		Ranker: rawEvidenceRanker{},
	}

	resp, err := svc.Search(context.Background(), QuerySpec{
		Text:    "shared search term",
		Filters: Filters{Types: []string{"code"}},
		Limits:  Limits{Total: 10},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "anchor", resp.Results[0].Type)
}

func TestSearch_Deadline_ReturnsPartialResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	timings := &Timings{}
	ctx = WithTimings(ctx, timings)

	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "intel_lexical", result: []Candidate{{SourceRef: SourceRef{Title: "fast", NoteID: "Notes/Fast.md", Path: "Notes/Fast.md"}, Handle: knowledge.NoteHandle("Notes/Fast.md"),
				Owner: knowledge.NoteHandle("Notes/Fast.md"),
				Type:  "note",
			}}},
			timedRetriever{name: "vector", delay: 250 * time.Millisecond, result: []Candidate{{SourceRef: SourceRef{Title: "slow", NoteID: "Notes/Slow.md", Path: "Notes/Slow.md"}, Handle: knowledge.NoteHandle("Notes/Slow.md"),
				Owner: knowledge.NoteHandle("Notes/Slow.md"),
				Type:  "note",
			}}},
		},
		Ranker: stubRanker{},
		Packer: deadlinePacker{},
	}
	spec := QuerySpec{Text: "q", Limits: Limits{Total: 5}, Budget: Budget{Chars: 2000}}
	resp, err := svc.Search(ctx, spec)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	require.Len(t, resp.Results, 1)
	require.Equal(t, "Notes/Fast.md", resp.Results[0].Path)
	require.Equal(t, LaneStateRan, statusForLane(resp.Lanes, "intel_fts").Status)
	require.Equal(t, LaneStateTimedOut, statusForLane(resp.Lanes, "code_vector").Status)
	// The expired packer routes through the service's minimal-pack fallback.
	require.NotNil(t, resp.Packed)
	require.Contains(t, resp.Packed.Text, "Notes/Fast.md")

	var vector *TimingEvent
	for _, ev := range timings.Snapshot() {
		if ev.Kind == "retriever" && ev.Name == "vector" {
			ev := ev
			vector = &ev
		}
	}
	require.NotNil(t, vector, "expected vector retriever timing")
	require.Greater(t, vector.Duration, time.Duration(0))
	require.Contains(t, []string{"timeout", "canceled"}, vector.Status)
}

func TestSearch_Deadline_BudgetsSlowRetrieverAndContinuesBroadSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "vector", delay: time.Second, result: []Candidate{{SourceRef: SourceRef{Title: "slow", NoteID: "Notes/Slow.md", Path: "Notes/Slow.md"}, Handle: knowledge.NoteHandle("Notes/Slow.md"),
				Owner: knowledge.NoteHandle("Notes/Slow.md"),
				Type:  "note",
			}}},
			timedRetriever{name: "graph", result: []Candidate{{SourceRef: SourceRef{Title: "graph", NoteID: "Notes/Graph.md", Path: "Notes/Graph.md"}, Handle: knowledge.NoteHandle("Notes/Graph.md"),
				Owner: knowledge.NoteHandle("Notes/Graph.md"),
				Type:  "note",
			}}},
		},
		Ranker: stubRanker{},
	}

	resp, err := svc.Search(ctx, QuerySpec{Text: "q", Intent: IntentOverview, Limits: Limits{Total: 5}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "Notes/Graph.md", resp.Results[0].Path)
	require.Equal(t, LaneStateTimedOut, statusForLane(resp.Lanes, "code_vector").Status)
	require.Equal(t, LaneStateRan, statusForLane(resp.Lanes, "graph").Status)
	require.NotEmpty(t, resp.Warnings)
	require.Equal(t, "retriever_degraded", resp.Warnings[0].Code)
}

func TestSearch_MergesSupportOnlyEvidenceOntoPrimaryCandidate(t *testing.T) {
	handle := knowledge.AnchorHandle("anchor-search")
	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "intel_lexical", result: []Candidate{{SourceRef: SourceRef{Path: "pkg/search/service.go"}, Handle: handle,
				Owner: handle,
				Type:  "anchor",
				Evidence: []Evidence{{
					Type:     "intel_fts_match",
					RawScore: 0.6,
				}},
			}}},
			timedRetriever{name: "rationale_fts", result: []Candidate{{SourceRef: SourceRef{Path: "pkg/search/service.go"}, Handle: handle,
				Owner:       handle,
				SupportOnly: true,
				Type:        "anchor",
				Evidence: []Evidence{{
					Type:     "rationale_fts_match",
					RawScore: 0.7,
				}},
			}}},
		},
		Ranker: &WeightedRanker{Weights: Weights{Lexical: 1}, MaxPerOwner: 3},
	}

	resp, err := svc.Search(context.Background(), QuerySpec{Text: "why search", Intent: IntentSearch, Limits: Limits{Total: 5}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.False(t, resp.Results[0].SupportOnly)
	require.Contains(t, evidenceTypes(resp.Results[0].Evidence), "rationale_fts_match")
}

func TestSearch_DropsSupportOnlyCandidatesWithoutPrimaryEvidence(t *testing.T) {
	handle := knowledge.AnchorHandle("anchor-rationale-only")
	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "rationale_fts", result: []Candidate{{SourceRef: SourceRef{Path: "pkg/search/service.go"}, Handle: handle,
				Owner:       handle,
				SupportOnly: true,
				Type:        "anchor",
				Evidence: []Evidence{{
					Type:     "rationale_fts_match",
					RawScore: 0.9,
				}},
			}}},
		},
		Ranker: &WeightedRanker{Weights: Weights{Lexical: 1}, MaxPerOwner: 3},
	}

	resp, err := svc.Search(context.Background(), QuerySpec{Text: "why search", Intent: IntentSearch, Limits: Limits{Total: 5}})
	require.NoError(t, err)
	require.Empty(t, resp.Results)
	require.Equal(t, LaneStateRan, statusForLane(resp.Lanes, "rationale_fts").Status)
}

func TestPruneCandidatesKeepsSymbolProbeHits(t *testing.T) {
	candidates := map[string]Candidate{}
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("note:%02d", i)
		candidates[key] = Candidate{
			SourceRef: SourceRef{Path: key + ".md"},
			Handle:    knowledge.NoteHandle(key),
			Type:      "note",
			Evidence: []Evidence{{
				Type:     "note_vector_similarity",
				RawScore: 1,
			}},
		}
	}
	candidates["code:symbol"] = Candidate{
		SourceRef: SourceRef{
			Path:     "pkg/embeddings/sqlite/note_schema.go",
			Symbol:   "SearchChunksByVector",
			AnchorID: "symbol",
		},
		Handle: knowledge.AnchorHandle("symbol"),
		Type:   "code",
		Evidence: []Evidence{{
			Type:     "symbol_match",
			RawScore: 0.9,
		}},
	}

	pruned := pruneCandidates(candidates, 5, map[EvidenceChannel]float64{EvidenceChannelSemantic: 1, EvidenceChannelRefs: 0.1})
	require.Contains(t, pruned, "code:symbol")
}

func TestPruneCandidatesKeepsNotesNamedByAgreedLinkLabels(t *testing.T) {
	candidates := map[string]Candidate{}
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("note:%02d", i)
		candidates[key] = Candidate{SourceRef: SourceRef{Path: key + ".md"}, Handle: knowledge.NoteHandle(key), Type: "note", Evidence: []Evidence{{Type: "note_vector_similarity", RawScore: 1}}}
	}
	named := Evidence{Type: "link_text_match", RawScore: 0.9, Details: map[string]string{LinkTextAliasDetail: "catalog migration"}}
	candidates["note:named"] = Candidate{SourceRef: SourceRef{Path: "named.md"}, Handle: knowledge.NoteHandle("named"), Type: "note", Evidence: []Evidence{named}}
	candidates["note:labeled"] = Candidate{SourceRef: SourceRef{Path: "labeled.md"}, Handle: knowledge.NoteHandle("labeled"), Type: "note", Evidence: []Evidence{{Type: "link_text_match", RawScore: 0.9}}}

	pruned := pruneCandidates(candidates, 5, map[EvidenceChannel]float64{EvidenceChannelSemantic: 1, EvidenceChannelLexical: 0.6})
	require.Contains(t, pruned, "note:named")
	require.NotContains(t, pruned, "note:labeled", "link text without an agreed name competes on score")

	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("note:named%02d", i)
		candidates[key] = Candidate{SourceRef: SourceRef{Path: key + ".md"}, Handle: knowledge.NoteHandle(key), Type: "note", Evidence: []Evidence{named}}
	}
	candidates["note:exact"] = Candidate{SourceRef: SourceRef{Path: "exact.md"}, Handle: knowledge.NoteHandle("exact"), Type: "note", Evidence: []Evidence{{Type: "note_title_exact", RawScore: 0.1}}}
	pruned = pruneCandidates(candidates, 5, map[EvidenceChannel]float64{EvidenceChannelSemantic: 1, EvidenceChannelLexical: 0.6})
	require.Contains(t, pruned, "note:exact", "agreed labels never crowd out an exact title")
}

func evidenceTypes(evidence []Evidence) []string {
	out := make([]string, 0, len(evidence))
	for _, ev := range evidence {
		out = append(out, ev.Type)
	}
	return out
}

type expiredPacker struct{}

func (expiredPacker) Pack(context.Context, QuerySpec, []RankedResult) (PackedContext, error) {
	return PackedContext{}, context.DeadlineExceeded
}

func TestPackRankedResults_DeadlineFallbackKeepsSelectedSourcesWithinBudget(t *testing.T) {
	note := func(path string) RankedResult {
		return RankedResult{Candidate: Candidate{SourceRef: SourceRef{Title: path, Path: path}, Handle: knowledge.NoteHandle(path), Type: "note"}, FinalScore: 1}
	}
	results := []RankedResult{note("Notes/A.md"), note("Notes/B.md"), note("Notes/C.md")}
	const header = "Search timed out; returning a minimal pack.\n\n"
	const lineA = " 1. [note] Notes/A.md (Notes/A.md)\n"

	t.Run("total bounds the selected population", func(t *testing.T) {
		packed, err := PackRankedResults(context.Background(), expiredPacker{}, QuerySpec{Limits: Limits{Total: 2}, Budget: Budget{Chars: 2000}}, results)
		require.NoError(t, err)
		require.Contains(t, packed.Text, "Notes/A.md")
		require.Contains(t, packed.Text, "Notes/B.md")
		require.NotContains(t, packed.Text, "Notes/C.md")
	})
	t.Run("budget excludes the next line", func(t *testing.T) {
		budget := len(header) + len(lineA) + 10 // less than one more line
		packed, err := PackRankedResults(context.Background(), expiredPacker{}, QuerySpec{Limits: Limits{Total: 5}, Budget: Budget{Chars: budget}}, results)
		require.NoError(t, err)
		require.Equal(t, header+lineA, packed.Text)
		require.LessOrEqual(t, len(packed.Text), budget)
	})
}

func TestSearch_TieBreaksDeterministically(t *testing.T) {
	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "intel_lexical", result: []Candidate{
				{SourceRef: SourceRef{
					Title:  "B",
					NoteID: "Notes/B.md",
					Path:   "Notes/B.md",
				}, Handle: knowledge.NoteHandle("Notes/B.md"),
					Owner: knowledge.NoteHandle("Notes/B.md"),
					Type:  "note",
				},
				{SourceRef: SourceRef{
					Title:  "A",
					NoteID: "Notes/A.md",
					Path:   "Notes/A.md",
				}, Handle: knowledge.NoteHandle("Notes/A.md"),
					Owner: knowledge.NoteHandle("Notes/A.md"),
					Type:  "note",
				},
			}},
		},
		Ranker: reverseTieRanker{},
	}
	spec := QuerySpec{Text: "q", Limits: Limits{Total: 5}}
	resp, err := svc.Search(context.Background(), spec)
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	require.Equal(t, "Notes/A.md", resp.Results[0].Path)
	require.Equal(t, "Notes/B.md", resp.Results[1].Path)
}

func TestSearch_BroadIntentSoftFailsTopLevelRetriever(t *testing.T) {
	svc := Service{
		Retrievers: []Retriever{
			failingRetriever{name: "vector", err: context.DeadlineExceeded},
			timedRetriever{name: "intel_lexical", result: []Candidate{{SourceRef: SourceRef{Title: "fast", NoteID: "Notes/Fast.md", Path: "Notes/Fast.md"}, Handle: knowledge.NoteHandle("Notes/Fast.md"),
				Owner: knowledge.NoteHandle("Notes/Fast.md"),
				Type:  "note",
				Evidence: []Evidence{
					{Type: "intel_fts_match", RawScore: 0.8},
				},
			}}},
		},
		Ranker: stubRanker{},
	}

	resp, err := svc.Search(context.Background(), QuerySpec{Text: "fast", Intent: IntentOverview, Limits: Limits{Total: 5}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Warnings)
	require.Equal(t, "retriever_degraded", resp.Warnings[0].Code)
	require.Equal(t, LaneStateTimedOut, statusForLane(resp.Lanes, "code_vector").Status)
	require.Equal(t, LaneStateRan, statusForLane(resp.Lanes, "intel_fts").Status)
}

func TestSearch_PrecisionIntentFailsOnStageTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "definition", delay: 350 * time.Millisecond},
			timedRetriever{name: "intel_lexical", result: []Candidate{{SourceRef: SourceRef{Title: "fast", NoteID: "Notes/Fast.md", Path: "Notes/Fast.md"}, Handle: knowledge.NoteHandle("Notes/Fast.md"),
				Owner: knowledge.NoteHandle("Notes/Fast.md"),
				Type:  "note",
			}}},
		},
		Ranker: stubRanker{},
	}

	_, err := svc.Search(ctx, QuerySpec{Text: "Service.Search", Intent: IntentGoToDef, Limits: Limits{Total: 5}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "definition retriever")
}

func TestSearch_LanesSplitVectorEvidenceByType(t *testing.T) {
	svc := Service{
		Retrievers: []Retriever{
			timedRetriever{name: "vector", result: []Candidate{{SourceRef: SourceRef{Title: "fast", NoteID: "Notes/Fast.md", Path: "Notes/Fast.md"}, Handle: knowledge.NoteHandle("Notes/Fast.md"),
				Owner: knowledge.NoteHandle("Notes/Fast.md"),
				Type:  "note",
				Evidence: []Evidence{
					{Type: "note_vector_similarity", RawScore: 0.9},
				},
			}}},
		},
		Ranker: stubRanker{},
	}

	resp, err := svc.Search(context.Background(), QuerySpec{Text: "fast", Intent: IntentOverview, Limits: Limits{Total: 5}})
	require.NoError(t, err)

	require.Equal(t, LaneStateRan, statusForLane(resp.Lanes, "note_vector").Status)
	codeLane := statusForLane(resp.Lanes, "code_vector")
	require.Equal(t, LaneStateEmpty, codeLane.Status)
	require.Equal(t, "vector ran but returned no code evidence", codeLane.Reason)
}

type expiredRanker struct{}

func (expiredRanker) Rank(context.Context, QuerySpec, []Candidate) ([]RankedResult, error) {
	return nil, context.DeadlineExceeded
}

func TestSearch_RankFallbackUsesRankerMaxPerOwner(t *testing.T) {
	// Sections of one note share an owner; note-type candidates of one owner
	// are coalesced before ranking, so the cap is exercised with sections.
	candidate := func(path, owner string, score float64) Candidate {
		return Candidate{
			SourceRef: SourceRef{Path: path},
			Handle:    knowledge.NoteHandle(path),
			Owner:     knowledge.NoteHandle(owner),
			Type:      "doc_section",
			Evidence:  []Evidence{{Type: "intel_fts_match", RawScore: score}},
		}
	}
	svc := Service{
		Retrievers: []Retriever{staticRetriever{name: "all", results: []Candidate{
			candidate("notes/a1.md", "owner-a", 0.9),
			candidate("notes/a2.md", "owner-a", 0.8),
			candidate("notes/a3.md", "owner-a", 0.7),
			candidate("notes/b.md", "owner-b", 0.6),
		}}},
		Ranker: expiredRanker{},
		Scorer: &WeightedRanker{Weights: Weights{Lexical: 1}, MaxPerOwner: 2},
	}

	resp, err := svc.Search(context.Background(), QuerySpec{Text: "q", Intent: IntentOverview, Limits: Limits{Total: 4}})
	require.NoError(t, err)
	paths := make([]string, 0, len(resp.Results))
	for _, result := range resp.Results {
		paths = append(paths, result.Path)
	}
	require.Equal(t, []string{"notes/a1.md", "notes/a2.md", "notes/b.md"}, paths)
}

func statusForLane(lanes []LaneStatus, lane string) LaneStatus {
	for _, status := range lanes {
		if status.Lane == lane {
			return status
		}
	}
	return LaneStatus{}
}
