package unifiedsearch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type recordingPacker struct {
	paths [][]string
}

func (p *recordingPacker) Pack(_ context.Context, _ search.QuerySpec, results []search.RankedResult) (search.PackedContext, error) {
	paths := make([]string, 0, len(results))
	for _, result := range results {
		paths = append(paths, result.Path)
	}
	p.paths = append(p.paths, paths)
	return search.PackedContext{Text: strings.Join(paths, ",")}, nil
}

func TestExecuteRejectsContradictoryTestEligibility(t *testing.T) {
	_, err := Execute(context.Background(), ApplicationOptions{Runtime: Options{Filters: search.Filters{TestsOnly: true, ExcludeTests: true}}})
	require.EqualError(t, err, "tests-only and exclude-tests filters cannot be combined")
}

type changingQueryProvider struct {
	base  embeddings.Provider
	calls atomic.Int32
}

func (p *changingQueryProvider) Dimensions() int { return p.base.Dimensions() }

func (p *changingQueryProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	vectors, err := p.base.EmbedTexts(ctx, texts)
	if err != nil {
		return nil, err
	}
	if p.calls.Add(1) > 1 {
		for i := range vectors {
			for j := range vectors[i] {
				vectors[i][j] = -vectors[i][j]
			}
		}
	}
	return vectors, nil
}

func TestExecuteCanonicalizesExplicitQueriesForRetrievalAnswerAndContinuation(t *testing.T) {
	var received [][]QueryInput
	runRuntime := func(_ context.Context, opts Options) (Result, error) {
		received = append(received, append([]QueryInput(nil), opts.QueryInputs...))
		return Result{IndexGeneration: "generation-1", Lanes: []search.LaneStatus{{Lane: "note_lexical", Status: search.LaneStateRan}}, Results: []search.RankedResult{
			{Candidate: search.Candidate{Handle: knowledge.NoteHandle("policy.md"), Path: "policy.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: 1},
			{Candidate: search.Candidate{Handle: knowledge.NoteHandle("context.md"), Path: "context.md", Type: "note", Evidence: []search.Evidence{{Type: "note_lexical", RawScore: .5}}}, FinalScore: .5},
		}}, nil
	}
	duplicate := Options{IntentInput: string(search.IntentSearch), QueryInputs: []QueryInput{{Text: "policy"}, {Text: " policy ", Mode: string(search.IntentSearch)}}, VaultPath: t.TempDir()}
	first, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Runtime: duplicate, runRuntime: runRuntime})
	require.NoError(t, err)
	require.Equal(t, []QueryInput{{Text: "policy", Mode: string(search.IntentSearch)}}, first.Request.Queries)
	require.Equal(t, first.Request.Queries, received[0])
	require.Len(t, first.Answer.MustRead, 1, "equivalent raw facets must not require unavailable multi-query facet evidence")
	require.NotEmpty(t, first.Continuation)

	canonical := duplicate
	canonical.QueryInputs = []QueryInput{{Text: "policy", Mode: string(search.IntentSearch)}}
	second, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Runtime: canonical, Continuation: first.Continuation, runRuntime: runRuntime})
	require.NoError(t, err)
	require.Equal(t, first.RequestIdentity, second.RequestIdentity)
	require.Equal(t, first.Request.Queries, received[1])
}

func TestExecuteDoesNotClaimExhaustiveRelationshipWhenMoreResultsRemain(t *testing.T) {
	results := make([]search.RankedResult, 0, 7)
	for index := 0; index < 7; index++ {
		results = append(results, search.RankedResult{Candidate: search.Candidate{
			Handle: knowledge.AnchorHandle(fmt.Sprintf("caller-%d", index)), Path: fmt.Sprintf("caller-%d.go", index), FQN: fmt.Sprintf("pkg.Caller%d", index), Type: "anchor",
			Evidence: []search.Evidence{{Type: "call_edge", RawScore: 1}},
		}, FinalScore: 1 - float64(index)/10})
	}
	result, err := Execute(context.Background(), ApplicationOptions{
		Profile: ProfileAgent, VisibleLimit: 6,
		Runtime: Options{Query: "callers of Target", IntentInput: string(search.IntentCallers), VaultPath: t.TempDir()},
		runRuntime: func(context.Context, Options) (Result, error) {
			return Result{
				IndexGeneration: "generation-1", Results: results,
				Lanes:        []search.LaneStatus{{Lane: "call_edges", Status: search.LaneStateRan}},
				TargetStatus: search.TargetStatusInferredSymbol, TargetConfidence: .95, ResolvedTarget: &search.TargetCandidate{FQN: "pkg.Target"},
			}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Counts.RemainingWindow)
	require.Contains(t, result.Answer.Coverage.Missing, "relationship:caller:more_results")
	require.NotEqual(t, "high", result.Answer.Confidence.Level)
	require.NotEmpty(t, result.Continuation)
}

func TestRemainingExhaustiveProofsIgnoresSupportingRemainderAndFindsPrecisionFacet(t *testing.T) {
	supporting := SourceAssessment{Eligibility: EvidenceSupporting, Relationship: "caller", Relevance: RelevanceStrong}
	require.Empty(t, remainingExhaustiveProofs(search.IntentCallers, nil, []SourceAssessment{supporting}))

	const facetText = "who calls Target"
	caller := SourceAssessment{Eligibility: EvidencePrimary, Relationship: "caller", Relevance: RelevanceStrong, FacetSupport: []FacetSupport{{
		Text: facetText, Mode: search.IntentCallers, Relevance: RelevanceStrong, Eligibility: EvidencePrimary, Relationship: "caller", TargetEstablished: true,
	}}}
	require.Equal(t, []string{"facet:" + facetText + ":more_results"}, remainingExhaustiveProofs(search.IntentSearch, []QueryInput{{Text: facetText, Mode: string(search.IntentCallers)}}, []SourceAssessment{caller}))
}

func TestExecuteRejectsMalformedContinuationBeforeProviderWork(t *testing.T) {
	var factoryCalls atomic.Int32
	_, err := Execute(context.Background(), ApplicationOptions{
		Profile: ProfileInteractive, Continuation: "not-a-cursor",
		Runtime: Options{Query: "search", IntentInput: string(search.IntentSearch), UseVector: true, VaultPath: t.TempDir(), EmbCfg: embeddings.Config{Enabled: true, Provider: "test", Dimensions: 8, IndexPath: "missing.sqlite"}},
		runRuntime: func(context.Context, Options) (Result, error) {
			factoryCalls.Add(1)
			return Result{}, nil
		},
	})
	require.ErrorIs(t, err, ErrCursorInvalid)
	require.Zero(t, factoryCalls.Load())
}

func TestExecuteCanExplicitlyDisableProfileTimeout(t *testing.T) {
	runRuntime := func(ctx context.Context, _ Options) (Result, error) {
		_, hasDeadline := ctx.Deadline()
		require.False(t, hasDeadline)
		return Result{}, nil
	}
	_, err := Execute(context.Background(), ApplicationOptions{
		Profile: ProfileInteractive, NoDefaultTimeout: true,
		Runtime: Options{Query: "unbounded"}, runRuntime: runRuntime,
	})
	require.NoError(t, err)
}

func TestExecutePacksOnlyTheSelectedCanonicalPage(t *testing.T) {
	packer := &recordingPacker{}
	results := []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.NoteHandle("alpha.md"), Path: "alpha.md", Type: "note"}, FinalScore: 3},
		{Candidate: search.Candidate{Handle: knowledge.NoteHandle("beta.md"), Path: "beta.md", Type: "note"}, FinalScore: 2},
		{Candidate: search.Candidate{Handle: knowledge.NoteHandle("gamma.md"), Path: "gamma.md", Type: "note"}, FinalScore: 1},
	}
	runRuntime := func(_ context.Context, opts Options) (Result, error) {
		require.True(t, opts.DeferPack)
		return Result{Results: results, IndexGeneration: "generation-1", DeferredPacker: packer, PackSpec: search.QuerySpec{Text: "guide", Budget: search.Budget{Chars: 2_000}}}, nil
	}
	runtime := Options{Query: "guide", IntentInput: string(search.IntentSearch), VaultPath: t.TempDir(), Pack: true}
	first, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Runtime: runtime, runRuntime: runRuntime})
	require.NoError(t, err)
	require.Equal(t, "alpha.md", first.PackedText)
	require.NotEmpty(t, first.Continuation)

	second, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Continuation: first.Continuation, Runtime: runtime, runRuntime: runRuntime})
	require.NoError(t, err)
	require.Equal(t, "beta.md", second.PackedText)
	require.Equal(t, [][]string{{"alpha.md"}, {"beta.md"}}, packer.paths)
}

func TestExecuteMaterializesRawDisplayBeforeRuntimeCleanup(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	anchor := codeanchor.IntelAnchor{
		AnchorID: "anchor-sync", Lang: codeanchor.LangGo, Kind: "function", Path: "dispatch.go",
		Symbol: "Sync", FQN: "example.com/dispatch.Sync", StartLine: 3, EndLine: 5, Fingerprint: "sync-fingerprint",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	anchor, found, err := store.IntelAnchorByID(ctx, anchor.AnchorID)
	require.NoError(t, err)
	require.True(t, found)
	owner := knowledge.NoteHandle("guide.md")
	results := []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.AnchorHandle(anchor.AnchorID), Type: "code", Path: anchor.Path, AnchorID: anchor.AnchorID, FQN: anchor.FQN}, FinalScore: 1},
		{Candidate: search.Candidate{Handle: knowledge.NoteChunkHandle("guide.md", 0), Owner: owner, Type: "note", Path: "guide.md", NoteID: "guide.md", Heading: "First", ChunkIndex: 0}, FinalScore: .9},
		{Candidate: search.Candidate{Handle: knowledge.NoteChunkHandle("guide.md", 1), Owner: owner, Type: "note", Path: "guide.md", NoteID: "guide.md", Heading: "Second", ChunkIndex: 1}, FinalScore: .8},
	}
	cleaned := false
	runRuntime := func(context.Context, Options) (Result, error) {
		return Result{
			Results: results, Display: CoalesceNoteResults(results), IntelStore: store, IndexGeneration: "generation-1",
			Cleanup: func() { cleaned = true; _ = store.Close() },
		}, nil
	}

	result, err := Execute(ctx, ApplicationOptions{
		Profile: ProfileInteractive, VisibleLimit: 5, MaterializeDisplay: true,
		Runtime: Options{Query: "sync guide", VaultPath: t.TempDir()}, runRuntime: runRuntime,
	})
	require.NoError(t, err)
	require.True(t, cleaned)
	require.Len(t, result.Display, 2)
	require.Equal(t, anchor, *result.Display[0].Anchor)
	require.Len(t, result.Display[1].NoteChunks, 2)
	require.Equal(t, []string{"First", "Second"}, []string{result.Display[1].NoteChunks[0].Heading, result.Display[1].NoteChunks[1].Heading})
}

func TestExecuteRealRuntimeContinuationKeepsVectorWindowWithChangingProvider(t *testing.T) {
	vault := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(vault, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n  go:\n    roots: [pkg]\ncodeEmbeddings:\n  enabled: true\n  provider: test\n  dimensions: 8\n"), 0o644))
	base := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := semdb.Open(filepath.Join(vault, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	for i, name := range []string{"Alpha", "Beta", "Gamma"} {
		path := filepath.ToSlash(filepath.Join("pkg", strings.ToLower(name)+".go"))
		require.NoError(t, os.WriteFile(filepath.Join(vault, filepath.FromSlash(path)), []byte("package pkg\n\nfunc "+name+"() {}\n"), 0o644))
		require.NoError(t, store.UpsertFileMeta(context.Background(), codeanchor.FileMeta{Path: path, Lang: codeanchor.LangGo, Hash: fmt.Sprintf("file-hash-%d", i), ParseStatus: codeanchor.ParseOK}))
		anchorID, chunkID := "anchor-"+strings.ToLower(name), "chunk-"+strings.ToLower(name)
		require.NoError(t, store.ReplaceIntelCodeFile(context.Background(), path, []codeanchor.IntelAnchor{{AnchorID: anchorID, Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: name, FQN: "pkg." + name, Signature: "func " + name + "()", StartLine: 3, EndLine: 3, Fingerprint: fmt.Sprintf("fp-%d", i)}}, nil, nil))
		require.NoError(t, store.ReplaceIntelChunks(context.Background(), []string{anchorID}, []codeanchor.IntelChunk{{ChunkID: chunkID, OwnerID: anchorID, OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: fmt.Sprintf("hash-%d", i)}}))
		vectors, embedErr := base.EmbedTexts(context.Background(), []string{"search"})
		require.NoError(t, embedErr)
		require.NoError(t, store.UpsertEmbeddings(context.Background(), map[string]embeddings.Embedding{chunkID: vectors[0]}))
	}
	provider := &changingQueryProvider{base: base}
	runtime := Options{
		Query: "search", IntentInput: string(search.IntentSearch), UseVector: true, UseIntel: true,
		VaultPath: vault, VaultDef: obsidian.VaultDefinition{Path: vault}, IntelStore: store,
		CodeProvider: provider, CodeProviderConfig: embeddings.ProviderConfig{Provider: "test", Dimensions: 8},
	}
	t.Cleanup(func() { _ = store.Close() })
	first, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Runtime: runtime})
	require.NoError(t, err)
	require.NotEmpty(t, first.Continuation)
	require.Equal(t, int32(1), provider.calls.Load())
	cursor, err := DecodeContinuation(first.Continuation)
	require.NoError(t, err)
	require.NotEmpty(t, cursor.QueryEmbeddings)
	second, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Continuation: first.Continuation, Runtime: runtime})
	require.NoError(t, err)
	require.Equal(t, int32(1), provider.calls.Load())
	require.NotEqual(t, first.Sources[0].Result.Path, second.Sources[0].Result.Path)
}

func TestExecuteInfersDefaultIntentWhenModeOmitted(t *testing.T) {
	capture := func(captured *Options) func(context.Context, Options) (Result, error) {
		return func(_ context.Context, opts Options) (Result, error) {
			*captured = opts
			return Result{IndexGeneration: "generation-1", Lanes: []search.LaneStatus{{Lane: "note_lexical", Status: search.LaneStateRan}}}, nil
		}
	}

	var inferred Options
	result, err := Execute(context.Background(), ApplicationOptions{
		Profile:    ProfileInteractive,
		Runtime:    Options{Query: "how does indexing work", VaultPath: t.TempDir()},
		runRuntime: capture(&inferred),
	})
	require.NoError(t, err)
	require.Equal(t, string(search.IntentOverview), inferred.IntentInput)
	require.Equal(t, "inferred", result.Request.IntentSource)
	require.Greater(t, result.Request.IntentScore, 0.0)

	var explicit Options
	explicitResult, err := Execute(context.Background(), ApplicationOptions{
		Profile:    ProfileInteractive,
		Runtime:    Options{Query: "search subsystem", IntentInput: string(search.IntentSearch), VaultPath: t.TempDir()},
		runRuntime: capture(&explicit),
	})
	require.NoError(t, err)
	require.Equal(t, string(search.IntentSearch), explicit.IntentInput)
	require.Equal(t, "explicit", explicitResult.Request.IntentSource)
}

func TestExecuteKeepsMixedExplicitFacetModesUninferred(t *testing.T) {
	var captured Options
	result, err := Execute(context.Background(), ApplicationOptions{
		Profile: ProfileInteractive,
		Runtime: Options{
			QueryInputs: []QueryInput{
				{Text: "how does indexing work", Mode: string(search.IntentSearch)},
				{Text: "callers of Execute", Mode: string(search.IntentCallers)},
			},
			VaultPath: t.TempDir(),
		},
		runRuntime: func(_ context.Context, opts Options) (Result, error) {
			captured = opts
			return Result{IndexGeneration: "generation-1"}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, string(search.IntentSearch), captured.IntentInput)
	require.Equal(t, "explicit", result.Request.IntentSource)
	require.Zero(t, result.Request.IntentScore)
}
