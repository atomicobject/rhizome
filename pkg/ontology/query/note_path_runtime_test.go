package query

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/stretchr/testify/require"
)

func TestCurrentNotePathsRequiresSelectedProjector(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Document @node(paths: ["**/*.md"]) { title: String @field }`, map[string]string{
		"source.md": "# Source\n",
	})
	deps := env.deps(nil)
	paths, err := currentNotePaths(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, []string{"source.md"}, paths)

	deps.NoteFormats = noteformat.Runtime{}
	paths, err = currentNotePaths(context.Background(), deps)
	require.NoError(t, err)
	require.Empty(t, paths, "unregistered Markdown must not bypass selected-provider admission")

	deps.NoteFormats, err = noteformat.NewRuntime(env.noteFormats.Registry())
	require.NoError(t, err)
	paths, err = currentNotePaths(context.Background(), deps)
	require.NoError(t, err)
	require.Empty(t, paths, "descriptor-only providers must not become executable note sources")
}
