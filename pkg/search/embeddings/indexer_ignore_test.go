package embeddings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanVaultRespectsObsidianIgnore(t *testing.T) {
	tmp := t.TempDir()

	ignored := filepath.Join(tmp, "Ignored.md")
	included := filepath.Join(tmp, "Included.md")
	require.NoError(t, os.WriteFile(ignored, []byte("#secret"), 0o644))
	require.NoError(t, os.WriteFile(included, []byte("#public"), 0o644))
	ignorePath := filepath.Join(tmp, ".rhizome", "ignore")
	require.NoError(t, os.MkdirAll(filepath.Dir(ignorePath), 0o755))
	require.NoError(t, os.WriteFile(ignorePath, []byte("Ignored.md\n"), 0o644))

	ix := &Indexer{Root: tmp}
	notes, err := ix.ScanVault()
	require.NoError(t, err)
	rootPaths, err := paths.NewVaultPaths(tmp)
	require.NoError(t, err)
	relIncluded, err := rootPaths.RelStrict(included)
	require.NoError(t, err)

	paths := make([]string, 0, len(notes))
	for _, n := range notes {
		paths = append(paths, n.Path)
	}

	assert.Len(t, notes, 1)
	assert.Equal(t, relIncluded.String(), paths[0])
}
