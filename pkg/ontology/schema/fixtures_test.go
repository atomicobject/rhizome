package schema

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func writeOntologySchema(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
}

func loadSDL(t *testing.T, sdl string) (*Schema, error) {
	t.Helper()
	root := t.TempDir()
	dir := OntologyDir(root)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0o644))
	return LoadSchema(root)
}

func mustLoadSDL(t *testing.T, sdl string) *Schema {
	t.Helper()
	schema, err := loadSDL(t, sdl)
	require.NoError(t, err)
	return schema
}
