package web

import (
	"fmt"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestTreeCountsEligibleOmissions(t *testing.T) {
	root := t.TempDir()
	catalog, err := NewFileCatalog(obsidian.VaultDefinition{Path: root}, nil, testkit.NoteMetadataIndexer(t))
	require.NoError(t, err)
	s := &Server{cfg: Config{VaultPath: root}, catalog: catalog}
	for i := 0; i < 201; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("file%03d.go", i)), nil, 0600))
	}
	page, err := s.listTree(t.Context(), "", 200)
	require.NoError(t, err)
	require.Len(t, page.Entries, 200)
	require.True(t, page.Truncated)
	require.Equal(t, 1, page.Omitted)
	all, err := s.listTree(t.Context(), "", 400)
	require.NoError(t, err)
	require.Len(t, all.Entries, 201)
	require.False(t, all.Truncated)
	require.Zero(t, all.Omitted)
	exact, err := s.listTree(t.Context(), "", 201)
	require.NoError(t, err)
	require.False(t, exact.Truncated)
}
