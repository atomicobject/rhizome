package graphdb

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestAnchorPageRankExcludesExternalTargets(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	callee := codeanchor.IntelAnchor{
		AnchorID: "local-callee", Lang: codeanchor.LangTS, Kind: "function", Path: "src/callee.ts",
		Symbol: "RenderLocal", FQN: "src.callee.RenderLocal", Fingerprint: "local-callee-fp",
	}
	caller := codeanchor.IntelAnchor{
		AnchorID: "local-caller", Lang: codeanchor.LangTS, Kind: "function", Path: "src/caller.ts",
		Symbol: "CallLocal", FQN: "src.caller.CallLocal", Fingerprint: "local-caller-fp",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, callee.Path, []codeanchor.IntelAnchor{callee}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, caller.Path, []codeanchor.IntelAnchor{caller}, []codeanchor.IntelEdge{{
		SrcID: caller.AnchorID, DstID: callee.AnchorID, Kind: "calls",
	}}, nil))

	before, err := ComputeAnchorPageRank(ctx, store)
	require.NoError(t, err)
	beforeRanks := pageRanksByAnchor(before)
	require.Len(t, beforeRanks, 2)
	require.Contains(t, beforeRanks, caller.AnchorID)
	require.Contains(t, beforeRanks, callee.AnchorID)

	seedExternalPageRankAssociation(t, ctx, store)

	after, err := ComputeAnchorPageRank(ctx, store)
	require.NoError(t, err)
	require.Equal(t, beforeRanks, pageRanksByAnchor(after))
}

func pageRanksByAnchor(scores []semdb.AnchorScore) map[string]float64 {
	out := make(map[string]float64, len(scores))
	for _, score := range scores {
		out[score.AnchorID] = score.PageRank
	}
	return out
}

func seedExternalPageRankAssociation(t *testing.T, ctx context.Context, store *semdb.Store) {
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
