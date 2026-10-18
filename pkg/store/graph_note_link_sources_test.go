package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGraphDocNoteLinkSourcesByTargetsIncludesProvidersAndBatches(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "note-link-sources.db"))
	require.NoError(t, err)
	defer store.Close()
	for source, kind := range map[string]string{"wiki.md": "wikilink", "markdown.md": "mdlink", "effort.html": "note_link:uri:basic", "code.go": "call"} {
		require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, source, kind, []string{"target.md"}))
	}
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "unrelated.md", "wikilink", []string{"other.md"}))
	paths := make([]string, 901)
	for i := range paths {
		paths[i] = fmt.Sprintf("not-target-%d.md", i)
	}
	paths[900] = "target.md"
	got, err := store.GraphDocNoteLinkSourcesByTargets(ctx, paths)
	require.NoError(t, err)
	require.Equal(t, []string{"effort.html", "markdown.md", "wiki.md"}, got)
	empty, err := store.GraphDocNoteLinkSourcesByTargets(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}
