package indexcore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestScopedMarkdownPathsIncludePersistedNotesAndCurrentChanges(t *testing.T) {
	f := newFixture(t)
	f.request.VaultDefinition.Includes = []string{"notes/*"}
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "notes/page.html"), []byte("<h1>Page</h1>"), 0600))
	f.publish(t, f.request)
	require.NoError(t, os.Remove(filepath.Join(f.root, "notes/source.md")))
	require.NoError(t, os.Rename(filepath.Join(f.root, "notes/target.md"), filepath.Join(f.root, "notes/renamed.MD")))
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "notes/added.md"), []byte("# Added\n"), 0600))
	f.request.Paths = []paths.RelPath{"notes/source.md", "notes/target.md", "notes/renamed.MD", "notes/added.md"}
	discovery, err := indexcore.Discover(context.Background(), f.request, f.store)
	require.NoError(t, err)
	require.False(t, discovery.FullDiscovery)
	require.Equal(t, []paths.NotePath{"notes/added.md", "notes/renamed.MD", "notes/untouched.md"}, discovery.MarkdownPaths())
}
