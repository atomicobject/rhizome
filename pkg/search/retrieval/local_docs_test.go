package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestLocalDocsRetriever_SurfacesAncestorAndSubmoduleDocs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg/app/mcp/sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/app/mcp/CONTEXT.md"), []byte("module"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/app/mcp/sub/CONTEXT.md"), []byte("submodule"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/app/mcp/semantic_query_unified.go"), []byte("package mcp"), 0o644))

	r := &LocalDocsRetriever{VaultPath: root, DocPatterns: []string{"CONTEXT.md"}, SubmoduleDepth: 1, Limit: 10}
	got, err := r.Retrieve(context.Background(), search.QuerySpec{
		Intent:            search.IntentSubsystemOverview,
		ExplicitSeedPaths: []string{"pkg/app/mcp"},
		HasExplicitSeeds:  true,
		Limits:            search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "pkg/app/mcp/CONTEXT.md", got[0].Path)
	require.Equal(t, search.DocClassModule, got[0].DocClass)
	require.Equal(t, "pkg/app/mcp/sub/CONTEXT.md", got[1].Path)
}
