package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestDefinitionRetriever_ResolvesQueryToAnchor(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "defs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/defs.go",
		Symbol:      "MakePlan",
		FQN:         "pkg.MakePlan",
		Fingerprint: "fp-a1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/defs.go", []codeanchor.IntelAnchor{anchor}, nil, nil))

	r := &DefinitionRetriever{Store: store, Limit: 5}
	results, err := r.Retrieve(ctx, search.QuerySpec{Text: "go to definition for MakePlan"})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	found := false
	for _, c := range results {
		if c.AnchorID == "a1" {
			found = true
			require.Equal(t, "definition_anchor", c.Evidence[0].Type)
			break
		}
	}
	require.True(t, found, "expected definition anchor to be returned")
}

func TestDefinitionRetriever_TreatsContainingModuleAsContextForSelectedSymbol(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "defs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const path = "pkg/defs.go"
	module := codeanchor.IntelAnchor{AnchorID: "module", Lang: codeanchor.LangGo, Kind: "module", Path: path, Symbol: "defs", FQN: "pkg", Fingerprint: "module-fp"}
	symbol := codeanchor.IntelAnchor{AnchorID: "symbol", Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "MakePlan", FQN: "pkg.MakePlan", Fingerprint: "symbol-fp"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{module, symbol}, nil, nil))

	r := &DefinitionRetriever{Store: store, Limit: 5}
	results, err := r.Retrieve(ctx, search.QuerySpec{Intent: search.IntentGoToDef, Seeds: []knowledge.Handle{
		knowledge.AnchorHandle(symbol.AnchorID),
		knowledge.FileHandle(path),
	}})
	require.NoError(t, err)
	require.Len(t, results, 2)

	byID := make(map[string]search.Candidate, len(results))
	for _, result := range results {
		byID[result.AnchorID] = result
	}
	require.Equal(t, "definition_anchor", byID[symbol.AnchorID].Evidence[0].Type)
	require.True(t, search.HasPrimaryEvidence(search.IntentGoToDef, byID[symbol.AnchorID].Evidence))
	require.Equal(t, "code_anchor", byID[module.AnchorID].Evidence[0].Type)
	require.False(t, search.HasPrimaryEvidence(search.IntentGoToDef, byID[module.AnchorID].Evidence))
}
