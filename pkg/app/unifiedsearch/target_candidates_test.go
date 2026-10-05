package unifiedsearch

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestTargetCandidateResultsMaterializeAmbiguousCandidatesAsSupporting(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "go/worker/worker.go", []codeanchor.IntelAnchor{{AnchorID: "anchor-go-run", Lang: codeanchor.LangGo, Kind: "function", Path: "go/worker/worker.go", Symbol: "Run", FQN: "example.com/polyglot/worker.Run", StartLine: 3, EndLine: 5, Fingerprint: "fp-go"}}, nil, nil))

	results := targetCandidateResults(ctx, store, search.Filters{}, []search.TargetCandidate{
		{Path: "go/worker/worker.go", Symbol: "Run", FQN: "example.com/polyglot/worker.Run", Kind: "function", Reason: "indexed_symbol_match"},
		{Path: "src/todoapp/main.py", Reason: "exact_path_match"},
		{Path: "src/todoapp/main.py", Reason: "exact_path_match"},
	})

	require.Len(t, results, 2, "duplicate candidates collapse")
	require.Equal(t, "anchor", results[0].Type)
	require.Equal(t, "anchor-go-run", results[0].AnchorID)
	require.Equal(t, "code", results[1].Type)
	require.Equal(t, "src/todoapp/main.py", results[1].Path)
	require.Greater(t, results[0].FinalScore, results[1].FinalScore, "candidate order is preserved")
	for _, result := range results {
		require.False(t, search.HasPrimaryEvidence(search.IntentGoToDef, result.Evidence), "candidates never claim the resolved definition")
	}
	require.Empty(t, targetCandidateResults(ctx, nil, search.Filters{}, nil))
	scoped := targetCandidateResults(ctx, store, search.Filters{Types: []string{"note"}}, []search.TargetCandidate{{Path: "src/todoapp/main.py", Reason: "exact_path_match"}})
	require.Empty(t, scoped, "candidates honor the caller's eligibility filters")
}
