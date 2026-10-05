package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestExplorerAndDefaultGraphExcludeExternalTargets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "local.ts"), []byte("export function RenderLocal() {}\n"), 0o644))

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	localPath := "src/local.ts"
	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{FilePath: localPath, Lang: codeanchor.LangTS, Hash: "local"}))
	localAnchor := codeanchor.IntelAnchor{
		AnchorID: "local-render", Lang: codeanchor.LangTS, Kind: "function", Path: localPath,
		Symbol: "RenderLocal", FQN: "src.local.RenderLocal", Fingerprint: "local-render-fp",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, localPath, []codeanchor.IntelAnchor{localAnchor}, nil, nil))
	require.NoError(t, store.ReplaceGraphDocScores(ctx, []semdb.GraphDocScore{{DocPath: localPath, DocType: "code", UpdatedAt: 1}}))
	require.NoError(t, store.ReplaceAnchorScores(ctx, []semdb.AnchorScore{{AnchorID: localAnchor.AnchorID, PageRank: 1, Updated: 1}}))
	seedExternalWebAssociation(t, ctx, store)

	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}
	srv, err := NewServer(ctx, Config{
		Vault: &obsidian.Vault{Name: "test"}, VaultPath: root, VaultDef: vaultDef,
		Runtime:      &Runtime{IntelStore: store},
		NoteMetadata: testNoteMetadataIndexer(t),
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })
	handler := srv.Handler()

	// Explorer is filesystem-backed. The external association has a source key,
	// but no source file or catalog entry is invented for it.
	var tree TreeResponse
	getExternalIsolationJSON(t, handler, "/api/v1/files/tree?path=src&limit=20", &tree)
	require.Len(t, tree.Entries, 1)
	require.Equal(t, localPath, tree.Entries[0].Path)
	require.Equal(t, "code", tree.Entries[0].Kind)

	// The default and local workspace graph consumers retain the real source
	// node and never project the pathless external endpoint.
	var global GraphResponse
	getExternalIsolationJSON(t, handler, "/api/v1/graphs/global?limit=100&depth=2", &global)
	require.Len(t, global.Nodes, 1)
	require.Equal(t, localPath, global.Nodes[0].Path)
	require.Equal(t, "code", global.Nodes[0].Kind)
	require.NotContains(t, global.Nodes[0].ID, "external")
	require.NotContains(t, global.Nodes[0].Label, "useState")

	var local GraphResponse
	getExternalIsolationJSON(t, handler, "/api/v1/graphs/local?path="+localPath+"&limit=100&depth=1", &local)
	require.Equal(t, nodeID("code", localPath), local.CenterID)
	center := graphNodeByID(local.Nodes, local.CenterID)
	require.Equal(t, localPath, center.Path)
	require.Equal(t, "code", center.Kind)
	for _, node := range local.Nodes {
		require.NotContains(t, node.ID, "external")
		require.NotContains(t, node.Label, "useState")
	}
}

func getExternalIsolationJSON(t *testing.T, handler http.Handler, target string, out any) {
	t.Helper()
	request := newApplicationRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), out))
}

func seedExternalWebAssociation(t *testing.T, ctx context.Context, store *semdb.Store) {
	t.Helper()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", SymbolPath: "useState", Kind: codeanchor.ExternalTargetSymbol,
	})
	require.NoError(t, err)
	path := "src/external-user.tsx"
	owner := "src.external.Component"
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
