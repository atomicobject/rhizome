package unifiedsearch

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestResolveSeedHandlesPreservesCanonicalInputOrderAndMarkdownCompatibility(t *testing.T) {
	vaultPath := t.TempDir()
	handles, err := ResolveSeedHandles(context.Background(), []string{
		"file:pkg/search/service.go",
		"note:docs/guide.md",
		"file:pkg/search/service.go",
		"  notes/Decision.MD  ",
		filepath.Join(vaultPath, "notes", "Decision.MD"),
	}, vaultPath, nil, 10)
	require.NoError(t, err)
	require.Equal(t, []knowledge.Handle{
		knowledge.FileHandle("pkg/search/service.go"),
		knowledge.NoteHandle("docs/guide.md"),
		knowledge.NoteHandle("notes/Decision.MD"),
	}, handles)
	require.Equal(t, []string{"pkg/search/service.go", "docs/guide.md", "notes/Decision.MD"}, search.ExplicitSeedPathsFromHandles(handles))
}

func TestResolveSeedHandlesRejectsOutsideVaultRawPaths(t *testing.T) {
	vaultPath := t.TempDir()
	for _, raw := range []string{"../escape.go", "../secret.MD", filepath.Join(t.TempDir(), "outside.go")} {
		t.Run(raw, func(t *testing.T) {
			handles, err := ResolveSeedHandles(context.Background(), []string{"file:pkg/search/service.go", raw}, vaultPath, nil, 10)
			require.ErrorContains(t, err, "must be within vault")
			require.Nil(t, handles)
		})
	}
}
