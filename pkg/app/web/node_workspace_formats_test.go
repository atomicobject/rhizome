package web

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestHTMLCommitWorkspacePreservesProviderFormatWithProjectionCache(t *testing.T) {
	for _, cached := range []bool{true, false} {
		name := "cached"
		if !cached {
			name = "uncached"
		}
		t.Run(name, func(t *testing.T) {
			fixture := prepareOntologyFixtureVault(t)
			fixture.vaultDef.Root = fixture.root
			fixture.vaultDef.Includes = []string{"**/*.md", "**/*.htm"}
			const note = "report.htm"
			const body = "<body><h1>Authored report</h1><p>Keep these bytes.</p></body></html>"
			require.NoError(t, os.WriteFile(filepath.Join(fixture.root, note), []byte("<!doctype html><html><head><title>Original</title></head>"+body), 0o644))
			_, err := ontologytest.Publish(context.Background(), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
			require.NoError(t, err)
			srv := newFixtureServer(t, fixture, &Runtime{IntelStore: fixture.intelStore})
			if !cached {
				srv.nodeProjectionCache = nil
			}
			httpSrv := httptest.NewServer(srv.Handler())
			defer httpSrv.Close()
			var created OntologyEditSessionResponse
			postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
				Ops: []OntologyEditOp{{Kind: "setField", Path: note, Field: "title", Value: "Saved HTML report"}},
			}, &created)
			var committed OntologyEditSessionResponse
			postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", OntologyEditSessionCommitRequest{}, &committed)
			require.Equal(t, "committed", string(committed.Outcome))
			require.Len(t, committed.Workspaces, 1)
			workspace := committed.Workspaces[0]
			require.Equal(t, "html", string(workspace.Content.Format))
			require.Equal(t, "Saved HTML report", workspace.Content.Title)
			require.Equal(t, "Saved HTML report", workspace.Node.Title)
			content, err := os.ReadFile(filepath.Join(fixture.root, note))
			require.NoError(t, err)
			require.Contains(t, string(content), body)
		})
	}
}
