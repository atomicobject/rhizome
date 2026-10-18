//go:build !windows

package ontology

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditCommitRejectsAliasedSourceParents(t *testing.T) {
	root := t.TempDir()
	writeOntologyNote(t, root, "real/note.md", "Original\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")))
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, nil)
	require.NoError(t, session.RewriteFile("real/note.md", "First replacement\n"))
	require.NoError(t, session.RewriteFile("alias/note.md", "Second replacement\n"))
	result, err := session.Commit(t.Context())
	require.False(t, result.Applied)
	require.True(t, err != nil || len(result.Conflicts) > 0, "aliased source paths must be rejected explicitly")
	actual, err := os.ReadFile(filepath.Join(root, "real/note.md"))
	require.NoError(t, err)
	require.Equal(t, "Original\n", string(actual))
}
