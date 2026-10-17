package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	anchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRunUnifiedCore_SyncsNoteMetadataAndOntology(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "project.md"), []byte(`---
type: Project
name: Roadmap
code-anchors:
  go:
    - label: RoadmapSource
      glob: src/**/*.go
---
`), 0o644))

	err := runUnifiedCoreForTest(t, context.Background(), UnifiedOptions{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testkit.NoteMetadataIndexer(t),
	})
	require.NoError(t, err)

	store, err := anchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	paths, err := store.CurrentNotePathsByPropertyValue(context.Background(), "type", "project", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/project.md"}, paths)

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.True(t, state.Ready)

	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/project.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", typ.TypeName)

	anchors, err := store.Anchors(context.Background())
	require.NoError(t, err)
	require.Len(t, anchors, 1)
	require.Equal(t, "RoadmapSource", anchors[0].Label)

	localCfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.False(t, localCfg.Code.Enabled)
	storedScopeHash, hasScopeHash, err := store.GetScopeConfigHash(context.Background())
	require.NoError(t, err)
	require.True(t, hasScopeHash)
	require.Equal(t, localCfg.ScopeConfigHash(), storedScopeHash)
}
