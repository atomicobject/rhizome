package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestExternalTargetsRemainIsolatedFromAmbientSourceBackedSurfaces(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	localPath := "src/local.ts"
	callerPath := "src/caller.ts"
	for _, summary := range []codeanchor.FileSummary{
		{FilePath: localPath, Lang: codeanchor.LangTS, Hash: "local"},
		{FilePath: callerPath, Lang: codeanchor.LangTS, Hash: "caller"},
	} {
		require.NoError(t, store.ReplaceFileSummary(ctx, summary))
	}
	localAnchor := codeanchor.IntelAnchor{
		AnchorID: "local-render", Lang: codeanchor.LangTS, Kind: "function", Path: localPath,
		Symbol: "RenderLocal", FQN: "src.local.RenderLocal", Fingerprint: "local-render-fp",
	}
	callerAnchor := codeanchor.IntelAnchor{
		AnchorID: "local-caller", Lang: codeanchor.LangTS, Kind: "function", Path: callerPath,
		Symbol: "CallLocal", FQN: "src.caller.CallLocal", Fingerprint: "local-caller-fp",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, localPath, []codeanchor.IntelAnchor{localAnchor}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "anchor", ItemID: localAnchor.AnchorID, Path: localPath,
		Title: localAnchor.Symbol, Body: "local renderer snippet",
	}}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, callerPath, []codeanchor.IntelAnchor{callerAnchor}, []codeanchor.IntelEdge{{
		SrcID: callerAnchor.AnchorID, DstID: localAnchor.AnchorID, Kind: "calls",
	}}, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{localAnchor.AnchorID}, []codeanchor.IntelChunk{{
		ChunkID: "local-render-chunk", OwnerID: localAnchor.AnchorID, OwnerType: "anchor",
		Ord: 0, Granularity: "symbol", ContentHash: "local-render-content",
	}}))

	fingerprintBefore, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	edgesBefore, err := store.AllGraphDocEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edgesBefore, 1)
	filesBefore, err := store.ListFiles(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []string{callerPath, localPath}, filesBefore)

	target := mustExternalTarget(t, "react", "useState")
	path := "src/component.tsx"
	owner := "src.component.Component"
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		path: {{
			SrcPath: path, OwnerFQN: owner, RefKind: codeanchor.RefKindCalls,
			DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState",
		}},
	}))
	evidence := codeanchor.ExternalEvidence{
		Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh,
		ImportedName: "useState", LocalName: "useState",
	}
	require.NoError(t, store.ReplaceExternalEvidenceForPathsBatch(ctx, map[string]codeanchor.ExternalEvidenceBatch{
		path: {
			Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
				OwnerFQN: owner, RefKind: codeanchor.RefKindCalls,
				Raw: codeanchor.RawSymbolTargetKey{
					DstLang: "ts", DstPkg: "react", DstName: "useState", DstFQN: "react.useState",
				},
				Target: target, Evidence: evidence,
			}},
			Imports: []codeanchor.ExternalImportEvidenceInput{{
				Module: "react", BindingOrdinal: 0, Target: target, Evidence: evidence,
			}},
		},
	}))

	// The dedicated operation is the only graph-visible route for this pathless
	// target. Its flags make the non-source-backed contract explicit.
	external, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{Handle: target.Handle, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, "resolved", external.Status)
	require.NotNil(t, external.Target)
	require.True(t, external.Target.External)
	require.False(t, external.Target.Indexed)
	require.False(t, external.Target.SourceBacked)
	require.False(t, external.Target.SourceAvailable)
	require.Len(t, external.Calls, 1)
	require.Len(t, external.Imports, 1)

	// External evidence must not invalidate or enter the default graph used by
	// Explorer and other ambient graph consumers.
	fingerprintAfter, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, fingerprintBefore, fingerprintAfter)
	edges, err := store.AllGraphDocEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, edgesBefore, edges)
	files, err := store.ListFiles(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, filesBefore, files)

	// Lexical search, chunks/embeddings, anchors, and definition lookup are all
	// source-backed surfaces and must remain empty for a catalog-only endpoint.
	fts, err := store.SearchIntelFTS(ctx, "useState", 10)
	require.NoError(t, err)
	require.Empty(t, fts)
	fts, err = store.SearchIntelFTS(ctx, "renderer", 10)
	require.NoError(t, err)
	require.Len(t, fts, 1)
	require.Equal(t, localAnchor.AnchorID, fts[0].ID)
	require.Contains(t, fts[0].Snippet, "local")
	require.Contains(t, fts[0].Snippet, "snippet")
	chunks, err := store.IntelChunks(ctx)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, "local-render-chunk", chunks[0].ChunkID)
	anchors, err := store.IntelAnchorsBySymbol(ctx, "useState", 10)
	require.NoError(t, err)
	require.Empty(t, anchors)
	anchors, err = store.IntelAnchorsBySymbol(ctx, localAnchor.Symbol, 10)
	require.NoError(t, err)
	require.Len(t, anchors, 1)
	require.Equal(t, localAnchor.AnchorID, anchors[0].AnchorID)
	require.Equal(t, localAnchor.FQN, anchors[0].FQN)
	anchorsByFQN, err := store.IntelAnchorsByFQNsLimited(ctx, []string{"react.useState"}, 10)
	require.NoError(t, err)
	require.Empty(t, anchorsByFQN["react.useState"])

	// A real raw association and all four external-table families were seeded,
	// while every ambient file/note/search/semantic/ontology family stayed empty.
	for table, want := range map[string]int{
		"intel_symbol_ref_files":         1,
		"intel_external_targets":         1,
		"intel_external_target_map":      1,
		"intel_external_symbol_evidence": 1,
		"intel_external_import_evidence": 1,
		"files":                          2,
		"notes":                          0,
		"intel_code_anchors":             2,
		"intel_fts":                      1,
		"intel_chunks":                   1,
		"intel_embeddings":               0,
		"ontology_nodes":                 0,
	} {
		var got int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got), table)
		require.Equal(t, want, got, table)
	}
}
