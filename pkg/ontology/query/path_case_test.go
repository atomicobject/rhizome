package query

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func rootFoldsCase(t *testing.T, root, actual, alias string) bool {
	t.Helper()
	actualInfo, err := os.Stat(filepath.Join(root, actual))
	require.NoError(t, err)
	aliasInfo, err := os.Stat(filepath.Join(root, alias))
	if err != nil {
		require.True(t, os.IsNotExist(err), err)
		return false
	}
	return os.SameFile(actualInfo, aliasInfo)
}

func TestExplicitRefsResolveToTheIndexedSpelling(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, `
type MapOfContent @node(paths: ["MOCs/**/*.md"]) { title: String }
`, map[string]string{
		"mocs/riders.md": "---\ntitle: Riders\n---\n\n# Riders\n",
	}, []string{"**/*.md"})
	fold := rootFoldsCase(t, env.root, "mocs", "MOCs")
	t.Logf("vault filesystem folds case: %v", fold)

	prepared, errs := Prepare(env.execSchema, `{
  nodes(refs: ["MOCs/riders.md", "mocs/riders.md"], first: 2) {
    items { requestedRef ref { notePath } node { __typename } error { code message } }
  }
  note(path: "MOCs/riders.md") { path }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	items := result.Data["nodes"].(map[string]any)["items"].([]any)
	require.Len(t, items, 2)
	authored := items[0].(map[string]any)
	indexed := items[1].(map[string]any)
	require.Equal(t, "MOCs/riders.md", authored["requestedRef"], "the response keeps the authored request spelling")
	require.Nil(t, indexed["error"])
	require.Equal(t, "mocs/riders.md", indexed["ref"].(map[string]any)["notePath"])

	if fold {
		require.Nil(t, authored["error"], "an authored spelling that names the indexed file resolves")
		require.Equal(t, "mocs/riders.md", authored["ref"].(map[string]any)["notePath"], "returned refs keep the indexed spelling")
		require.Equal(t, "MapOfContent", authored["node"].(map[string]any)["__typename"], "the schema spelling MOCs/ selects the indexed mocs/ note")
		require.Equal(t, "mocs/riders.md", result.Data["note"].(map[string]any)["path"])
		return
	}
	require.NotNil(t, authored["error"], "a sensitive filesystem keeps differently cased spellings distinct")
	require.Nil(t, authored["node"])
	require.Nil(t, result.Data["note"])
	require.NotEqual(t, "MapOfContent", indexed["node"].(map[string]any)["__typename"], "exact: MOCs/ does not select mocs/")
}
