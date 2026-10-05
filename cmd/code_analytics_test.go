package cmd

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestCodeAnalyticsResolveSeedHandles_CanonicalizesVaultRelativeCodePath(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := semdb.Open(filepath.Join(vaultPath, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "pkg/coverage.go",
		Lang: codeanchor.LangGo,
		Hash: "coverage",
	}))

	handles, err := resolveSeedHandles(ctx, []string{filepath.Join(vaultPath, "pkg/coverage.go")}, vaultPath, store, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{knowledge.FileHandle("pkg/coverage.go")}, handles)
}
