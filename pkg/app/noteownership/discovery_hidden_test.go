package noteownership

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// Index discovery reaches a hidden directory only through an include that names
// a path inside it, as obsidian discovery does; broad globs stay out.
func TestDiscoverReachesHiddenDirectoryOnlyThroughExplicitInclude(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"notes/a.md", ".kb/curation/_conflicts.md", ".kb/config/vault.md", ".github/notes.md"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("# x\n"), 0o600))
	}
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	snapshot, err := Discover(context.Background(), DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", ".kb/curation/_conflicts.md"}},
		Registry:        runtime.Registry(),
	})
	require.NoError(t, err)
	var notes []string
	for _, candidate := range snapshot.Candidates() {
		if candidate.Owner == notediscovery.Note {
			notes = append(notes, candidate.Path.String())
		}
	}
	require.Equal(t, []string{".kb/curation/_conflicts.md", "notes/a.md"}, notes)
}
