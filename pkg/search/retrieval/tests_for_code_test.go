package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestTestsForCodeRetriever_PromotesOnlyPersistedDirectCallerTestAnchor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "tests-for-code.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	target := codeanchor.IntelAnchor{AnchorID: "target", Lang: codeanchor.LangGo, Kind: "method", Path: "internal/queue.go", Symbol: "Drain", FQN: "example.com/app/internal.Queue.Drain", Fingerprint: "target-fp"}
	testAnchor := codeanchor.IntelAnchor{AnchorID: "test-direct", Lang: codeanchor.LangGo, Kind: "function", Path: "internal/queue_test.go", Symbol: "TestDrainRejectsTerminalOperation", FQN: "example.com/app/internal.TestDrainRejectsTerminalOperation", Fingerprint: "direct-fp"}
	unrelated := codeanchor.IntelAnchor{AnchorID: "test-unrelated", Lang: codeanchor.LangGo, Kind: "function", Path: "internal/queue_test.go", Symbol: "TestDrainUnrelated", FQN: "example.com/app/internal.TestDrainUnrelated", Fingerprint: "unrelated-fp"}
	outOfScope := codeanchor.IntelAnchor{AnchorID: "test-out-of-scope", Lang: codeanchor.LangGo, Kind: "function", Path: "aaa/early_test.go", Symbol: "AFirstTest", FQN: "example.com/app/aaa.AFirstTest", Fingerprint: "outside-fp"}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, target.Path, []codeanchor.IntelAnchor{target}, nil, nil))
	productionCallers := []codeanchor.IntelAnchor{
		{AnchorID: "caller-a", Lang: codeanchor.LangGo, Kind: "function", Path: "internal/a.go", Symbol: "ACaller", FQN: "example.com/app/internal.ACaller", Fingerprint: "a-fp"},
		{AnchorID: "caller-b", Lang: codeanchor.LangGo, Kind: "function", Path: "internal/b.go", Symbol: "BCaller", FQN: "example.com/app/internal.BCaller", Fingerprint: "b-fp"},
		{AnchorID: "caller-c", Lang: codeanchor.LangGo, Kind: "function", Path: "internal/c.go", Symbol: "CCaller", FQN: "example.com/app/internal.CCaller", Fingerprint: "c-fp"},
	}
	for _, caller := range productionCallers {
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, caller.Path, []codeanchor.IntelAnchor{caller}, []codeanchor.IntelEdge{{SrcID: caller.AnchorID, DstID: target.AnchorID, Kind: "calls"}}, nil))
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, outOfScope.Path, []codeanchor.IntelAnchor{outOfScope}, []codeanchor.IntelEdge{{SrcID: outOfScope.AnchorID, DstID: target.AnchorID, Kind: "calls"}}, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, testAnchor.Path, []codeanchor.IntelAnchor{testAnchor, unrelated}, []codeanchor.IntelEdge{{SrcID: testAnchor.AnchorID, DstID: target.AnchorID, Kind: "calls"}}, nil))

	r := &TestsForCodeRetriever{Store: store, VaultPath: root, Limit: 1}
	candidates, err := r.Retrieve(ctx, search.QuerySpec{
		Intent:         search.IntentTestsForCode,
		Seeds:          []knowledge.Handle{knowledge.AnchorHandle(target.AnchorID)},
		ResolvedTarget: &search.TargetCandidate{Path: target.Path, Symbol: target.Symbol, FQN: target.FQN},
		Filters:        search.Filters{PathPrefixes: []string{"internal"}},
	})
	require.NoError(t, err)
	require.NotEmpty(t, candidates)

	require.Equal(t, testAnchor.AnchorID, candidates[0].AnchorID)
	require.Equal(t, testAnchor.FQN, candidates[0].FQN)
	require.True(t, search.HasPrimaryEvidence(search.IntentTestsForCode, candidates[0].Evidence))
	require.Equal(t, target.FQN, candidates[0].Evidence[0].Details["target_fqn"])
	for _, candidate := range candidates {
		require.NotEqual(t, unrelated.AnchorID, candidate.AnchorID, "name similarity without a persisted edge is not relationship proof")
		if candidate.Path == testAnchor.Path && candidate.AnchorID == "" {
			require.False(t, search.HasPrimaryEvidence(search.IntentTestsForCode, candidate.Evidence), "file context must not duplicate the exact test anchor as primary")
		}
	}
}

func TestTestsForCodeRetriever_DoesNotPromoteAmbiguousGoMethodFallback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "ambiguous-tests-for-code.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	target := codeanchor.IntelAnchor{AnchorID: "queue-drain", Lang: codeanchor.LangGo, Kind: "method", Path: "internal/queue.go", Symbol: "Drain", FQN: "example.com/app/internal.Queue.Drain", Fingerprint: "queue-fp"}
	distractor := codeanchor.IntelAnchor{AnchorID: "other-drain", Lang: codeanchor.LangGo, Kind: "method", Path: "internal/other.go", Symbol: "Drain", FQN: "example.com/app/internal.Other.Drain", Fingerprint: "other-fp"}
	testAnchor := codeanchor.IntelAnchor{AnchorID: "test-drain", Lang: codeanchor.LangGo, Kind: "function", Path: "internal/queue_test.go", Symbol: "TestDrain", FQN: "example.com/app/internal.TestDrain", Fingerprint: "test-fp"}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, target.Path, []codeanchor.IntelAnchor{target}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, distractor.Path, []codeanchor.IntelAnchor{distractor}, nil, nil))
	// This mirrors the package-empty Go fallback: one syntactic call can resolve
	// to every same-named receiver method in the package.
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, testAnchor.Path, []codeanchor.IntelAnchor{testAnchor}, []codeanchor.IntelEdge{
		{SrcID: testAnchor.AnchorID, DstID: target.AnchorID, Kind: "calls"},
		{SrcID: testAnchor.AnchorID, DstID: distractor.AnchorID, Kind: "calls"},
	}, nil))

	r := &TestsForCodeRetriever{Store: store, VaultPath: root, Limit: 5}
	candidates, err := r.Retrieve(ctx, search.QuerySpec{
		Intent:         search.IntentTestsForCode,
		Seeds:          []knowledge.Handle{knowledge.AnchorHandle(target.AnchorID)},
		ResolvedTarget: &search.TargetCandidate{Path: target.Path, Symbol: target.Symbol, FQN: target.FQN},
	})
	require.NoError(t, err)
	for _, candidate := range candidates {
		require.NotEqual(t, testAnchor.AnchorID, candidate.AnchorID)
		require.False(t, search.HasPrimaryEvidence(search.IntentTestsForCode, candidate.Evidence), "a heuristic file match cannot prove an ambiguous symbol relationship")
	}
}

func TestTestsForCodeRetriever_FindsSiblingTestFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	codeRel := filepath.ToSlash(filepath.Join("pkg", "foo.go"))
	testRel := filepath.ToSlash(filepath.Join("pkg", "foo_test.go"))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(codeRel)), []byte("package pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(testRel)), []byte("package pkg\n"), 0o644))

	r := &TestsForCodeRetriever{VaultPath: root, Limit: 5}
	cands, err := r.Retrieve(ctx, search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.FileHandle(codeRel)},
	})
	require.NoError(t, err)
	require.NotEmpty(t, cands)

	found := false
	for _, c := range cands {
		if filepath.ToSlash(c.Path) == testRel {
			found = true
			require.Equal(t, "tests_path", c.Evidence[0].Type)
		}
	}
	require.True(t, found, "expected test file to be returned")
}

func TestTestsForCodeRetriever_FindsGoPackageTestReferencingSeedSymbol(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	codeRel := filepath.ToSlash(filepath.Join("pkg", "ontology", "query", "execute.go"))
	testRel := filepath.ToSlash(filepath.Join("pkg", "ontology", "query", "query_test.go"))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "ontology", "query"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(codeRel)), []byte("package query\n\nfunc Execute() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(testRel)), []byte("package query\n\nfunc TestExecute_ResolvesNodes(t *testing.T) { Execute() }\n"), 0o644))

	r := &TestsForCodeRetriever{VaultPath: root, Limit: 5}
	cands, err := r.Retrieve(ctx, search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.FileHandle(codeRel)},
	})
	require.NoError(t, err)

	found := false
	for _, c := range cands {
		if filepath.ToSlash(c.Path) == testRel {
			found = true
			require.Equal(t, "tests_package", c.Evidence[0].Type)
		}
	}
	require.True(t, found, "expected package-level Go test to be returned")
}

func TestTestsForCodeRetriever_UsesExplicitSeedPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	codeRel := filepath.ToSlash(filepath.Join("pkg", "ontology", "query", "execute.go"))
	testRel := filepath.ToSlash(filepath.Join("pkg", "ontology", "query", "query_test.go"))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "ontology", "query"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(codeRel)), []byte("package query\n\nfunc Execute() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(testRel)), []byte("package query\n\nfunc TestExecute_ResolvesNodes(t *testing.T) { Execute() }\n"), 0o644))

	r := &TestsForCodeRetriever{VaultPath: root, Limit: 5}
	cands, err := r.Retrieve(ctx, search.QuerySpec{
		ExplicitSeedPaths: []string{codeRel},
		HasExplicitSeeds:  true,
	})
	require.NoError(t, err)

	require.NotEmpty(t, cands)
	require.Equal(t, testRel, filepath.ToSlash(cands[0].Path))
}

func TestTestsForCodeRetriever_FileSeedReturnsExactPersistedTestAnchors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "file-seed-exact-tests.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	target := codeanchor.IntelAnchor{AnchorID: "merge", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/search/merge.go", Symbol: "MergeCandidate", FQN: "example/search.MergeCandidate", Fingerprint: "merge-fp"}
	testAnchor := codeanchor.IntelAnchor{AnchorID: "test-merge", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/search/merge_test.go", Symbol: "TestMergeProvenanceIsDeterministic", FQN: "example/search.TestMergeProvenanceIsDeterministic", Fingerprint: "test-fp"}
	otherTest := codeanchor.IntelAnchor{AnchorID: "test-other", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/search/merge_test.go", Symbol: "TestMergeUnrelatedMetadata", FQN: "example/search.TestMergeUnrelatedMetadata", Fingerprint: "other-fp"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, target.Path, []codeanchor.IntelAnchor{target}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, testAnchor.Path, []codeanchor.IntelAnchor{otherTest, testAnchor}, []codeanchor.IntelEdge{
		{SrcID: otherTest.AnchorID, DstID: target.AnchorID, Kind: "calls"},
		{SrcID: testAnchor.AnchorID, DstID: target.AnchorID, Kind: "calls"},
	}, nil))

	r := &TestsForCodeRetriever{Store: store, VaultPath: root, Limit: 5}
	candidates, err := r.Retrieve(ctx, search.QuerySpec{
		Text: "Find tests proving equivalent observations preserve provenance", Intent: search.IntentTestsForCode,
		ExplicitSeedPaths: []string{target.Path}, HasExplicitSeeds: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, candidates)
	require.Equal(t, testAnchor.AnchorID, candidates[0].AnchorID)
	require.Equal(t, testAnchor.FQN, candidates[0].FQN)
	require.True(t, search.HasPrimaryEvidence(search.IntentTestsForCode, candidates[0].Evidence))
	require.Equal(t, target.FQN, candidates[0].Evidence[0].Details["target_fqn"])
	for _, candidate := range candidates {
		if candidate.AnchorID == "" {
			require.False(t, search.HasPrimaryEvidence(search.IntentTestsForCode, candidate.Evidence))
		}
	}
}

func TestTestsForCodeRetriever_FileSeedRetainsEveryTargetForDuplicateCaller(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "file-seed-multiple-targets.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	targetA := codeanchor.IntelAnchor{AnchorID: "target-a", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/merge.go", Symbol: "MergeCandidate", FQN: "example/pkg.MergeCandidate", Fingerprint: "a"}
	targetB := codeanchor.IntelAnchor{AnchorID: "target-b", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/merge.go", Symbol: "MergeEvidence", FQN: "example/pkg.MergeEvidence", Fingerprint: "b"}
	testAnchor := codeanchor.IntelAnchor{AnchorID: "test-both", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/merge_test.go", Symbol: "TestMerge", FQN: "example/pkg.TestMerge", Fingerprint: "test"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, targetA.Path, []codeanchor.IntelAnchor{targetA, targetB}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, testAnchor.Path, []codeanchor.IntelAnchor{testAnchor}, []codeanchor.IntelEdge{
		{SrcID: testAnchor.AnchorID, DstID: targetA.AnchorID, Kind: "calls"},
		{SrcID: testAnchor.AnchorID, DstID: targetB.AnchorID, Kind: "calls"},
	}, nil))

	r := &TestsForCodeRetriever{Store: store, Limit: 1}
	candidates, err := r.Retrieve(ctx, search.QuerySpec{Intent: search.IntentTestsForCode, ExplicitSeedPaths: []string{targetA.Path}, HasExplicitSeeds: true})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, testAnchor.AnchorID, candidates[0].AnchorID)
	targets := map[string]bool{}
	for _, evidence := range candidates[0].Evidence {
		if evidence.Type == "tests_path" {
			targets[evidence.Details["target_fqn"]] = true
		}
	}
	require.Equal(t, map[string]bool{targetA.FQN: true, targetB.FQN: true}, targets)
}
