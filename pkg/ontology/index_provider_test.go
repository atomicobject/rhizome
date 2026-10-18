package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildIndexWithStore_RequiresMarkdownSource(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(matches: ["type:project"]) {
  name: String!
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}

type Decision @node(matches: ["type:decision"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/project.md", `---
type: Project
name: Roadmap
---

See [[decision]].
`)
	writeOntologyNote(t, root, "notes/decision.md", `---
type: Decision
name: API choice
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	_, err = BuildIndexWithStore(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store, schema, "notes-hash")
	require.ErrorContains(t, err, "read note")
}
