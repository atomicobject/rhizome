package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunOntologyQueryRejectsMissingMetadataIndexerBeforeOpeningStore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`), 0o644))

	_, err := runOntologyQuery(context.Background(), Config{
		VaultPath: root,
		VaultDef:  obsidian.VaultDefinition{Path: root},
	}, `query { notes(type: "Project", first: 1) { nodes { path } } }`, nil)
	require.ErrorContains(t, err, "note metadata runtime")

	_, statErr := os.Stat(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.True(t, errors.Is(statErr, os.ErrNotExist), "zero metadata indexer must fail before opening a durable store")
}
