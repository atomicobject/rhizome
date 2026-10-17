package schema

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSchemaCacheUsesCapturedCurrentSources(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `type Project @node(paths: ["projects/*.md"]) { name: String! }`)
	var cache SchemaCache
	first, err := cache.Load(root)
	require.NoError(t, err)
	same, err := cache.Load(root)
	require.NoError(t, err)
	require.Same(t, first, same)
	writeOntologySchema(t, root, `type Project @node(paths: ["projects/*.md"]) { name: String!, status: String }`)
	updated, err := cache.Load(root)
	require.NoError(t, err)
	require.NotSame(t, first, updated)
	require.NotNil(t, updated.Types["Project"].ByName["status"])
	require.Nil(t, first.Types["Project"].ByName["status"])
	writeOntologySchema(t, root, `type Project {`)
	invalid, err := cache.Load(root)
	require.Error(t, err)
	require.Nil(t, invalid)
}
