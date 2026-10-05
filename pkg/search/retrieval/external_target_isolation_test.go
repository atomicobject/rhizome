package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestSearchAndDefinitionConsumersExcludeExternalTargets(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	local := codeanchor.IntelAnchor{
		AnchorID: "local-render", Lang: codeanchor.LangTS, Kind: "function", Path: "src/local.ts",
		Symbol: "RenderLocal", FQN: "src.local.RenderLocal", Fingerprint: "local-render-fp",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, local.Path, []codeanchor.IntelAnchor{local}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "anchor", ItemID: local.AnchorID, Path: local.Path,
		Title: local.Symbol, Body: "local renderer snippet",
	}}))
	seedExternalSearchAssociation(t, ctx, store)

	lexical := &IntelLexicalRetriever{Store: store}
	generalSearch := &search.Service{Retrievers: []search.Retriever{lexical}, Ranker: externalIsolationRanker{}}
	response, err := generalSearch.Search(ctx, search.QuerySpec{Text: "renderer", Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	results := response.Results
	require.Len(t, results, 1)
	require.Equal(t, local.AnchorID, results[0].AnchorID)
	require.Equal(t, local.Path, results[0].Path)
	require.NotEmpty(t, results[0].Evidence)
	require.Contains(t, results[0].Evidence[0].Details["snippet"], "local")
	require.NotContains(t, results[0].Evidence[0].Details["snippet"], "useState")

	response, err = generalSearch.Search(ctx, search.QuerySpec{Text: "useState", Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	require.Empty(t, response.Results)

	definitions := &DefinitionRetriever{Store: store, Limit: 10}
	definitionSearch := &search.Service{Retrievers: []search.Retriever{definitions}, Ranker: externalIsolationRanker{}}
	response, err = definitionSearch.Search(ctx, search.QuerySpec{Text: "go to definition for RenderLocal", Intent: search.IntentGoToDef})
	require.NoError(t, err)
	results = response.Results
	require.Len(t, results, 1)
	require.Equal(t, local.AnchorID, results[0].AnchorID)
	require.Equal(t, local.Path, results[0].Path)

	response, err = definitionSearch.Search(ctx, search.QuerySpec{Text: "go to definition for react.useState", Intent: search.IntentGoToDef})
	require.NoError(t, err)
	require.Empty(t, response.Results)
}

type externalIsolationRanker struct{}

func (externalIsolationRanker) Rank(_ context.Context, _ search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	results := make([]search.RankedResult, 0, len(candidates))
	for _, candidate := range candidates {
		results = append(results, search.RankedResult{Candidate: candidate, FinalScore: 1})
	}
	return results, nil
}

func seedExternalSearchAssociation(t *testing.T, ctx context.Context, store *semdb.Store) {
	t.Helper()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", SymbolPath: "useState", Kind: codeanchor.ExternalTargetSymbol,
	})
	require.NoError(t, err)
	path := "src/component.tsx"
	owner := "src.component.Component"
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		path: {{
			SrcPath: path, OwnerFQN: owner, RefKind: codeanchor.RefKindCalls,
			DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState",
		}},
	}))
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		path: {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
			OwnerFQN: owner, RefKind: codeanchor.RefKindCalls,
			Raw: codeanchor.RawSymbolTargetKey{
				DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState",
			},
			Target: target,
			Evidence: codeanchor.ExternalEvidence{
				Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh,
			},
		}}},
	}))
}
